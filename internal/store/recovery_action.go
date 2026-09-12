package store

import (
	"context"
	"strings"
)

// LatestRecoveryActionInfo returns the newest managed resume action whose
// outcome can require manual intervention. Pause notices and project task
// actions have separate recovery linkage.
func (s *Store) LatestRecoveryActionInfo(ctx context.Context, threadID string) (ActionInfo, error) {
	threadID = strings.TrimSpace(threadID)
	return scanActionInfo(s.db.QueryRowContext(ctx, `
SELECT id,kind,thread_id,status,created_at,updated_at
FROM actions
WHERE thread_id=? AND kind='resume'
ORDER BY id DESC
LIMIT 1
`, threadID))
}
