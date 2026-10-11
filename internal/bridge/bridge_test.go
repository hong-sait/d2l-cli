//go:build darwin || linux

package bridge

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"d2l-cli/internal/ipc"
	"d2l-cli/internal/native"
)

func TestWhoamiRequestIsCorrelatedAcrossNativeAndLocalFrames(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "d2l-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "bridge.sock")
	extensionToHost, extensionWriter := io.Pipe()
	hostToExtension, hostWriter := io.Pipe()
	defer extensionToHost.Close()
	defer hostToExtension.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, extensionToHost, hostWriter, socket) }()
	waitForSocket(t, socket, done)

	request := native.Request{ID: "cli-test-1", Operation: "whoami", Params: json.RawMessage(`{}`)}
	callDone := make(chan struct {
		response native.Response
		err      error
	}, 1)
	go func() {
		response, err := ipc.Call(context.Background(), socket, request)
		callDone <- struct {
			response native.Response
			err      error
		}{response, err}
	}()

	body, err := native.ReadFrame(hostToExtension, native.MaxApplicationResponse)
	if err != nil {
		t.Fatal(err)
	}
	var forwarded native.Request
	if err := json.Unmarshal(body, &forwarded); err != nil {
		t.Fatal(err)
	}
	if forwarded.ID != request.ID || forwarded.Operation != "whoami" {
		t.Fatalf("forwarded request = %#v", forwarded)
	}
	response := native.Response{ID: forwarded.ID, OK: true, Data: &native.ResponseData{Identifier: "123", UniqueName: "test.user"}}
	responseBody, _ := json.Marshal(response)
	if err := native.WriteFrame(extensionWriter, responseBody, native.MaxApplicationMessage); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-callDone:
		if result.err != nil || !result.response.OK || result.response.Data.UniqueName != "test.user" {
			t.Fatalf("local call result=%#v err=%v", result.response, result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("local call did not receive the correlated extension response")
	}

	_ = extensionWriter.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("bridge did not stop after extension disconnect")
	}
	cancel()
}

func waitForSocket(t *testing.T, path string, done <-chan error) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("bridge exited before listening: %v", err)
		default:
		}
		if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("socket %s was not created", path)
}
