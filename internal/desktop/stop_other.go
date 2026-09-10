//go:build !windows

package desktop

import (
	"context"
	"errors"
)

func NavigateAndStop(context.Context, NativeSender, string, string) (bool, error) {
	return false, errors.New("Codex Desktop Stop automation is Windows-only")
}
