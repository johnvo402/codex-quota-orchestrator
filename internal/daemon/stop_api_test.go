package daemon

import (
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

func TestDashboardStopQueuesExactCurrentTurn(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	task, err := st.UpsertTask(ctx, "thread-stop", "turn-stop", "working", `D:\work`)
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	svc := NewService(cfg, st, nil)
	srv := NewServer(cfg.ListenAddr, svc, st, nil)

	call := func() map[string]any {
		r := httptest.NewRequest(http.MethodPost, "/v1/dashboard/tasks/"+task.ID+"/stop", nil)
		w := httptest.NewRecorder()
		srv.http.Handler.ServeHTTP(w, r)
		if w.Code != http.StatusAccepted {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	first := call()
	second := call()
	if first["actionId"] != second["actionId"] {
		t.Fatalf("stop endpoint should deduplicate action: first=%v second=%v", first["actionId"], second["actionId"])
	}
	if first["turnId"] != "turn-stop" {
		t.Fatalf("turnId=%v want turn-stop", first["turnId"])
	}

	actions, err := st.PendingActions(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Kind != store.ActionKindStop || actions[0].Message != "turn-stop" {
		t.Fatalf("unexpected pending stop actions: %#v", actions)
	}
}

func TestDashboardStopRejectsTerminalTask(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	task, err := st.UpsertTask(ctx, "thread-stop", "turn-stop", "working", `D:\work`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, task.ThreadID, domain.StateCompleted, "done"); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	svc := NewService(cfg, st, nil)
	srv := NewServer(cfg.ListenAddr, svc, st, nil)
	r := httptest.NewRequest(http.MethodPost, "/v1/dashboard/tasks/"+task.ID+"/stop", nil)
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d want 409 body=%s", w.Code, w.Body.String())
	}
}
