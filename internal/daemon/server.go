package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"codex-desktop-quota-guard/internal/domain"
	"codex-desktop-quota-guard/internal/store"
)

type Server struct {
	addr    string
	service *Service
	store   *store.Store
	log     *slog.Logger
	http    *http.Server
}

func NewServer(addr string, svc *Service, st *store.Store, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{addr: addr, service: svc, store: st, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/v1/quota", s.quota)
	mux.HandleFunc("/v1/tasks", s.tasks)
	mux.HandleFunc("/v1/tasks/register", s.register)
	mux.HandleFunc("/v1/tasks/checkpoint", s.checkpoint)
	mux.HandleFunc("/v1/tasks/paused", s.paused)
	mux.HandleFunc("/v1/tasks/complete", s.complete)
	mux.HandleFunc("/v1/actions", s.actions)
	mux.HandleFunc("/v1/actions/", s.actionRoute)
	s.http = &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	return s
}

func (s *Server) ListenAndServe() error              { return s.http.ListenAndServe() }
func (s *Server) Shutdown(ctx context.Context) error { return s.http.Shutdown(ctx) }

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	jsonOut(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) quota(w http.ResponseWriter, r *http.Request) {
	q, d, err := s.service.CurrentDecision(r.Context())
	if err != nil {
		httpErr(w, 500, err)
		return
	}
	jsonOut(w, 200, map[string]any{"quota": q, "decision": d})
}

func (s *Server) tasks(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListTasks(r.Context())
	if err != nil {
		httpErr(w, 500, err)
		return
	}
	jsonOut(w, 200, items)
}

type taskReq struct {
	ThreadID  string `json:"threadId"`
	TurnID    string `json:"turnId"`
	Objective string `json:"objective"`
	Workspace string `json:"workspace"`
	Summary   string `json:"summary"`
	Pending   string `json:"pending"`
	LastTest  string `json:"lastTest"`
	Reason    string `json:"reason"`
}

func decode[T any](r *http.Request) (T, error) {
	var v T
	defer r.Body.Close()
	err := json.NewDecoder(io.LimitReader(r.Body, 128*1024)).Decode(&v)
	return v, err
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	v, err := decode[taskReq](r)
	if err != nil || v.ThreadID == "" {
		httpErr(w, 400, errors.New("threadId required"))
		return
	}
	t, err := s.store.UpsertTask(r.Context(), v.ThreadID, v.TurnID, v.Objective, v.Workspace)
	if err != nil {
		httpErr(w, 500, err)
		return
	}
	jsonOut(w, 200, t)
}

func (s *Server) checkpoint(w http.ResponseWriter, r *http.Request) {
	v, err := decode[taskReq](r)
	if err != nil || v.ThreadID == "" {
		httpErr(w, 400, errors.New("threadId required"))
		return
	}
	if err := s.store.SaveCheckpoint(r.Context(), v.ThreadID, v.Summary, v.Pending, v.LastTest); err != nil {
		httpErr(w, 500, err)
		return
	}
	jsonOut(w, 200, map[string]any{"ok": true})
}

func (s *Server) paused(w http.ResponseWriter, r *http.Request) {
	v, err := decode[taskReq](r)
	if err != nil || v.ThreadID == "" {
		httpErr(w, 400, errors.New("threadId required"))
		return
	}
	t, err := s.store.GetByThread(r.Context(), v.ThreadID)
	if err != nil {
		httpErr(w, 404, err)
		return
	}
	if t.State == domain.StateRunning {
		_, _ = s.store.Transition(r.Context(), v.ThreadID, domain.StatePauseRequested, "agent pause")
	}
	t, err = s.store.Transition(r.Context(), v.ThreadID, domain.StatePausedQuota, firstNonEmpty(v.Reason, "quota"))
	if err != nil {
		httpErr(w, 409, err)
		return
	}
	jsonOut(w, 200, t)
}

func (s *Server) complete(w http.ResponseWriter, r *http.Request) {
	v, err := decode[taskReq](r)
	if err != nil || v.ThreadID == "" {
		httpErr(w, 400, errors.New("threadId required"))
		return
	}
	if v.Summary != "" {
		_ = s.store.SaveCheckpoint(r.Context(), v.ThreadID, v.Summary, "", "")
	}
	t, err := s.store.Transition(r.Context(), v.ThreadID, domain.StateCompleted, "agent completed")
	if err != nil {
		httpErr(w, 409, err)
		return
	}
	jsonOut(w, 200, t)
}

func (s *Server) actions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.store.PendingActions(r.Context(), 20)
	if err != nil {
		httpErr(w, 500, err)
		return
	}
	jsonOut(w, 200, items)
}

func (s *Server) actionRoute(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/claim"):
		s.actionClaim(w, r)
	case strings.HasSuffix(r.URL.Path, "/uncertain"):
		s.actionUncertain(w, r)
	case strings.HasSuffix(r.URL.Path, "/ack"):
		s.actionAck(w, r)
	default:
		http.NotFound(w, r)
	}
}

func actionIDFromPath(path, suffix string) (int64, error) {
	idText := strings.TrimPrefix(path, "/v1/actions/")
	idText = strings.TrimSuffix(idText, suffix)
	return strconv.ParseInt(strings.Trim(idText, "/"), 10, 64)
}

func (s *Server) actionClaim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, err := actionIDFromPath(r.URL.Path, "/claim")
	if err != nil {
		httpErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.ClaimAction(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrActionNotPending) {
			httpErr(w, http.StatusConflict, err)
			return
		}
		httpErr(w, http.StatusInternalServerError, err)
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) actionUncertain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, err := actionIDFromPath(r.URL.Path, "/uncertain")
	if err != nil {
		httpErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 32*1024)).Decode(&body)
	}
	if err := s.store.MarkActionUncertain(r.Context(), id, body.Reason); err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) actionAck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, err := actionIDFromPath(r.URL.Path, "/ack")
	if err != nil {
		httpErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 32*1024)).Decode(&body); err != nil {
		httpErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.CompleteClaimedAction(r.Context(), id, body.Success, body.Error); err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{"ok": true})
}

func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, status int, err error) {
	jsonOut(w, status, map[string]any{"error": err.Error()})
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
