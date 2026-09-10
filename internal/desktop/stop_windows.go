//go:build windows

package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const (
	desktopStopAutomationTimeout = 6 * time.Second
	desktopStopVerifyTimeout     = 1800 * time.Millisecond
)

// NavigateAndStop opens the requested Codex Desktop thread through the native
// codex_app pipe, verifies that Desktop still exposes the exact expected turn,
// invokes the visible Desktop Stop control, and then verifies through
// read_thread that the backend turn actually became interrupted.
//
// attempted reports whether UI Automation reached the point where a Stop side
// effect was attempted. Callers must treat attempted=true + err!=nil as an
// uncertain outcome because Desktop may have accepted a click even when the
// backend state could not be confirmed.
func NavigateAndStop(ctx context.Context, sender NativeSender, targetThreadID, expectedTurnID string) (attempted bool, err error) {
	targetThreadID = strings.TrimSpace(targetThreadID)
	expectedTurnID = strings.TrimSpace(expectedTurnID)
	if targetThreadID == "" {
		return false, errors.New("target thread ID is empty")
	}
	if expectedTurnID == "" {
		return false, errors.New("expected turn ID is empty")
	}

	windowsSender, ok := sender.(*windowsNativeSender)
	if !ok || windowsSender == nil {
		return false, errors.New("Desktop native sender is not the Windows implementation")
	}
	if !windowsSender.Available() {
		return false, fmt.Errorf("Desktop native delivery unavailable: %s", windowsSender.Description())
	}

	// Navigation is safe to retry: it changes only which Desktop thread is
	// visible. The actual Stop side effect happens only after exact-turn checks.
	if err := windowsSender.callTool(ctx, "navigate_to_codex_page", map[string]any{
		"threadId": targetThreadID,
	}); err != nil {
		return false, fmt.Errorf("navigate Codex Desktop to target thread: %w", err)
	}

	if err := requireExpectedRunningTurn(ctx, windowsSender, targetThreadID, expectedTurnID); err != nil {
		return false, fmt.Errorf("verify target Desktop turn before Stop: %w", err)
	}

	// First use the accessibility-native InvokePattern. It is non-invasive and
	// does not require moving the user's mouse or foregrounding another window.
	firstAttempted, err := runDesktopStopAutomation(ctx, "invoke")
	if err != nil {
		return firstAttempted, err
	}
	attempted = firstAttempted

	interrupted, status, verifyErr := waitForExpectedTurnInterrupted(
		ctx,
		windowsSender,
		targetThreadID,
		expectedTurnID,
		desktopStopVerifyTimeout,
	)
	if interrupted {
		return true, nil
	}
	if verifyErr != nil {
		return true, fmt.Errorf("Desktop Stop was invoked but backend verification is uncertain: %w", verifyErr)
	}

	// Some Chromium/Electron accessibility bridges acknowledge InvokePattern
	// without producing the same result as a physical user click. If the exact
	// same turn is still inProgress, retry once with a guarded real click on the
	// freshly rediscovered, unique Stop button. This is still fail-closed: the
	// script must foreground the exact Codex window and obtain a clickable point.
	if !isTurnInProgress(status) {
		return true, fmt.Errorf("Desktop Stop was invoked but turn %s ended with status %q instead of interrupted", expectedTurnID, status)
	}
	if err := requireExpectedRunningTurn(ctx, windowsSender, targetThreadID, expectedTurnID); err != nil {
		return true, fmt.Errorf("Desktop Stop InvokePattern did not confirm interruption and fallback was blocked: %w", err)
	}

	fallbackAttempted, fallbackErr := runDesktopStopAutomation(ctx, "click")
	attempted = attempted || fallbackAttempted
	if fallbackErr != nil {
		return attempted, fmt.Errorf("Desktop Stop InvokePattern left turn inProgress and guarded click fallback failed: %w", fallbackErr)
	}

	interrupted, status, verifyErr = waitForExpectedTurnInterrupted(
		ctx,
		windowsSender,
		targetThreadID,
		expectedTurnID,
		desktopStopVerifyTimeout,
	)
	if interrupted {
		return true, nil
	}
	if verifyErr != nil {
		return true, fmt.Errorf("Desktop Stop click was attempted but backend verification is uncertain: %w", verifyErr)
	}
	return true, fmt.Errorf(
		"Codex Desktop Stop controls were invoked but turn %s is still %q; the Desktop Stop control may be nonfunctional",
		expectedTurnID,
		status,
	)
}

