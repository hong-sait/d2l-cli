package native

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)

type Request struct {
	ID        string          `json:"id"`
	Operation string          `json:"operation"`
	Params    json.RawMessage `json:"params"`
}

type Response struct {
	ID    string         `json:"id"`
	OK    bool           `json:"ok"`
	Data  *ResponseData  `json:"data,omitempty"`
	Error *ResponseError `json:"error,omitempty"`
}

type ResponseData struct {
	Status string `json:"status"`
}

type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Serve processes one framed request at a time until Chrome closes stdin.
// Stdout contains protocol frames only; callers send diagnostics elsewhere.
func Serve(in io.Reader, out io.Writer) error {
	for {
		body, err := ReadFrame(in, MaxApplicationMessage)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		response, err := Handle(body)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("encode native response: %w", err)
		}
		if err := WriteFrame(out, encoded, MaxApplicationResponse); err != nil {
			return err
		}
	}
}

// Handle validates one JSON request. Invalid JSON/schema is rejected without
// echoing untrusted request text. Recognized IDs are echoed for valid-schema
// operation errors so callers can correlate responses.
func Handle(body []byte) (Response, error) {
	if len(body) == 0 || len(body) > MaxApplicationMessage || !json.Valid(body) {
		return Response{}, ErrInvalidFrame
	}
	var request Request
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return Response{}, fmt.Errorf("%w: invalid request schema", ErrInvalidFrame)
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		return Response{}, err
	}
	if !requestIDPattern.MatchString(request.ID) {
		return Response{}, fmt.Errorf("%w: invalid request ID", ErrInvalidFrame)
	}
	if len(request.Params) == 0 {
		return Response{}, fmt.Errorf("%w: params are required", ErrInvalidFrame)
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(request.Params, &params); err != nil || params == nil {
		return Response{}, fmt.Errorf("%w: params must be an object", ErrInvalidFrame)
	}
	if request.Operation != "ping" {
		return Response{ID: request.ID, OK: false, Error: &ResponseError{
			Code: "unsupported_operation", Message: "Operation is not supported.",
		}}, nil
	}
	if len(params) != 0 {
		return Response{ID: request.ID, OK: false, Error: &ResponseError{
			Code: "invalid_params", Message: "Ping does not accept parameters.",
		}}, nil
	}
	return Response{ID: request.ID, OK: true, Data: &ResponseData{Status: "ready"}}, nil
}

func ensureSingleJSONValue(decoder *json.Decoder) error {
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("%w: multiple JSON values", ErrInvalidFrame)
	}
	return nil
}

func IsExtensionOrigin(origin string) bool {
	const prefix = "chrome-extension://"
	if len(origin) <= len(prefix)+32 || origin[:len(prefix)] != prefix || origin[len(origin)-1] != '/' {
		return false
	}
	id := origin[len(prefix) : len(origin)-1]
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if c < 'a' || c > 'p' {
			return false
		}
	}
	return true
}
