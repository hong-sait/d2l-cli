//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"d2l-cli/internal/ipc"
	"d2l-cli/internal/native"
)

func TestCLIStatusReportsDisconnectedWithoutFailing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"status"}, &stdout, &stderr, filepath.Join(t.TempDir(), "missing.sock"))
	if err != nil || stdout.String() != "Browser bridge: disconnected\n" || stderr.Len() != 0 {
		t.Fatalf("status stdout=%q stderr=%q err=%v", stdout.String(), stderr.String(), err)
	}
}

func TestCLIWhoamiReturnsOnlyAllowlistedFields(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "d2l-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "bridge.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- ipc.Serve(ctx, socket, func(_ context.Context, request native.Request) (native.Response, error) {
			return native.Response{ID: request.ID, OK: true, Data: &native.ResponseData{Identifier: "123", UniqueName: "test.user"}}, nil
		})
	}()
	waitForM3Socket(t, socket, done)
	var stdout, stderr bytes.Buffer
	if err := runCLI([]string{"whoami"}, &stdout, &stderr, socket); err != nil {
		t.Fatalf("runCLI error: %v (%s)", err, stderr.String())
	}
	if stdout.String() != "Identifier: 123\nUnique name: test.user\n" || stderr.Len() != 0 {
		t.Fatalf("unexpected CLI output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("IPC test server did not stop")
	}
}

func TestCLIWhoamiReportsDisconnectedOnMissingBridge(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"whoami"}, &stdout, &stderr, filepath.Join(t.TempDir(), "missing.sock"))
	if err == nil || !strings.Contains(stderr.String(), "Browser bridge is disconnected") {
		t.Fatalf("whoami stdout=%q stderr=%q err=%v", stdout.String(), stderr.String(), err)
	}
}

func waitForM3Socket(t *testing.T, path string, done <-chan error) {
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
