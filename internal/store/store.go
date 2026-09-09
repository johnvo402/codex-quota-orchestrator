package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"codex-desktop-quota-guard/internal/domain"
	"codex-desktop-quota-guard/internal/quota"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

type Action struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	ThreadID  string    `json:"threadId"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=NORMAL", "PRAGMA busy_timeout=5000", "PRAGMA foreign_keys=ON"} {
		if _, err := db.Exec(q); err != nil {
			db.Close()
			return nil, fmt.Errorf("sqlite pragma: %w", err)
		}
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.ensureDashboardSchema(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.ensureProjectQueueSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS tasks(
 id TEXT PRIMARY KEY,
 thread_id TEXT NOT NULL UNIQUE,
 turn_id TEXT NOT NULL DEFAULT '',
 objective TEXT NOT NULL,
 workspace TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL,
 pause_reason TEXT NOT NULL DEFAULT '',
 checkpoint TEXT NOT NULL DEFAULT '',
 pending TEXT NOT NULL DEFAULT '',
 last_test TEXT NOT NULL DEFAULT '',
 last_quota_remaining REAL,
 last_quota_reset_at INTEGER,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tasks_state ON tasks(state);
CREATE TABLE IF NOT EXISTS task_events(
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 task_id TEXT NOT NULL,
 from_state TEXT NOT NULL,
 to_state TEXT NOT NULL,
 reason TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL,
 FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS actions(
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 kind TEXT NOT NULL,
 thread_id TEXT NOT NULL,
 message TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending',
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_actions_status ON actions(status,id);
CREATE TABLE IF NOT EXISTS quota_state(
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 remaining_percent REAL NOT NULL,
 reset_at INTEGER,
 hard_pause INTEGER NOT NULL,
 soft_pause INTEGER NOT NULL,
 can_resume INTEGER NOT NULL,
 observed_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS quota_state_v2(
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 snapshot_json TEXT NOT NULL,
 observed_at INTEGER NOT NULL
);
`)
	return err
}

func newID() string { b := make([]byte, 16); _, _ = rand.Read(b); return hex.EncodeToString(b) }

