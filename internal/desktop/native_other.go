//go:build !windows

package desktop

import (
	"context"
	"errors"
)

type unsupportedNativeSender struct{}

func newPlatformNativeSender(string) NativeSender { return unsupportedNativeSender{} }
func (unsupportedNativeSender) Available() bool   { return false }
func (unsupportedNativeSender) Description() string {
	return "Desktop native relay in this MVP is Windows-only"
}
func (unsupportedNativeSender) SendMessage(context.Context, string, string) error {
	return errors.New("Desktop native relay is Windows-only in this MVP")
}
