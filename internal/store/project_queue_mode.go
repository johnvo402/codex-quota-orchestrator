package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"codex-desktop-quota-guard/internal/domain"
)

type ProjectQueueSettings struct {
	ProjectID string                  `json:"projectId"`
	Mode      domain.ProjectQueueMode `json:"mode"`
	UpdatedAt time.Time               `json:"updatedAt,omitempty"`
}

func (s *Store) GetProjectQueueMode(ctx context.Context, projectID string) (ProjectQueueSettings, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ProjectQueueSettings{}, errors.New("projectId required")
	}
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return ProjectQueueSettings{}, err
	}

	var modeRaw string
	var updated int64
	err := s.db.QueryRowContext(ctx, `SELECT mode,updated_at FROM project_queue_settings WHERE project_id=?`, projectID).Scan(&modeRaw, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		// No policy row means the legacy/default behavior: AUTO.
		return ProjectQueueSettings{ProjectID: projectID, Mode: domain.ProjectQueueAuto}, nil
	}
	if err != nil {
		return ProjectQueueSettings{}, err
	}
	mode := domain.ProjectQueueMode(modeRaw)
	if !mode.Valid() {
		return ProjectQueueSettings{}, fmt.Errorf("invalid stored queue mode %q", modeRaw)
	}
	return ProjectQueueSettings{ProjectID: projectID, Mode: mode, UpdatedAt: time.UnixMilli(updated)}, nil
}

func (s *Store) SetProjectQueueMode(ctx context.Context, projectID string, mode domain.ProjectQueueMode) (ProjectQueueSettings, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ProjectQueueSettings{}, errors.New("projectId required")
	}
	if !mode.Valid() {
		return ProjectQueueSettings{}, fmt.Errorf("invalid queue mode %q; use AUTO, MANUAL, or PAUSED", mode)
	}
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return ProjectQueueSettings{}, err
	}
	now := time.Now().UTC().UnixMilli()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO project_queue_settings(project_id,mode,updated_at)
VALUES(?,?,?)
ON CONFLICT(project_id) DO UPDATE SET mode=excluded.mode,updated_at=excluded.updated_at
`, projectID, string(mode), now)
	if err != nil {
		return ProjectQueueSettings{}, err
	}
	return ProjectQueueSettings{ProjectID: projectID, Mode: mode, UpdatedAt: time.UnixMilli(now)}, nil
}