func (s *Store) UpsertTask(ctx context.Context, threadID, turnID, objective, workspace string) (domain.Task, error) {
	now := time.Now().UTC().UnixMilli()
	if objective == "" {
		objective = "Desktop task"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO tasks(id,thread_id,turn_id,objective,workspace,state,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(thread_id) DO UPDATE SET turn_id=CASE WHEN excluded.turn_id<>'' THEN excluded.turn_id ELSE tasks.turn_id END,
objective=CASE WHEN excluded.objective<>'' THEN excluded.objective ELSE tasks.objective END,
workspace=CASE WHEN excluded.workspace<>'' THEN excluded.workspace ELSE tasks.workspace END,
updated_at=excluded.updated_at`, newID(), threadID, turnID, objective, workspace, domain.StateRunning, now, now)
	if err != nil {
		return domain.Task{}, err
	}
	return s.GetByThread(ctx, threadID)
}

func (s *Store) GetByThread(ctx context.Context, threadID string) (domain.Task, error) {
	return scanTask(s.db.QueryRowContext(ctx, `SELECT id,thread_id,turn_id,objective,workspace,state,pause_reason,checkpoint,pending,last_test,last_quota_remaining,last_quota_reset_at,created_at,updated_at FROM tasks WHERE thread_id=?`, threadID))
}
func (s *Store) GetTask(ctx context.Context, id string) (domain.Task, error) {
	return scanTask(s.db.QueryRowContext(ctx, `SELECT id,thread_id,turn_id,objective,workspace,state,pause_reason,checkpoint,pending,last_test,last_quota_remaining,last_quota_reset_at,created_at,updated_at FROM tasks WHERE id=?`, id))
}
func (s *Store) ListTasks(ctx context.Context) ([]domain.Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,thread_id,turn_id,objective,workspace,state,pause_reason,checkpoint,pending,last_test,last_quota_remaining,last_quota_reset_at,created_at,updated_at FROM tasks ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Store) ListStates(ctx context.Context, states ...domain.TaskState) ([]domain.Task, error) {
	all, err := s.ListTasks(ctx)
	if err != nil {
		return nil, err
	}
	set := map[domain.TaskState]bool{}
	for _, st := range states {
		set[st] = true
	}
	out := []domain.Task{}
	for _, t := range all {
		if set[t.State] {
			out = append(out, t)
		}
	}
	return out, nil
}
func (s *Store) Transition(ctx context.Context, threadID string, to domain.TaskState, reason string) (domain.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()
	var id, currentRaw string
	if err := tx.QueryRowContext(ctx, `SELECT id,state FROM tasks WHERE thread_id=?`, threadID).Scan(&id, &currentRaw); err != nil {
		return domain.Task{}, err
	}
	current := domain.TaskState(currentRaw)
	if current == to {
		return s.GetByThread(ctx, threadID)
	}
	if err := domain.ValidateTransition(current, to); err != nil {
		return domain.Task{}, err
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET state=?,pause_reason=?,updated_at=? WHERE thread_id=?`, to, reason, now, threadID); err != nil {
		return domain.Task{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_events(task_id,from_state,to_state,reason,created_at)VALUES(?,?,?,?,?)`, id, current, to, reason, now); err != nil {
		return domain.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, err
	}
	return s.GetByThread(ctx, threadID)
}
func (s *Store) SaveCheckpoint(ctx context.Context, threadID, summary, pending, lastTest string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET checkpoint=?,pending=?,last_test=?,updated_at=? WHERE thread_id=?`, summary, pending, lastTest, time.Now().UTC().UnixMilli(), threadID)
	return err
}
func (s *Store) UpdateTaskQuota(ctx context.Context, threadID string, q quota.Snapshot) error {
	var reset any
	if q.ResetAt != nil {
		reset = q.ResetAt.UnixMilli()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET last_quota_remaining=?,last_quota_reset_at=?,updated_at=? WHERE thread_id=?`, q.RemainingPercent, reset, time.Now().UTC().UnixMilli(), threadID)
	return err
}
func (s *Store) SaveQuota(ctx context.Context, q quota.Snapshot) error {
	payload, err := json.Marshal(q)
	if err != nil {
		return fmt.Errorf("marshal quota snapshot: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO quota_state_v2(singleton,snapshot_json,observed_at) VALUES(1,?,?) ON CONFLICT(singleton) DO UPDATE SET snapshot_json=excluded.snapshot_json,observed_at=excluded.observed_at`, string(payload), q.ObservedAt.UnixMilli())
	if err != nil {
		return err
	}
	var reset any
	if q.ResetAt != nil {
		reset = q.ResetAt.UnixMilli()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO quota_state(singleton,remaining_percent,reset_at,hard_pause,soft_pause,can_resume,observed_at) VALUES(1,?,?,?,?,?,?) ON CONFLICT(singleton) DO UPDATE SET remaining_percent=excluded.remaining_percent,reset_at=excluded.reset_at,hard_pause=excluded.hard_pause,soft_pause=excluded.soft_pause,can_resume=excluded.can_resume,observed_at=excluded.observed_at`, q.RemainingPercent, reset, boolInt(q.HardPause), boolInt(q.SoftPause), boolInt(q.CanResume), q.ObservedAt.UnixMilli())
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) LatestQuota(ctx context.Context) (quota.Snapshot, error) {
	var q quota.Snapshot
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT snapshot_json FROM quota_state_v2 WHERE singleton=1`).Scan(&raw)
	if err == nil {
		if err := json.Unmarshal([]byte(raw), &q); err != nil {
			return q, fmt.Errorf("decode quota snapshot: %w", err)
		}
		return q, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return q, err
	}
	var reset sql.NullInt64
	var hard, soft, resume int
	var observed int64
	err = s.db.QueryRowContext(ctx, `SELECT remaining_percent,reset_at,hard_pause,soft_pause,can_resume,observed_at FROM quota_state WHERE singleton=1`).Scan(&q.RemainingPercent, &reset, &hard, &soft, &resume, &observed)
	if err != nil {
		return q, err
	}
	q.HardPause = hard != 0
	q.SoftPause = soft != 0
	q.CanResume = resume != 0
	q.ObservedAt = time.UnixMilli(observed)
	if reset.Valid {
		t := time.UnixMilli(reset.Int64)
		q.ResetAt = &t
	}
	return q, nil
}
func (s *Store) EnqueueAction(ctx context.Context, kind, threadID, message string) (Action, error) {
	now := time.Now().UTC().UnixMilli()
	res, err := s.db.ExecContext(ctx, `INSERT INTO actions(kind,thread_id,message,status,created_at,updated_at)VALUES(?,?,?,'pending',?,?)`, kind, threadID, message, now, now)
	if err != nil {
		return Action{}, err
	}
	id, _ := res.LastInsertId()
	return Action{ID: id, Kind: kind, ThreadID: threadID, Message: message, CreatedAt: time.UnixMilli(now)}, nil
}
func (s *Store) PendingActions(ctx context.Context, limit int) ([]Action, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,thread_id,message,created_at FROM actions WHERE status='pending' ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Action{}
	for rows.Next() {
		var a Action
		var ts int64
		if err := rows.Scan(&a.ID, &a.Kind, &a.ThreadID, &a.Message, &ts); err != nil {
			return nil, err
		}
		a.CreatedAt = time.UnixMilli(ts)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetAction(ctx context.Context, id int64) (Action, error) {
	var a Action
	var ts int64
	err := s.db.QueryRowContext(ctx, `SELECT id,kind,thread_id,message,created_at FROM actions WHERE id=?`, id).Scan(&a.ID, &a.Kind, &a.ThreadID, &a.Message, &ts)
	if err != nil {
		return a, err
	}
	a.CreatedAt = time.UnixMilli(ts)
	return a, nil
}

func (s *Store) AckAction(ctx context.Context, id int64, success bool) error {
	status := "done"
	if !success {
		status = "failed"
	}
	res, err := s.db.ExecContext(ctx, `UPDATE actions SET status=?,updated_at=? WHERE id=?`, status, time.Now().UTC().UnixMilli(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("action not found")
	}
	return nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

type scanner interface{ Scan(...any) error }

func scanTask(s scanner) (domain.Task, error) {
	var t domain.Task
	var state string
	var q sql.NullFloat64
	var reset sql.NullInt64
	var created, updated int64
	err := s.Scan(&t.ID, &t.ThreadID, &t.TurnID, &t.Objective, &t.Workspace, &state, &t.PauseReason, &t.Checkpoint, &t.Pending, &t.LastTest, &q, &reset, &created, &updated)
	if err != nil {
		return t, err
	}
	t.State = domain.TaskState(state)
	if q.Valid {
		v := q.Float64
		t.LastQuotaRemaining = &v
	}
	if reset.Valid {
		v := time.UnixMilli(reset.Int64)
		t.LastQuotaResetAt = &v
	}
	t.CreatedAt = time.UnixMilli(created)
	t.UpdatedAt = time.UnixMilli(updated)
	return t, nil
}
