//go:build darwin || linux

package ipc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"d2l-cli/internal/native"
)

func TestPrivateUnixSocketCallAndPermissions(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "d2l-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "bridge.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, path, func(_ context.Context, request native.Request) (native.Response, error) {
			return native.Response{ID: request.ID, OK: true, Data: &native.ResponseData{Status: "ready"}}, nil
		})
	}()
	waitForSocket(t, path, done)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("socket permissions = %o, want 600", info.Mode().Perm())
	}
	request := native.Request{ID: "ipc-test-1", Operation: "ping", Params: []byte(`{}`)}
	response, err := Call(context.Background(), path, request)
	if err != nil || !response.OK || response.Data == nil || response.Data.Status != "ready" {
		t.Fatalf("Call() response=%#v err=%v", response, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("IPC server did not stop after cancellation")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("socket path still exists after shutdown: %v", err)
	}
}

func TestCallRejectsUnsupportedRequestBeforeDial(t *testing.T) {
	request := native.Request{ID: "ipc-test-2", Operation: "fetch", Params: []byte(`{}`)}
	if _, err := Call(context.Background(), filepath.Join(t.TempDir(), "absent.sock"), request); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("Call() error = %v, want invalid request", err)
	}
}

func TestListenSecureRejectsBroadDirectoryPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "insecure")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := listenSecure(filepath.Join(dir, "bridge.sock")); err == nil {
		t.Fatal("listenSecure accepted a directory accessible to other users")
	}
}

func waitForSocket(t *testing.T, path string, done <-chan error) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("IPC server exited before listening: %v", err)
		default:
		}
		if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("socket %s was not created", path)
}
