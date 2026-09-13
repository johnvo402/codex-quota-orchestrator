package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type projectDispatchLink struct {
	itemID    string
	actionID  sql.NullInt64
	kind      sql.NullString
	threadID  sql.NullString
	status    sql.NullString
	errorText sql.NullString
}

// ReconcileProjectTaskDispatches repairs durable queue/action mismatches that
// would otherwise leave a project blocked in DISPATCHING forever. Pending and
// actively delivering actions are left untouched because the delivery worker
// still owns those states. Terminal action states are projected back onto the
// queue row using the same safety rules as normal delivery completion.
func (s *Store) ReconcileProjectTaskDispatches(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT pt.id, pt.action_id, a.kind, a.thread_id, a.status, a.error_text
FROM project_tasks pt
LEFT JOIN actions a ON a.id=pt.action_id
WHERE pt.state='DISPATCHING'
ORDER BY pt.updated_at, pt.id
`)
	if err != nil {
		return 0, err
	}
	var links []projectDispatchLink
	for rows.Next() {
		var link projectDispatchLink
		if err := rows.Scan(&link.itemID, &link.actionID, &link.kind, &link.threadID, &link.status, &link.errorText); err != nil {
			_ = rows.Close()
			return 0, err
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	repaired := 0
	for _, link := range links {
		changed, err := s.reconcileProjectTaskDispatch(ctx, link)
		if err != nil {
			return repaired, err
		}
		if changed {
			repaired++
		}
	}
	return repaired, nil
}

func (s *Store) reconcileProjectTaskDispatch(ctx context.Context, link projectDispatchLink) (bool, error) {
	if !link.actionID.Valid || !link.kind.Valid || !link.status.Valid {
		return s.moveDispatchingProjectTaskToReview(ctx, link.itemID, "DISPATCHING project task has no valid linked Desktop action")
	}
	if !isProjectTaskAction(link.kind.String) || projectTaskIDFromActionKind(link.kind.String) != link.itemID {
		return s.moveDispatchingProjectTaskToReview(ctx, link.itemID, "DISPATCHING project task has an inconsistent Desktop action linkage")
	}

	switch link.status.String {
	case "pending", "delivering":
		return false, nil
	case "done", "failed":
		if !link.threadID.Valid || strings.TrimSpace(link.threadID.String) == "" {
			return s.moveDispatchingProjectTaskToReview(ctx, link.itemID, "terminal Desktop action is missing its target thread")
		}
		success := link.status.String == "done"
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return false, err
		}
		now := time.Now().UTC().UnixMilli()
		err = completeProjectTaskActionTx(ctx, tx, link.kind.String, link.threadID.String, success, link.errorText.String, now)
		if err == nil {
			err = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
		if err != nil {
			reason := fmt.Sprintf("Desktop action is %s but queue activation could not be reconciled: %v", link.status.String, err)
			return s.moveDispatchingProjectTaskToReview(ctx, link.itemID, reason)
		}
		return true, nil
	case "uncertain":
		reason := strings.TrimSpace(link.errorText.String)
		if reason == "" {
			reason = "Desktop dispatch outcome is uncertain"
		}
		return s.moveDispatchingProjectTaskToReview(ctx, link.itemID, reason)
	case "cancelled":
		now := time.Now().UTC().UnixMilli()
		res, err := s.db.ExecContext(ctx, `
UPDATE project_tasks
SET state='CANCELLED', updated_at=?, completed_at=COALESCE(completed_at,?)
WHERE id=? AND state='DISPATCHING'
`, now, now, link.itemID)
		if err != nil {
			return false, err
		}
		n, err := res.RowsAffected()
		return n > 0, err
	default:
		return s.moveDispatchingProjectTaskToReview(ctx, link.itemID, "DISPATCHING project task has unsupported Desktop action status "+link.status.String)
	}
}

func (s *Store) moveDispatchingProjectTaskToReview(ctx context.Context, itemID, reason string) (bool, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "Desktop dispatch state requires manual review"
	}
	now := time.Now().UTC().UnixMilli()
	res, err := s.db.ExecContext(ctx, `
UPDATE project_tasks
SET state='NEEDS_REVIEW',
    details=CASE WHEN details='' THEN ? ELSE details || char(10) || ? END,
    updated_at=?
WHERE id=? AND state='DISPATCHING'
`, reason, reason, now, itemID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
