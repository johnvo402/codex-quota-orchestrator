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

func TestActionStatusEndpointShowsDeliveryFailure(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	action, err := st.EnqueueAction(ctx, "pause_notice", "thread-action-api", "pause safely")
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
	if err := st.CompleteClaimedAction(ctx, action.ID, false, "native delivery failed"); err != nil {
		t.Fatal(err)
	}
	got := get()
	if got.Status != "failed" || got.Error != "native delivery failed" {
		t.Fatalf("action status=%#v", got)
	}
}
