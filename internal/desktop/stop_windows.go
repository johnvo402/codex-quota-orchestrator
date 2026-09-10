//go:build windows

package desktop

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// NavigateAndStop opens the requested Codex Desktop thread through the native
// codex_app pipe, then invokes the visible Desktop Stop button with Windows UI
// Automation. It never sends a prompt to the thread.
//
// attempted reports whether UI Automation reached the point where Invoke() was
// called. Callers must treat attempted=true + err!=nil as an uncertain stop
// outcome because Desktop may have accepted the click before the local process
// observed the result.
func NavigateAndStop(ctx context.Context, sender NativeSender, targetThreadID string) (attempted bool, err error) {
	targetThreadID = strings.TrimSpace(targetThreadID)
	if targetThreadID == "" {
		return false, errors.New("target thread ID is empty")
	}

	windowsSender, ok := sender.(*windowsNativeSender)
	if !ok || windowsSender == nil {
		return false, errors.New("Desktop native sender is not the Windows implementation")
	}
	if !windowsSender.Available() {
		return false, fmt.Errorf("Desktop native delivery unavailable: %s", windowsSender.Description())
	}

	// Navigation is safe to retry: it changes only which Desktop thread is
	// visible. The actual Stop side effect happens only after the UI Automation
	// script has found exactly one allow-listed button in exactly one visible
	// Codex Desktop window.
	if err := windowsSender.callTool(ctx, "navigate_to_codex_page", map[string]any{
		"threadId": targetThreadID,
	}); err != nil {
		return false, fmt.Errorf("navigate Codex Desktop to target thread: %w", err)
	}

	stopCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	cmd := exec.CommandContext(stopCtx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", desktopStopPowerShell)
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
			return true, fmt.Errorf("Desktop Stop invocation outcome is uncertain: %w", stopCtx.Err())
		}
		return false, fmt.Errorf("Desktop Stop UI Automation timed out before invoking Stop: %w", stopCtx.Err())
	}
	if attempted {
		return true, fmt.Errorf("Desktop Stop invocation outcome is uncertain: %v; output=%s", runErr, truncateString(text, 800))
	}
	return false, fmt.Errorf("Desktop Stop UI Automation failed before invoking Stop: %v; output=%s", runErr, truncateString(text, 800))
}

// Keep the selector deliberately narrow. A translated/renamed button should
// fail closed instead of risking a click on an unrelated control. Multiple
// visible Codex windows are also rejected because UI Automation cannot prove
// which window owns the requested thread id.
const desktopStopPowerShell = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes

$allowed = @('Stop', 'Stop task', 'Stop response', 'Stop generating')
$deadline = [DateTime]::UtcNow.AddSeconds(3)

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
    $patternObject = $null
    if (-not $matches[0].TryGetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern, [ref]$patternObject)) {
      throw 'Matched Stop button does not expose InvokePattern'
    }
    Write-Output 'CDQG_STOP_ATTEMPTING'
    ([System.Windows.Automation.InvokePattern]$patternObject).Invoke()
    Write-Output 'CDQG_STOP_OK'
    exit 0
  }

  Start-Sleep -Milliseconds 150
}

Write-Output 'CDQG_STOP_NOT_FOUND'
exit 3
`
