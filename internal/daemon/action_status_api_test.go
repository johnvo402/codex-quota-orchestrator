package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"codex-desktop-quota-guard/internal/config"
	"codex-desktop-quota-guard/internal/store"
)

func TestActionStatusEndpointShowsStopFailure(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	if _, err := st.UpsertTask(ctx, "thread-action-api", "turn-action-api", "working", `D:\work`); err != nil {
		t.Fatal(err)
	}
	action, err := st.QueueStopSafe(ctx, "thread-action-api")
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	svc := NewService(cfg, st, nil)
	srv := NewServer(cfg.ListenAddr, svc, st, nil)
	get := func() store.ActionStatus {
		r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/actions/%d", action.ID), nil)
		w := httptest.NewRecorder()
		srv.http.Handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		var out store.ActionStatus
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	if got := get(); got.Status != "pending" {
		t.Fatalf("initial status=%q want pending", got.Status)
	}
	if err := st.ClaimAction(ctx, action.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteClaimedAction(ctx, action.ID, false, "Stop button missing"); err != nil {
		t.Fatal(err)
	}
	got := get()
	if got.Status != "failed" || got.Error != "Stop button missing" {
		t.Fatalf("action status=%#v", got)
	}
}
