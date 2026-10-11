package native

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestFrameRoundTripUsesNativeEndianAndUTF8ByteLength(t *testing.T) {
	body := []byte(`{"id":"café","operation":"ping","params":{}}`)
	var framed bytes.Buffer
	if err := WriteFrame(&framed, body, MaxApplicationResponse); err != nil {
		t.Fatal(err)
	}
	gotLength := binary.NativeEndian.Uint32(framed.Bytes()[:4])
	if int(gotLength) != len(body) {
		t.Fatalf("frame length = %d, want UTF-8 byte length %d", gotLength, len(body))
	}
	got, err := ReadFrame(&framed, MaxApplicationMessage)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("frame body = %q, want %q", got, body)
	}
}

func TestReadFrameRejectsZeroOversizedTruncatedAndInvalidUTF8(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		max  uint32
		want error
	}{
		{"zero", lengthFrame(0, nil), 64, ErrInvalidFrame},
		{"oversized", lengthFrame(65, make([]byte, 65)), 64, ErrFrameTooLarge},
		{"truncated header", []byte{1, 2}, 64, io.ErrUnexpectedEOF},
		{"truncated body", lengthFrame(4, []byte("{}")), 64, ErrInvalidFrame},
		{"invalid utf8", lengthFrame(1, []byte{0xff}), 64, ErrInvalidFrame},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadFrame(bytes.NewReader(tc.data), tc.max)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ReadFrame error = %v, want errors.Is(_, %v)", err, tc.want)
			}
		})
	}
}

func TestHandleOnlyAllowsPingWithEmptyParams(t *testing.T) {
	got, err := Handle([]byte(`{"id":"req-1","operation":"ping","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.ID != "req-1" || got.Data == nil || got.Data.Status != "ready" {
		t.Fatalf("unexpected ping response: %#v", got)
	}

	unsupported, err := Handle([]byte(`{"id":"req-2","operation":"fetch","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if unsupported.OK || unsupported.Error == nil || unsupported.Error.Code != "unsupported_operation" {
		t.Fatalf("unexpected unsupported response: %#v", unsupported)
	}

	badParams, err := Handle([]byte(`{"id":"req-3","operation":"ping","params":{"url":"https://example.invalid"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if badParams.OK || badParams.Error == nil || badParams.Error.Code != "invalid_params" {
		t.Fatalf("unexpected params response: %#v", badParams)
	}
}

func TestHandleRejectsMalformedAndUnexpectedFields(t *testing.T) {
	for _, raw := range []string{
		`{`,
		`{"id":"req-1","operation":"ping","params":{},"url":"https://example.invalid"}`,
		`{"id":"req-1","operation":"ping","params":{}} {}`,
		`{"id":"bad id","operation":"ping","params":{}}`,
		`{"id":"req-1","operation":"ping","params":null}`,
	} {
		if _, err := Handle([]byte(raw)); err == nil {
			t.Errorf("Handle(%s) succeeded; want rejection", raw)
		}
	}
}

func TestServeHandlesMultipleFramesAndKeepsStdoutFramedJSON(t *testing.T) {
	var input bytes.Buffer
	for _, id := range []string{"one", "two"} {
		frame, _ := json.Marshal(Request{ID: id, Operation: "ping", Params: json.RawMessage(`{}`)})
		if err := WriteFrame(&input, frame, MaxApplicationMessage); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	if err := Serve(&input, &output); err != nil {
		t.Fatal(err)
	}
	for _, wantID := range []string{"one", "two"} {
		frame, err := ReadFrame(&output, MaxApplicationResponse)
		if err != nil {
			t.Fatal(err)
		}
		var response Response
		if err := json.Unmarshal(frame, &response); err != nil {
			t.Fatal(err)
		}
		if response.ID != wantID || !response.OK {
			t.Fatalf("response = %#v; want successful ID %q", response, wantID)
		}
	}
	if _, err := ReadFrame(&output, MaxApplicationResponse); !errors.Is(err, io.EOF) {
		t.Fatalf("unexpected trailing output: %v", err)
	}
}

func TestServeRejectsApplicationOversizeBeforeBodyAllocation(t *testing.T) {
	var header [4]byte
	binary.NativeEndian.PutUint32(header[:], MaxApplicationMessage+1)
	err := Serve(bytes.NewReader(header[:]), io.Discard)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("Serve error = %v, want oversized frame rejection", err)
	}
}

func TestWriteFrameEnforcesResponseCap(t *testing.T) {
	var output bytes.Buffer
	err := WriteFrame(&output, []byte(strings.Repeat("x", 5)), 4)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("WriteFrame error = %v, want oversized response rejection", err)
	}
	if output.Len() != 0 {
		t.Fatalf("wrote %d bytes before rejecting oversized response", output.Len())
	}
}

func TestExtensionOriginValidation(t *testing.T) {
	valid := "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/"
	if !IsExtensionOrigin(valid) {
		t.Fatalf("IsExtensionOrigin(%q) = false", valid)
	}
	for _, bad := range []string{"https://example.invalid/", "chrome-extension://abc/", "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "chrome-extension://zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz/"} {
		if IsExtensionOrigin(bad) {
			t.Errorf("IsExtensionOrigin(%q) = true", bad)
		}
	}
}

func lengthFrame(n uint32, body []byte) []byte {
	frame := make([]byte, 4+len(body))
	binary.NativeEndian.PutUint32(frame[:4], n)
	copy(frame[4:], body)
	return frame
}
