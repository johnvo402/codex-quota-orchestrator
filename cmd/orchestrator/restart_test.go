package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codex-desktop-quota-guard/internal/config"
)

func TestRequestDaemonRestartUsesRuntimeDiscovery(t *testing.T) {
	var gotHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-CDQG-Control")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer ts.Close()

	dir := t.TempDir()
	addr := strings.TrimPrefix(ts.URL, "http://")
	if err := config.SaveRuntime(dir, config.DaemonRuntime{
		ListenAddr: addr,
		PID:        42,
		StartedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.DataDir = dir
	cfg.ListenAddr = "127.0.0.1:59998"
	base, err := requestDaemonRestart(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if base != ts.URL {
		t.Fatalf("expected runtime endpoint %s, got %s", ts.URL, base)
	}
	if gotHeader != "restart" {
		t.Fatalf("missing restart control header: %q", gotHeader)
	}
}

func TestRequestDaemonShutdownUsesRuntimeDiscovery(t *testing.T) {
	var gotHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-CDQG-Control")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer ts.Close()

	dir := t.TempDir()
	addr := strings.TrimPrefix(ts.URL, "http://")
	if err := config.SaveRuntime(dir, config.DaemonRuntime{
		ListenAddr: addr,
		PID:        42,
		StartedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.DataDir = dir
	cfg.ListenAddr = "127.0.0.1:59998"
	base, err := requestDaemonShutdown(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if base != ts.URL {
		t.Fatalf("expected runtime endpoint %s, got %s", ts.URL, base)
	}
	if gotHeader != "shutdown" {
		t.Fatalf("missing shutdown control header: %q", gotHeader)
	}
}
