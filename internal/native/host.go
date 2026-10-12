package native

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)

type Request struct {
	ID        string          `json:"id"`
	Operation string          `json:"operation"`
	Params    json.RawMessage `json:"params"`
}

// DecodeExtensionResponse accepts only the bounded response shape used by the
// extension bridge. It never returns unrecognized response fields.
func DecodeExtensionResponse(body []byte) (Response, error) {
	if len(body) == 0 || len(body) > MaxApplicationResponse || !json.Valid(body) {
		return Response{}, ErrInvalidFrame
	}
	var response Response
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return Response{}, fmt.Errorf("%w: invalid extension response", ErrInvalidFrame)
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		return Response{}, err
	}
	if !requestIDPattern.MatchString(response.ID) {
		return Response{}, fmt.Errorf("%w: invalid response ID", ErrInvalidFrame)
	}
	if response.OK {
		if response.Error != nil || response.Data == nil {
			return Response{}, fmt.Errorf("%w: invalid success response", ErrInvalidFrame)
		}
		for _, value := range []string{response.Data.Status, response.Data.Identifier, response.Data.UniqueName} {
			if len(value) > 256 || strings.ContainsAny(value, "\r\n\x00") {
				return Response{}, fmt.Errorf("%w: unsafe response value", ErrInvalidFrame)
			}
		}
		if len(response.Data.Versions) > 16 || len(response.Data.Courses) > 2000 || len(response.Data.Content) > 2000 || len(response.Data.Students) > 2000 {
			return Response{}, fmt.Errorf("%w: response collection exceeds limit", ErrInvalidFrame)
		}
		for _, item := range response.Data.Versions {
			if !safeResponseText(item.ProductCode, 16) || !safeResponseText(item.LatestVersion, 32) {
				return Response{}, ErrInvalidFrame
			}
		}
		for _, item := range response.Data.Courses {
			if !safeResponseText(item.ID, 32) || !safeResponseText(item.Name, 512) || !safeResponseText(item.Code, 128) || !safeResponseText(item.TypeCode, 128) || !safeResponseText(item.Role, 128) {
				return Response{}, ErrInvalidFrame
			}
		}
		for _, item := range response.Data.Content {
			if !safeResponseText(item.ID, 32) || !safeResponseText(item.Title, 512) || !safeResponseText(item.Type, 32) {
				return Response{}, ErrInvalidFrame
			}
		}
		if !safeResponseText(response.Data.Next, 4096) {
			return Response{}, ErrInvalidFrame
		}
		for _, item := range response.Data.Students {
			if !safeResponseText(item.StudentID, 128) || !safeResponseText(item.DisplayName, 256) {
				return Response{}, ErrInvalidFrame
			}
		}
		if response.Data.Paging != nil && (!safeResponseText(response.Data.Paging.Bookmark, 128)) {
			return Response{}, ErrInvalidFrame
		}
	} else if response.Data != nil || response.Error == nil || len(response.Error.Code) > 64 ||
		len(response.Error.Message) > 256 || strings.ContainsAny(response.Error.Code+response.Error.Message, "\r\n\x00") {
		return Response{}, fmt.Errorf("%w: invalid error response", ErrInvalidFrame)
	}
	return response, nil
}

func safeResponseText(value string, max int) bool {
	return len(value) <= max && !strings.ContainsAny(value, "\r\n\x00") && !strings.ContainsFunc(value, unicode.IsControl)
}

type Response struct {
	ID    string         `json:"id"`
	OK    bool           `json:"ok"`
	Data  *ResponseData  `json:"data,omitempty"`
	Error *ResponseError `json:"error,omitempty"`
}

type ResponseData struct {
	Status     string        `json:"status,omitempty"`
	Identifier string        `json:"Identifier,omitempty"`
	UniqueName string        `json:"UniqueName,omitempty"`
	Versions   []APIVersion  `json:"versions,omitempty"`
	Courses    []Course      `json:"courses,omitempty"`
	Content    []ContentItem `json:"content,omitempty"`
	Students   []Student     `json:"students,omitempty"`
	Next       string        `json:"next,omitempty"`
	Paging     *Paging       `json:"paging,omitempty"`
}

