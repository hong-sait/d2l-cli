package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"d2l-cli/internal/native"
)

func TestNativeHostArgsAcceptChromeInvocationAndManualSubcommand(t *testing.T) {
	origin := "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/"
	for _, args := range [][]string{{origin}, {"native-host", origin}} {
		got := nativeHostArgs(args)
		if len(got) != 1 || got[0] != origin {
			t.Fatalf("nativeHostArgs(%q) = %q", args, got)
		}
	}
}

func TestRunNativeHostChecksOriginBeforeReadingFrames(t *testing.T) {
	var output, diagnostics bytes.Buffer
	err := runNativeHost([]string{"chrome-extension://bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/"},
		bytes.NewReader([]byte("not a frame")), &output, &diagnostics,
		"chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/")
	if !errors.Is(err, native.ErrUnauthorizedOrigin) {
		t.Fatalf("runNativeHost error = %v, want unauthorized origin", err)
	}
	if output.Len() != 0 {
		t.Fatalf("unauthorized origin wrote %d bytes to stdout", output.Len())
	}
	if diagnostics.Len() == 0 {
		t.Fatal("expected sanitized diagnostic on stderr")
	}
}

func TestRunNativeHostRespondsWithFramedPing(t *testing.T) {
	origin := "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/"
	request, err := json.Marshal(native.Request{ID: "test-1", Operation: "ping", Params: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	var input, output, diagnostics bytes.Buffer
	if err := native.WriteFrame(&input, request, native.MaxApplicationMessage); err != nil {
		t.Fatal(err)
	}
	if err := runNativeHost([]string{origin}, &input, &output, &diagnostics, origin); err != nil {
		t.Fatal(err)
	}
	body, err := native.ReadFrame(&output, native.MaxApplicationResponse)
	if err != nil {
		t.Fatal(err)
	}
	var response native.Response
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != "test-1" || !response.OK || response.Data == nil || response.Data.Status != "ready" {
		t.Fatalf("unexpected native response: %#v", response)
	}
	if diagnostics.Len() != 0 {
		t.Fatalf("successful ping wrote diagnostics: %q", diagnostics.String())
	}
	if _, err := native.ReadFrame(&output, native.MaxApplicationResponse); !errors.Is(err, io.EOF) {
		t.Fatalf("unexpected extra stdout data: %v", err)
	}
}
