package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"codex-desktop-quota-guard/internal/config"
	"codex-desktop-quota-guard/internal/domain"
	"codex-desktop-quota-guard/internal/store"
)

func TestRegisterAPIReactivatesCompletedThreadForNewTurn(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cfg := config.Default()
	svc := NewService(cfg, st, nil)
	srv := NewServer(cfg.ListenAddr, svc, st, nil)

	register := func(turnID, objective string) domain.Task {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"threadId":  "thread-chat",
			"turnId":    turnID,
			"objective": objective,
		})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/v1/tasks/register", bytes.NewReader(body))
		w := httptest.NewRecorder()
		srv.http.Handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
		}
		var task domain.Task
		if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
			t.Fatal(err)
		}
		return task
	}

	first := register("turn-1", "first objective")
	if _, err := st.Transition(context.Background(), first.ThreadID, domain.StateCompleted, "done"); err != nil {
		t.Fatal(err)
	}

	second := register("turn-2", "follow-up objective")
	if second.ID != first.ID {
		t.Fatalf("task id changed: first=%s second=%s", first.ID, second.ID)
	}
	if second.State != domain.StateRunning {
		t.Fatalf("state=%s want RUNNING", second.State)
	}
	if second.TurnID != "turn-2" || second.Objective != "follow-up objective" {
		t.Fatalf("unexpected continued task: turn=%q objective=%q", second.TurnID, second.Objective)
	}
}