func requireExpectedRunningTurn(ctx context.Context, sender *windowsNativeSender, targetThreadID, expectedTurnID string) error {
	latestTurnID, status, err := sender.LatestTurn(ctx, targetThreadID)
	if err != nil {
		return err
	}
	latestTurnID = strings.TrimSpace(latestTurnID)
	status = strings.TrimSpace(status)
	if latestTurnID != expectedTurnID {
		return fmt.Errorf("Desktop turn changed: expected %s, latest %s", expectedTurnID, latestTurnID)
	}
	if !isTurnInProgress(status) {
		return fmt.Errorf("Desktop turn %s is not running; status=%q", expectedTurnID, status)
	}
	return nil
}

func waitForExpectedTurnInterrupted(
	ctx context.Context,
	sender *windowsNativeSender,
	targetThreadID string,
	expectedTurnID string,
	wait time.Duration,
) (interrupted bool, lastStatus string, err error) {
	deadline := time.Now().Add(wait)
	for {
		latestTurnID, status, readErr := sender.LatestTurn(ctx, targetThreadID)
		if readErr != nil {
			return false, lastStatus, readErr
		}
		latestTurnID = strings.TrimSpace(latestTurnID)
		status = strings.TrimSpace(status)
		lastStatus = status
		if latestTurnID != expectedTurnID {
			return false, status, fmt.Errorf("Desktop turn changed during Stop verification: expected %s, latest %s", expectedTurnID, latestTurnID)
		}
		if isTurnInterrupted(status) {
			return true, status, nil
		}
		if !isTurnInProgress(status) {
			return false, status, nil
		}
		if time.Now().After(deadline) {
			return false, status, nil
		}
		select {
		case <-ctx.Done():
			return false, status, ctx.Err()
		case <-time.After(180 * time.Millisecond):
		}
	}
}

func isTurnInProgress(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "inProgress")
}

func isTurnInterrupted(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "interrupted")
}

func runDesktopStopAutomation(ctx context.Context, mode string) (attempted bool, err error) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode != "invoke" && mode != "click" {
		return false, fmt.Errorf("unsupported Desktop Stop automation mode %q", mode)
	}

	stopCtx, cancel := context.WithTimeout(ctx, desktopStopAutomationTimeout)
	defer cancel()

	cmd := exec.CommandContext(stopCtx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", desktopStopPowerShell)
	cmd.Env = append(os.Environ(), "CDQG_STOP_MODE="+mode)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	output, runErr := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	attempted = strings.Contains(text, "CDQG_STOP_ATTEMPTING") || strings.Contains(text, "CDQG_STOP_OK")
	if runErr == nil && strings.Contains(text, "CDQG_STOP_OK") {
		return true, nil
	}

	if strings.Contains(text, "CDQG_STOP_NOT_FOUND") {
		return false, errors.New("Codex Desktop Stop button was not found after navigation")
	}
	if strings.Contains(text, "CDQG_STOP_AMBIGUOUS") {
		return false, errors.New("Desktop Stop target is ambiguous; refusing to click")
	}
	if stopCtx.Err() != nil {
		if attempted {
			return true, fmt.Errorf("Desktop Stop %s outcome is uncertain: %w", mode, stopCtx.Err())
		}
		return false, fmt.Errorf("Desktop Stop %s automation timed out before attempting Stop: %w", mode, stopCtx.Err())
	}
	if attempted {
		return true, fmt.Errorf("Desktop Stop %s outcome is uncertain: %v; output=%s", mode, runErr, truncateString(text, 800))
	}
	return false, fmt.Errorf("Desktop Stop %s automation failed before attempting Stop: %v; output=%s", mode, runErr, truncateString(text, 800))
}

// Keep the selector deliberately narrow. A translated/renamed button should
// fail closed instead of risking a click on an unrelated control. Multiple
// visible Codex windows are also rejected because UI Automation cannot prove
// which window owns the requested thread id.
//
// invoke mode uses InvokePattern first. click mode is a one-shot fallback that
// foregrounds the unique Codex window, reacquires the unique Stop button, gets
// its UI Automation clickable point, performs one physical click, then restores
// the user's cursor position.
const desktopStopPowerShell = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes

$mode = [string]$env:CDQG_STOP_MODE
if ($mode -ne 'invoke' -and $mode -ne 'click') {
  throw "Unsupported CDQG_STOP_MODE: $mode"
}

if ($mode -eq 'click') {
  Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;
public static class CDQGMouse {
  [StructLayout(LayoutKind.Sequential)]
  public struct POINT { public int X; public int Y; }
  [DllImport("user32.dll")] public static extern bool GetCursorPos(out POINT point);
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
  [DllImport("user32.dll")] public static extern void mouse_event(uint flags, uint dx, uint dy, uint data, UIntPtr extraInfo);
}
"@
}

