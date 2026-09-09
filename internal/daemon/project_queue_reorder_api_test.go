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

func TestProjectTaskReorderAPI(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	p, _ := st.CreateProject(ctx, "Demo", `D:\work\demo`, "")
	a, _ := st.CreateProjectTask(ctx, p.ID, "A", "")
	b, _ := st.CreateProjectTask(ctx, p.ID, "B", "")
	c, _ := st.CreateProjectTask(ctx, p.ID, "C", "")

	cfg := config.Default()
	svc := NewService(cfg, st, nil)
	srv := NewServer(cfg.ListenAddr, svc, st, nil)
	body, _ := json.Marshal(projectTaskReorderReq{ProjectID: p.ID, TaskIDs: []string{c.ID, a.ID, b.ID}})
	r := httptest.NewRequest(http.MethodPut, "/v1/project-tasks/reorder", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var items []domain.ProjectTask
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	var queued []domain.ProjectTask
	for _, item := range items {
		if item.State == domain.ProjectTaskQueued {
			queued = append(queued, item)
		}
	}
	if len(queued) != 3 || queued[0].ID != c.ID || queued[0].Position != 1 || queued[1].ID != a.ID || queued[1].Position != 2 || queued[2].ID != b.ID || queued[2].Position != 3 {
		t.Fatalf("unexpected reordered queue: %#v", queued)
	}
}

func TestProjectTaskReorderAPIRejectsPartialQueue(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	p, _ := st.CreateProject(ctx, "Demo", `D:\work\demo`, "")
	a, _ := st.CreateProjectTask(ctx, p.ID, "A", "")
	_, _ = st.CreateProjectTask(ctx, p.ID, "B", "")

	cfg := config.Default()
	srv := NewServer(cfg.ListenAddr, NewService(cfg, st, nil), st, nil)
	body, _ := json.Marshal(projectTaskReorderReq{ProjectID: p.ID, TaskIDs: []string{a.ID}})
	r := httptest.NewRequest(http.MethodPut, "/v1/project-tasks/reorder", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
}
