package observability

import (
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"all": slog.LevelDebug,
		"info": slog.LevelInfo,
		"warn": slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error": slog.LevelError,
	}
	for input, want := range cases {
		got, err := ParseLevel(input)
		if err != nil || got != want {
			t.Fatalf("ParseLevel(%q) = %v, %v; want %v", input, got, err, want)
		}
	}
	if _, err := ParseLevel("fatal"); err == nil {
		t.Fatal("expected invalid level error")
	}
}

func TestReadRecordsFiltersAndTailsAcrossComponents(t *testing.T) {
	dir := t.TempDir()
	daemon := strings.Join([]string{
		`{"time":"2026-09-10T01:00:00Z","level":"INFO","msg":"a","component":"daemon"}`,
		`{"time":"2026-09-10T01:01:00Z","level":"ERROR","msg":"b","component":"daemon","error":"boom"}`,
	}, "\n") + "\n"
	companion := `{"time":"2026-09-10T01:02:00Z","level":"WARN","msg":"c","component":"companion"}` + "\n"
	if err := os.MkdirAll(LogDir(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LogPath(dir, "daemon"), []byte(daemon), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LogPath(dir, "companion"), []byte(companion), 0o600); err != nil {
		t.Fatal(err)
	}

	records, err := ReadRecords(dir, []string{"daemon", "companion"}, slog.LevelWarn, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Message != "b" || records[1].Message != "c" {
		t.Fatalf("unexpected records: %#v", records)
	}
}
