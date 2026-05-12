package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"or3-intern/internal/config"
)

func TestServiceUnixSocketTransportUsesHTTPHandlers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets are not supported on windows")
	}
	socketDir, err := os.MkdirTemp("/tmp", "or3-sock-*")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	defer os.RemoveAll(socketDir)
	socketPath := filepath.Join(socketDir, "or3.sock")
	ctx, cancel := context.WithCancel(context.Background())
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/ping" {
				t.Fatalf("unexpected path %q", r.URL.Path)
			}
			_, _ = w.Write([]byte("pong"))
		}),
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- serveHTTPWithConfiguredTransport(ctx, server, config.Config{Service: config.ServiceConfig{UnixSocket: socketPath}})
	}()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("unix socket was not created")
		}
		time.Sleep(10 * time.Millisecond)
	}
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}}
	resp, err := client.Get("http://unix/ping")
	if err != nil {
		cancel()
		t.Fatalf("GET over unix socket: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("serve returned error: %v", err)
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("expected unix socket cleanup, got %v", err)
	}
}

func TestServiceUnixSocketTransportRefusesExistingRegularFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets are not supported on windows")
	}
	socketPath := filepath.Join(t.TempDir(), "or3.sock")
	if err := os.WriteFile(socketPath, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write existing file: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}

	err := serveHTTPWithConfiguredTransport(context.Background(), server, config.Config{Service: config.ServiceConfig{UnixSocket: socketPath}})
	if err == nil {
		t.Fatal("expected existing regular file to be refused")
	}
	if _, statErr := os.Stat(socketPath); statErr != nil {
		t.Fatalf("expected existing regular file to remain, got %v", statErr)
	}
}
