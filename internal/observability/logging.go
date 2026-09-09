package observability

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

const (
	DefaultMaxLogBytes = int64(5 * 1024 * 1024)
	DefaultLogBackups  = 3
)

func LogDir(dataDir string) string {
	return filepath.Join(dataDir, "logs")
}

func LogPath(dataDir, component string) string {
	return filepath.Join(LogDir(dataDir), component+".log")
}

// NewLogger creates a JSON-lines slog logger backed by a rotating file. stderr
// may be nil for fully background processes; when present, records are mirrored
// there as JSON as well.
func NewLogger(dataDir, component string, stderr io.Writer) (*slog.Logger, io.Closer, error) {
	if component == "" {
		return nil, nil, fmt.Errorf("log component is required")
	}
	if err := os.MkdirAll(LogDir(dataDir), 0o700); err != nil {
		return nil, nil, fmt.Errorf("create log directory: %w", err)
	}

	rot, err := OpenRotatingFile(LogPath(dataDir, component), DefaultMaxLogBytes, DefaultLogBackups)
	if err != nil {
		return nil, nil, err
	}

	var out io.Writer = rot
	if stderr != nil {
		out = io.MultiWriter(rot, stderr)
	}
	handler := slog.NewJSONHandler(out, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(handler).With("component", component), rot, nil
}

type RotatingFile struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	backups  int
	file     *os.File
	size     int64
}

func OpenRotatingFile(path string, maxBytes int64, backups int) (*RotatingFile, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("maxBytes must be positive")
	}
	if backups < 0 {
		return nil, fmt.Errorf("backups cannot be negative")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	r := &RotatingFile{path: path, maxBytes: maxBytes, backups: backups}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *RotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.file == nil {
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	if r.size > 0 && r.size+int64(len(p)) > r.maxBytes {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := r.file.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *RotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}

func (r *RotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open log %s: %w", r.path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	r.file = f
	r.size = info.Size()
	return nil
}

func (r *RotatingFile) rotate() error {
	if r.file != nil {
		if err := r.file.Close(); err != nil {
			return err
		}
		r.file = nil
	}

	if r.backups > 0 {
		_ = os.Remove(fmt.Sprintf("%s.%d", r.path, r.backups))
		for i := r.backups - 1; i >= 1; i-- {
			oldPath := fmt.Sprintf("%s.%d", r.path, i)
			newPath := fmt.Sprintf("%s.%d", r.path, i+1)
			if err := os.Rename(oldPath, newPath); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if err := os.Rename(r.path, r.path+".1"); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else {
		_ = os.Remove(r.path)
	}

	r.size = 0
	return r.open()
}
