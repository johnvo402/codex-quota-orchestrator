package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	mux.HandleFunc("/v1/actions/", s.actionAck)
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
	if r.Method != "POST" {
		w.WriteHeader(405)
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
	items, err := s.store.PendingActions(r.Context(), 20)
	if err != nil {
		httpErr(w, 500, err)
		return
	}
	jsonOut(w, 200, items)
}
func (s *Server) actionAck(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	idText := strings.TrimPrefix(r.URL.Path, "/v1/actions/")
	idText = strings.TrimSuffix(idText, "/ack")
	id, err := strconv.ParseInt(strings.Trim(idText, "/"), 10, 64)
	if err != nil {
		httpErr(w, 400, err)
		return
	}
	var body struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpErr(w, 400, err)
		return
	}
	a, err := s.store.GetAction(r.Context(), id)
	if err != nil {
		httpErr(w, 404, err)
		return
	}
	if err := s.store.AckAction(r.Context(), id, body.Success); err != nil {
		httpErr(w, 500, err)
		return
	}
	if body.Success && a.Kind == "resume" {
		_, _ = s.store.Transition(r.Context(), a.ThreadID, domain.StateRunning, "resume delivered")
	}
	if !body.Success && a.Kind == "resume" {
		_, _ = s.store.Transition(r.Context(), a.ThreadID, domain.StatePausedQuota, "resume delivery failed")
	}
	jsonOut(w, 200, map[string]any{"ok": true})
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

var _ = fmt.Sprintf
