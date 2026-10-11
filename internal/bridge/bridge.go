package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"

	"d2l-cli/internal/ipc"
	"d2l-cli/internal/native"
)

type frameEvent struct {
	body []byte
	err  error
}

type localCall struct {
	request native.Request
	reply   chan native.Response
}

type pendingCall struct {
	reply     chan native.Response
	operation string
}

// Serve multiplexes the browser's Native Messaging stream with the per-user
// Unix socket used by CLI processes.
func Serve(ctx context.Context, in io.Reader, out io.Writer, socketPath string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	incoming := make(chan frameEvent, 1)
	requests := make(chan localCall)
	cancelled := make(chan string, 32)
	go func() {
		for {
			body, err := native.ReadFrame(in, native.MaxApplicationMessage)
			incoming <- frameEvent{body: body, err: err}
			if err != nil {
				return
			}
		}
	}()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- ipc.Serve(ctx, socketPath, func(callCtx context.Context, request native.Request) (native.Response, error) {
			reply := make(chan native.Response, 1)
			select {
			case requests <- localCall{request: request, reply: reply}:
			case <-callCtx.Done():
				select {
				case cancelled <- request.ID:
				default:
				}
				return native.Response{}, callCtx.Err()
			case <-ctx.Done():
				return native.Response{}, io.EOF
			}
			select {
			case response := <-reply:
				return response, nil
			case <-callCtx.Done():
				select {
				case cancelled <- request.ID:
				default:
				}
				return native.Response{}, callCtx.Err()
			case <-ctx.Done():
				return native.Response{}, io.EOF
			}
		})
	}()
	pending := make(map[string]pendingCall)
	var writeMu sync.Mutex
	writeMessage := func(message any) error {
		body, err := json.Marshal(message)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		return native.WriteFrame(out, body, native.MaxApplicationResponse)
	}
	for {
		select {
		case request := <-requests:
			if _, exists := pending[request.request.ID]; exists {
				request.reply <- native.Response{ID: request.request.ID, OK: false, Error: &native.ResponseError{Code: "duplicate_id", Message: "Request ID is already active."}}
				continue
			}
			if len(pending) >= 32 {
				request.reply <- native.Response{ID: request.request.ID, OK: false, Error: &native.ResponseError{Code: "busy", Message: "Too many requests are active."}}
				continue
			}
			pending[request.request.ID] = pendingCall{reply: request.reply, operation: request.request.Operation}
			if err := writeMessage(request.request); err != nil {
				delete(pending, request.request.ID)
				request.reply <- native.Response{ID: request.request.ID, OK: false, Error: &native.ResponseError{Code: "disconnected", Message: "Browser extension is disconnected."}}
			}
		case event := <-incoming:
			if event.err != nil {
				cancel()
				<-serverDone
				if errors.Is(event.err, io.EOF) {
					return nil
				}
				return event.err
			}
			var kind struct {
				Operation string `json:"operation"`
				OK        *bool  `json:"ok"`
			}
			if err := json.Unmarshal(event.body, &kind); err != nil {
				return err
			}
			if kind.Operation != "" {
				request, err := decodeRequest(event.body)
				if err != nil {
					return err
				}
				response, err := native.Handle(event.body)
				if err != nil {
					return err
				}
				response.ID = request.ID
				if err := writeMessage(response); err != nil {
					return err
				}
				continue
			}
			if kind.OK == nil {
				return native.ErrInvalidFrame
			}
			response, err := native.DecodeExtensionResponse(event.body)
			if err != nil {
				return err
			}
			if call, exists := pending[response.ID]; exists {
				delete(pending, response.ID)
				if response.OK && ((call.operation == "ping" && (response.Data == nil || response.Data.Status != "ready")) ||
					(call.operation == "whoami" && (response.Data == nil || response.Data.Identifier == "" || response.Data.UniqueName == "")) ||
					(call.operation == "api.versions" && (response.Data == nil || len(response.Data.Versions) == 0)) ||
					(call.operation == "courses.page" && (response.Data == nil || response.Data.Courses == nil || response.Data.Paging == nil)) ||
					(call.operation == "content.list" && (response.Data == nil || response.Data.Content == nil))) {
					response = native.Response{ID: response.ID, OK: false, Error: &native.ResponseError{Code: "invalid_response", Message: "Browser returned an incomplete response."}}
				}
				call.reply <- response
			}
		case id := <-cancelled:
			delete(pending, id)
		case <-ctx.Done():
			cancel()
			<-serverDone
			return ctx.Err()
		case serverErr := <-serverDone:
			cancel()
			return serverErr
		}
	}
}

func decodeRequest(body []byte) (native.Request, error) {
	var request native.Request
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return native.Request{}, native.ErrInvalidFrame
	}
	if err := native.ValidateRequest(request); err != nil {
		return native.Request{}, err
	}
	return request, nil
}