$allowed = @('Stop', 'Stop task', 'Stop response', 'Stop generating')
$deadline = [DateTime]::UtcNow.AddSeconds(3)
$foregrounded = $false

while ([DateTime]::UtcNow -lt $deadline) {
  $pids = @(Get-Process -ErrorAction SilentlyContinue | Where-Object {
    $_.ProcessName -eq 'Codex'
  } | Select-Object -ExpandProperty Id)

  $root = [System.Windows.Automation.AutomationElement]::RootElement
  $top = @()
  if ($pids.Count -gt 0) {
    $windows = $root.FindAll(
      [System.Windows.Automation.TreeScope]::Children,
      [System.Windows.Automation.Condition]::TrueCondition
    )
    foreach ($window in $windows) {
      if ($pids -notcontains $window.Current.ProcessId) { continue }
      if ($window.Current.ControlType -ne [System.Windows.Automation.ControlType]::Window) { continue }
      if ($window.Current.IsOffscreen) { continue }
      $top += $window
    }
  }

  if ($top.Count -gt 1) {
    Write-Output 'CDQG_STOP_AMBIGUOUS'
    exit 4
  }

  $matches = @()
  if ($top.Count -eq 1) {
    $buttons = $top[0].FindAll(
      [System.Windows.Automation.TreeScope]::Descendants,
      [System.Windows.Automation.Condition]::TrueCondition
    )
    foreach ($button in $buttons) {
      if ($button.Current.ControlType -ne [System.Windows.Automation.ControlType]::Button) { continue }
      if (-not $button.Current.IsEnabled -or $button.Current.IsOffscreen) { continue }
      $name = [string]$button.Current.Name
      if ($allowed -contains $name) { $matches += $button }
    }
  }

  if ($matches.Count -gt 1) {
    Write-Output 'CDQG_STOP_AMBIGUOUS'
    exit 4
  }

  if ($matches.Count -eq 1) {
    if ($mode -eq 'invoke') {
      $patternObject = $null
      if (-not $matches[0].TryGetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern, [ref]$patternObject)) {
        throw 'Matched Stop button does not expose InvokePattern'
      }
      Write-Output 'CDQG_STOP_ATTEMPTING:invoke'
      ([System.Windows.Automation.InvokePattern]$patternObject).Invoke()
      Write-Output 'CDQG_STOP_OK:invoke'
      exit 0
    }

    $handle = [IntPtr]$top[0].Current.NativeWindowHandle
    if ($handle -eq [IntPtr]::Zero) {
      throw 'Matched Codex window has no native handle'
    }

    if (-not $foregrounded) {
      if (-not [CDQGMouse]::SetForegroundWindow($handle)) {
        throw 'Could not foreground the Codex Desktop window for guarded click fallback'
      }
      $foregrounded = $true
      Start-Sleep -Milliseconds 150
      continue
    }

    $point = New-Object System.Windows.Point
    if (-not $matches[0].TryGetClickablePoint([ref]$point)) {
      throw 'Matched Stop button has no clickable point'
    }
    $rect = $matches[0].Current.BoundingRectangle
    if ($point.X -lt $rect.Left -or $point.X -gt $rect.Right -or $point.Y -lt $rect.Top -or $point.Y -gt $rect.Bottom) {
      throw 'Stop clickable point is outside the matched button bounds'
    }

    $cursor = New-Object CDQGMouse+POINT
    if (-not [CDQGMouse]::GetCursorPos([ref]$cursor)) {
      throw 'Could not capture cursor position before guarded Stop click'
    }

    Write-Output 'CDQG_STOP_ATTEMPTING:click'
    if (-not [CDQGMouse]::SetCursorPos([int]$point.X, [int]$point.Y)) {
      throw 'Could not move cursor to guarded Stop click point'
    }
    Start-Sleep -Milliseconds 40
    [CDQGMouse]::mouse_event(0x0002, 0, 0, 0, [UIntPtr]::Zero)
    [CDQGMouse]::mouse_event(0x0004, 0, 0, 0, [UIntPtr]::Zero)
    Start-Sleep -Milliseconds 40
    [void][CDQGMouse]::SetCursorPos($cursor.X, $cursor.Y)
    Write-Output 'CDQG_STOP_OK:click'
    exit 0
  }

  Start-Sleep -Milliseconds 150
}

Write-Output 'CDQG_STOP_NOT_FOUND'
exit 3
`
