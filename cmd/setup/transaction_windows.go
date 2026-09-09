//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

type installBackup struct {
	existed        bool
	version        string
	backupDir      string
	agentsPath     string
	agentsExisted  bool
	agentsBackup   string
	files          map[string]string
}

func createInstallBackup(p installPaths) (installBackup, error) {
	b := installBackup{
		version:   installedVersion(),
		backupDir: filepath.Join(p.installRoot, ".upgrade-backup"),
		files:     map[string]string{},
	}
	if _, err := os.Stat(p.installRoot); err == nil {
		b.existed = true
	}
	_ = os.RemoveAll(b.backupDir)
	if err := os.MkdirAll(b.backupDir, 0o755); err != nil {
		return b, err
	}

	for _, src := range []string{p.orchestrator, p.orch, p.daemon, p.companion, p.uninstaller} {
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := filepath.Join(b.backupDir, filepath.Base(src))
		if err := copyFile(src, dst); err != nil {
			return b, fmt.Errorf("backup %s: %w", filepath.Base(src), err)
		}
		b.files[src] = dst
	}

	if ap, err := agentsPath(); err == nil {
		b.agentsPath = ap
		if _, err := os.Stat(ap); err == nil {
			b.agentsExisted = true
			b.agentsBackup = filepath.Join(b.backupDir, "AGENTS.md")
			if err := copyFile(ap, b.agentsBackup); err != nil {
				return b, fmt.Errorf("backup AGENTS.md: %w", err)
			}
		}
	}
	return b, nil
}

func (b installBackup) discard() {
	_ = os.RemoveAll(b.backupDir)
}

func rollbackInstall(p installPaths, b installBackup) error {
	stopInstalledProcesses()
	_ = removeMCP()
	_ = removeRunEntry()
	_ = removeUserPath(p.installBin)
	_ = removeAgentInstructions()

	if !b.existed {
		_ = os.RemoveAll(p.installRoot)
		broadcastEnvironmentChange()
		return nil
	}

	for dst, src := range b.files {
		if err := copyWithRetry(src, dst); err != nil {
			return fmt.Errorf("restore %s: %w", filepath.Base(dst), err)
		}
	}
	if b.agentsPath != "" {
		if b.agentsExisted {
			if err := copyWithRetry(b.agentsBackup, b.agentsPath); err != nil {
				return fmt.Errorf("restore AGENTS.md: %w", err)
			}
		} else {
			_ = os.Remove(b.agentsPath)
		}
	}
	if err := addUserPath(p.installBin); err != nil {
		return err
	}
	if err := addMCP(p.companion); err != nil {
		return err
	}
	if err := installRunEntry(p.daemon); err != nil {
		return err
	}
	if b.version != "" {
		if err := registerUninstallerVersion(p, b.version); err != nil {
			return err
		}
	}
	if _, err := os.Stat(p.daemon); err == nil {
		if err := startBackgroundDaemon(p.daemon); err != nil {
			return err
		}
	}
	broadcastEnvironmentChange()
	return nil
}
