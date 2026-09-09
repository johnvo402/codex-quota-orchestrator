//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

var version = "dev"

type installPaths struct {
	packageRoot     string
	installRoot     string
	installBin      string
	dataDir         string
	setupSource     string
	orchSource      string
	daemonSource    string
	companionSource string
	orchestrator    string
	orch            string
	daemon          string
	companion       string
	uninstaller     string
}

func main() {
	action, silent, purge := parseAction()
	if action == "install" && !silent {
		if messageBox(installPrompt(), appName+" "+normalizedVersion(version), mbYesNo|mbIconQuestion) != idYes {
			return
		}
	}
	if action == "uninstall" && !silent {
		if messageBox("Uninstall Codex Desktop Quota Guard?\n\nTask history is preserved by default.", appName, mbYesNo|mbIconQuestion) != idYes {
			return
		}
		purge = messageBox("Also delete local task history, SQLite state, and relay configuration?", appName, mbYesNo|mbIconQuestion) == idYes
	}

	var err error
	switch action {
	case "install":
		err = install(silent)
	case "uninstall":
		err = uninstall(purge)
	default:
		err = fmt.Errorf("unknown action %q; use install or uninstall", action)
	}
	if err != nil {
		if !silent {
			messageBox(err.Error(), appName+" - Error", mbOK|mbIconError)
		}
		os.Exit(1)
	}
	if silent {
		return
	}
	if action == "install" {
		messageBox(
			fmt.Sprintf("Codex Desktop Quota Guard %s installed successfully.\n\nDaemon health check: OK\n\nOpen a new terminal and use:\n  orch status\n  orch ui\n\nFully quit and reopen Codex Desktop once so it reloads the MCP companion.", normalizedVersion(version)),
			appName,
			mbOK|mbIconInfo,
		)
	} else {
		msg := "Uninstall completed. Fully restart Codex Desktop."
		if !purge {
			msg += "\n\nLocal task history was preserved."
		}
		messageBox(msg, appName, mbOK|mbIconInfo)
	}
}

func parseAction() (action string, silent, purge bool) {
	action = "install"
	if exe, err := os.Executable(); err == nil && strings.Contains(strings.ToLower(filepath.Base(exe)), "uninstall") {
		action = "uninstall"
	}
	for _, arg := range os.Args[1:] {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "install", "/install", "--install":
			action = "install"
		case "uninstall", "/uninstall", "--uninstall":
			action = "uninstall"
		case "/s", "-s", "--silent", "/silent":
			silent = true
		case "--purge-data", "/purge-data", "/purge":
			purge = true
		}
	}
	return
}

func resolvePaths() (installPaths, error) {
	exe, err := os.Executable()
	if err != nil {
		return installPaths{}, err
	}
	exe, _ = filepath.Abs(exe)
	packageRoot := filepath.Dir(exe)
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return installPaths{}, errors.New("LOCALAPPDATA is not set")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return installPaths{}, err
	}
	installRoot := filepath.Join(localAppData, "CodexQuotaGuard")
	installBin := filepath.Join(installRoot, "bin")
	return installPaths{
		packageRoot:     packageRoot,
		installRoot:     installRoot,
		installBin:      installBin,
		dataDir:         filepath.Join(home, ".codex-desktop-quota-guard"),
		setupSource:     exe,
		orchSource:      filepath.Join(packageRoot, "bin", "orchestrator.exe"),
		daemonSource:    filepath.Join(packageRoot, "bin", "orchestrator-daemon.exe"),
		companionSource: filepath.Join(packageRoot, "bin", "desktop-companion.exe"),
		orchestrator:    filepath.Join(installBin, "orchestrator.exe"),
		orch:            filepath.Join(installBin, "orch.exe"),
		daemon:          filepath.Join(installBin, "orchestrator-daemon.exe"),
		companion:       filepath.Join(installBin, "desktop-companion.exe"),
		uninstaller:     filepath.Join(installRoot, "Uninstall.exe"),
	}, nil
}

func install(silent bool) (retErr error) {
	p, err := resolvePaths()
	if err != nil {
		return err
	}
	for _, required := range []string{p.orchSource, p.daemonSource, p.companionSource} {
		if _, err := os.Stat(required); err != nil {
			return fmt.Errorf("release package is incomplete; missing %s", required)
		}
	}
	if _, err := exec.LookPath("codex"); err != nil {
		return errors.New("Codex CLI was not found in PATH. Install/login to Codex first, then run setup again")
	}
	if err := waitForCompanionExit(silent); err != nil {
		return err
	}

	backup, err := createInstallBackup(p)
	if err != nil {
		return fmt.Errorf("prepare upgrade rollback: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			backup.discard()
			return
		}
		if rbErr := rollbackInstall(p, backup); rbErr != nil {
			if retErr == nil {
				retErr = fmt.Errorf("installation failed and rollback also failed: %w", rbErr)
			} else {
				retErr = fmt.Errorf("%v\n\nRollback also failed: %v", retErr, rbErr)
			}
		}
	}()

	removeLegacyScheduledTask()
	_ = removeRunEntry()
	stopInstalledProcesses()

	if err := os.MkdirAll(p.installBin, 0o755); err != nil {
		return err
	}
	if err := copyWithRetry(p.orchSource, p.orchestrator); err != nil {
		return lockedFileError(err)
	}
	if err := copyWithRetry(p.orchSource, p.orch); err != nil {
		return lockedFileError(err)
	}
	if err := copyWithRetry(p.daemonSource, p.daemon); err != nil {
		return lockedFileError(err)
	}
	if err := copyWithRetry(p.companionSource, p.companion); err != nil {
		return lockedFileError(err)
	}
	if !sameFilePath(p.setupSource, p.uninstaller) {
		if err := copyWithRetry(p.setupSource, p.uninstaller); err != nil {
			return fmt.Errorf("install uninstaller: %w", err)
		}
	}

	if err := addUserPath(p.installBin); err != nil {
		return fmt.Errorf("add orch to user PATH: %w", err)
	}
	if err := addMCP(p.companion); err != nil {
		return err
	}
	if err := installAgentInstructions(); err != nil {
		return err
	}
	if err := ensureRelay(p.orchestrator, p.dataDir); err != nil {
		return err
	}
	if err := installRunEntry(p.daemon); err != nil {
		return err
	}
	if err := registerUninstaller(p); err != nil {
		return err
	}
	if err := startBackgroundDaemon(p.daemon); err != nil {
		return fmt.Errorf("start background daemon: %w", err)
	}
	if err := waitForDaemonHealth(6 * time.Second); err != nil {
		return fmt.Errorf("daemon failed post-install health check: %w", err)
	}

	broadcastEnvironmentChange()
	committed = true
	return nil
}

func uninstall(purge bool) error {
	p, err := resolvePaths()
	if err != nil {
		return err
	}
	_ = removeMCP()
	removeLegacyScheduledTask()
	_ = removeRunEntry()
	stopInstalledProcesses()
	_ = removeAgentInstructions()
	_ = removeUserPath(p.installBin)
	_ = registry.DeleteKey(registry.CURRENT_USER, uninstallKey)
	broadcastEnvironmentChange()

	if purge {
		_ = os.RemoveAll(p.dataDir)
	}

	current, _ := os.Executable()
	if sameOrChildPath(current, p.installRoot) {
		if err := scheduleDirectoryRemoval(p.installRoot); err != nil {
			return fmt.Errorf("schedule install directory cleanup: %w", err)
		}
	} else {
		_ = os.RemoveAll(p.installRoot)
	}
	return nil
}