type APIVersion struct {
	ProductCode   string `json:"productCode"`
	LatestVersion string `json:"latestVersion"`
}
type Course struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Code     string `json:"code,omitempty"`
	TypeCode string `json:"typeCode,omitempty"`
	Role     string `json:"role,omitempty"`
	Active   *bool  `json:"active,omitempty"`
}
type ContentItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type,omitempty"`
}
type Student struct {
	StudentID   string `json:"studentId"`
	DisplayName string `json:"displayName"`
}
type Paging struct {
	Bookmark     string `json:"bookmark,omitempty"`
	HasMoreItems bool   `json:"hasMoreItems"`
}

type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func ValidateRequest(request Request) error {
	if !requestIDPattern.MatchString(request.ID) || len(request.Params) == 0 {
		return ErrInvalidFrame
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(request.Params, &params); err != nil || params == nil || len(params) != 0 {
		if err != nil || params == nil {
			return ErrInvalidFrame
		}
		var allowed []string
		switch request.Operation {
		case "courses.page":
			allowed = []string{"bookmark"}
		case "content.list":
			allowed = []string{"courseId"}
		case "students.page":
			allowed = []string{"courseId", "next"}
		default:
			return ErrInvalidFrame
		}
		if len(params) != len(allowed) {
			return ErrInvalidFrame
		}
		for _, key := range allowed {
			if params[key] == nil {
				return ErrInvalidFrame
			}
			var value string
			maxLength := 128
			if request.Operation == "students.page" && key == "next" {
				maxLength = 4096
			}
			if json.Unmarshal(params[key], &value) != nil || len(value) > maxLength {
				return ErrInvalidFrame
			}
			if (request.Operation == "content.list" || (request.Operation == "students.page" && key == "courseId")) && (value == "" || !isDecimalID(value)) {
				return ErrInvalidFrame
			}
			if (request.Operation == "courses.page" || (request.Operation == "students.page" && key == "next")) && strings.ContainsAny(value, "\r\n\x00") {
				return ErrInvalidFrame
			}
		}
		if request.Operation == "students.page" {
			var courseID, next string
			if json.Unmarshal(params["courseId"], &courseID) != nil || json.Unmarshal(params["next"], &next) != nil ||
				(next != "" && !validClasslistCursor(next, courseID)) {
				return ErrInvalidFrame
			}
		}
		return nil
	}
	if request.Operation != "ping" && request.Operation != "whoami" && request.Operation != "api.versions" {
		return ErrInvalidFrame
	}
	return nil
}

func validClasslistCursor(value, courseID string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host != "learn.sait.ca" || u.User != nil || u.Fragment != "" || u.RawQuery == "" {
		return false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 7 || parts[0] != "d2l" || parts[1] != "api" || parts[2] != "le" || parts[4] != courseID || parts[5] != "classlist" || parts[6] != "paged" {
		return false
	}
	if !regexp.MustCompile(`^\d{1,3}\.\d{1,3}$`).MatchString(parts[3]) {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query) != 1 || len(query["bookmark"]) != 1 || query.Get("bookmark") == "" || len(query.Get("bookmark")) > 1024 || strings.ContainsAny(query.Get("bookmark"), "\r\n\x00") {
		return false
	}
	return true
}

func isDecimalID(s string) bool {
	if len(s) > 20 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
	if request.Operation != "ping" && request.Operation != "whoami" && request.Operation != "api.versions" && request.Operation != "courses.page" && request.Operation != "content.list" {
		return Response{ID: request.ID, OK: false, Error: &ResponseError{
			Code: "unsupported_operation", Message: "Operation is not supported.",
		}}, nil
	}
	if len(params) != 0 {
		return Response{ID: request.ID, OK: false, Error: &ResponseError{
			Code: "invalid_params", Message: "Operation does not accept parameters.",
		}}, nil
	}
	if request.Operation == "ping" {
		return Response{ID: request.ID, OK: true, Data: &ResponseData{Status: "ready"}}, nil
	}
	return Response{ID: request.ID, OK: false, Error: &ResponseError{
		Code: "not_connected", Message: "Browser API requests require the connected extension.",
	}}, nil
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
