package observability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewLoggerWritesStructuredJSON(t *testing.T) {
	dir := t.TempDir()
	log, closer, err := NewLogger(dir, "daemon", nil)
	if err != nil {
		t.Fatal(err)
	}
	log.Info("started", "listen", "127.0.0.1:47631")
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(LogPath(dir, "daemon"))
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(b))), &record); err != nil {
		t.Fatalf("log is not JSON: %v\n%s", err, b)
	}
	if record["component"] != "daemon" || record["msg"] != "started" || record["level"] != "INFO" {
		t.Fatalf("unexpected record: %#v", record)
	}
	if record["listen"] != "127.0.0.1:47631" {
		t.Fatalf("missing structured field: %#v", record)
	}
}

func TestRotatingFileKeepsBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.log")
	r, err := OpenRotatingFile(path, 12, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"aaaaaa\n", "bbbbbb\n", "cccccc\n", "dddddd\n"} {
		if _, err := r.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, path + ".1", path + ".2"} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected rotated file %s: %v", p, err)
		}
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf("unexpected extra backup: %v", err)
	}
}
