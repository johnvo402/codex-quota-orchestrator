package store

import (
	"context"
	"strings"
)

// LatestRecoveryActionInfo returns the newest action whose outcome can require
// manual intervention. It deliberately excludes pause notices and project task
// actions, which have their own recovery linkage.
func (s *Store) LatestRecoveryActionInfo(ctx context.Context, threadID string) (ActionInfo, error) {
	threadID = strings.TrimSpace(threadID)
	return scanActionInfo(s.db.QueryRowContext(ctx, `
SELECT id,kind,thread_id,status,created_at,updated_at
FROM actions
WHERE thread_id=? AND kind IN ('resume','stop')
ORDER BY id DESC
LIMIT 1
`, threadID))
}
