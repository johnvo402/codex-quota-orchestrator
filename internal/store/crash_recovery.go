package store

import "context"

// RecoverInterruptedDeliveries is intended for daemon startup. Any action that
// is still marked delivering must have been claimed by the previous daemon
// process, so its Desktop delivery outcome can no longer be proven. Move those
// actions to the existing uncertain/NEEDS_REVIEW path immediately instead of
// waiting for the periodic stale-delivery timeout.
//
// Pending actions are deliberately left alone: they were never claimed and can
// safely be delivered after the daemon comes back up.
func (s *Store) RecoverInterruptedDeliveries(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id
FROM actions
WHERE status='delivering'
ORDER BY id
`)
	if err != nil {
		return 0, err
	}

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	recovered := 0
	for _, id := range ids {
		if err := s.MarkActionUncertain(ctx, id, "daemon restarted while Desktop delivery was in progress; delivery outcome is unknown"); err != nil {
			return recovered, err
		}
		recovered++
	}
	return recovered, nil
}
