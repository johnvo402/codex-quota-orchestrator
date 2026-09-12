package observability

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type ProcessLifecycleMarker struct {
	RunID      string `json:"runId"`
	Component  string `json:"component"`
	PID        int    `json:"pid"`
	ParentPID  int    `json:"parentPid"`
	StartedAt  string `json:"startedAt"`
	CleanExit  bool   `json:"cleanExit"`
	ExitReason string `json:"exitReason,omitempty"`
	ExitedAt   string `json:"exitedAt,omitempty"`
}

func LifecycleMarkerPath(dataDir, component string) string {
	return filepath.Join(LogDir(dataDir), component+"-lifecycle.json")
}

func LifecycleEventPath(dataDir, component string) string {
	return filepath.Join(LogDir(dataDir), component+"-lifecycle.log")
}

// BeginProcessLifecycle records the current process before normal daemon work
// begins. If the previous marker was never closed cleanly, the next process can
// report that fact even when Windows terminated the previous process too quickly
// for it to emit a final log line.
func BeginProcessLifecycle(dataDir, component string) (ProcessLifecycleMarker, *ProcessLifecycleMarker, error) {
	if component == "" {
		return ProcessLifecycleMarker{}, nil, errors.New("lifecycle component is required")
	}
	if err := os.MkdirAll(LogDir(dataDir), 0o700); err != nil {
		return ProcessLifecycleMarker{}, nil, err
	}

	var previous *ProcessLifecycleMarker
	markerPath := LifecycleMarkerPath(dataDir, component)
	if b, err := os.ReadFile(markerPath); err == nil {
		var p ProcessLifecycleMarker
		if json.Unmarshal(b, &p) == nil {
			previous = &p
		}
	} else if !os.IsNotExist(err) {
		return ProcessLifecycleMarker{}, nil, err
	}

	now := time.Now().UTC()
	current := ProcessLifecycleMarker{
		RunID:     fmt.Sprintf("%d-%d", os.Getpid(), now.UnixNano()),
		Component: component,
		PID:       os.Getpid(),
		ParentPID: os.Getppid(),
		StartedAt: now.Format(time.RFC3339Nano),
		CleanExit: false,
	}
	if err := writeLifecycleMarker(markerPath, current); err != nil {
		return ProcessLifecycleMarker{}, previous, err
	}
	if err := appendLifecycleEvent(dataDir, current, "started"); err != nil {
		return current, previous, err
	}
	if previous != nil && !previous.CleanExit {
		_ = appendLifecycleEvent(dataDir, *previous, "previous_unclean_exit")
	}
	return current, previous, nil
}

// MarkProcessClean closes the marker only when it still belongs to the current
// process. This prevents a stale companion or shutdown callback from marking a
// newer daemon instance clean by accident.
func MarkProcessClean(dataDir, component, reason string) error {
	path := LifecycleMarkerPath(dataDir, component)
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var marker ProcessLifecycleMarker
	if err := json.Unmarshal(b, &marker); err != nil {
		return err
	}
	if marker.PID != os.Getpid() {
		return fmt.Errorf("lifecycle marker belongs to pid %d, current pid is %d", marker.PID, os.Getpid())
	}
	marker.CleanExit = true
	marker.ExitReason = reason
	marker.ExitedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeLifecycleMarker(path, marker); err != nil {
		return err
	}
	return appendLifecycleEvent(dataDir, marker, "clean_exit")
}

func writeLifecycleMarker(path string, marker ProcessLifecycleMarker) error {
	b, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func appendLifecycleEvent(dataDir string, marker ProcessLifecycleMarker, event string) error {
	entry := struct {
		Time  string                 `json:"time"`
		Event string                 `json:"event"`
		Data  ProcessLifecycleMarker `json:"data"`
	}{
		Time:  time.Now().UTC().Format(time.RFC3339Nano),
		Event: event,
		Data:  marker,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(LifecycleEventPath(dataDir, marker.Component), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}
