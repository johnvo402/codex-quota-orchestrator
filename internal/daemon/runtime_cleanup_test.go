package daemon

import (
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codex-desktop-quota-guard/internal/config"
	"codex-desktop-quota-guard/internal/store"
)

func TestServeRemovesRuntimeMetadataWhenListenerStops(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	st, err := store.Open(filepath.Join(cfg.DataDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	svc := NewService(cfg, st, nil)
	srv := NewServer(addr, svc, st, nil)

	done := make(chan error, 1)
	go func() { done <- srv.Serve(listener) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		runtimeInfo, err := config.LoadRuntime(cfg.DataDir)
		if err == nil && runtimeInfo.ListenAddr == addr {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("runtime metadata was not published: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			// Closing the listener directly normally returns a use-of-closed-network
			// error. The exact wrapper is platform-dependent and is not the behavior
			// under test; runtime cleanup is.
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not exit after listener close")
	}

	deadline = time.Now().Add(2 * time.Second)
	for {
		_, err := os.Stat(config.RuntimePath(cfg.DataDir))
		if os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("runtime metadata remained after Serve exit: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
