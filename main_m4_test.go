package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"d2l-cli/internal/native"
)

func TestParseM4CommandsAndRejectsArbitraryOptions(t *testing.T) {
	cmd, format, active, course, err := parseCLI([]string{"courses", "list", "--active", "--format", "json"})
	if err != nil || cmd != "courses list" || format != "json" || !active || course != "" {
		t.Fatalf("parsed values: %q %q %v %q %v", cmd, format, active, course, err)
	}
	cmd, format, _, course, err = parseCLI([]string{"content", "list", "--course", "6605", "--format", "csv"})
	if err != nil || cmd != "content list" || format != "csv" || course != "6605" {
		t.Fatalf("content parse = %q %q %q %v", cmd, format, course, err)
	}
	if _, _, _, _, err := parseCLI([]string{"courses", "list", "--url", "https://example.com"}); err == nil {
		t.Fatal("arbitrary CLI option was accepted")
	}
	if _, _, _, _, err := parseCLI([]string{"content", "list", "--course", "../1"}); err == nil {
		t.Fatal("unsafe course ID was accepted")
	}
	if command, _, active, _, err := parseCLI([]string{"courses", "list", "--all", "--active"}); err != nil || command != "courses list" || !active {
		t.Fatalf("--all with --active was rejected: %v", err)
	}
	if _, _, _, _, err := parseCLI([]string{"courses", "list", "--all", "--all"}); err == nil {
		t.Fatal("duplicate --all was accepted")
	}
	if _, _, _, _, err := parseCLI([]string{"students", "list", "--course", "302b", "--all"}); err == nil {
		t.Fatal("--all was accepted for student listing")
	}
}

func TestCurrentAndFutureCourses(t *testing.T) {
	courses := []native.Course{
		{ID: "1", Name: "Fall 2025 Old"},
		{ID: "2", Name: "Spring 2026 Old"},
		{ID: "3", Name: "Fall 2026 Current"},
		{ID: "4", Name: "Winter 2027 Future"},
		{ID: "5", Name: "Fall 2027 Future"},
		{ID: "6", Name: "Non-Credit 2019-2020"},
	}
	got := currentAndFutureCourses(courses, time.Date(2026, time.October, 11, 0, 0, 0, 0, time.UTC))
	if len(got) != 3 || got[0].ID != "3" || got[1].ID != "4" || got[2].ID != "5" {
		t.Fatalf("Fall 2026 selection = %#v", got)
	}
	got = currentAndFutureCourses(courses, time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC))
	if len(got) != 4 || got[0].ID != "2" {
		t.Fatalf("Winter 2026 selection = %#v", got)
	}
}

func TestCollectCoursesPaginatesFiltersAndRejectsRepeatedBookmark(t *testing.T) {
	active := true
	inactive := false
	pages := map[string]native.Response{
		"":   {OK: true, Data: &native.ResponseData{Courses: []native.Course{{ID: "10", Name: "Course A", TypeCode: "Course Offering", Active: &active}, {ID: "20", Name: "Semester", TypeCode: "Semester", Active: &active}}, Paging: &native.Paging{Bookmark: "20", HasMoreItems: true}}},
		"20": {OK: true, Data: &native.ResponseData{Courses: []native.Course{{ID: "30", Name: "Course B", TypeCode: "Course Offering", Active: &inactive}}, Paging: &native.Paging{Bookmark: "30", HasMoreItems: false}}},
	}
	var requested []string
	courses, err := collectCourses(true, func(bookmark string) (native.Response, error) {
		requested = append(requested, bookmark)
		return pages[bookmark], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(requested) != 2 || requested[0] != "" || requested[1] != "20" {
		t.Fatalf("requested pages %q", requested)
	}
	if len(courses) != 1 || courses[0].ID != "10" {
		t.Fatalf("filtered courses %#v", courses)
	}
	_, err = collectCourses(false, func(bookmark string) (native.Response, error) {
		return native.Response{OK: true, Data: &native.ResponseData{Paging: &native.Paging{Bookmark: "same", HasMoreItems: true}}}, nil
	})
	if err == nil {
		t.Fatal("repeated bookmark was accepted")
	}
}

func TestOutputFormatsUseJSONAndEscapedCSV(t *testing.T) {
	courses := []native.Course{{ID: "1", Name: "Course, \"One\"", Code: "ABC", Role: "Learner"}}
	var out bytes.Buffer
	if err := writeCourses(&out, courses, "json"); err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]string
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || len(decoded) != 1 || decoded[0]["name"] != courses[0].Name || len(decoded[0]) != 2 {
		t.Fatalf("invalid JSON %q: %v", out.String(), err)
	}
	out.Reset()
	if err := writeCourses(&out, courses, "csv"); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "id,name\n1,\"Course, \"\"One\"\"\"\n"; got != want {
		t.Fatalf("CSV=%q, want %q", got, want)
	}
	out.Reset()
	if err := writeCourses(&out, courses, "table"); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "id\tname\n1\tCourse, \"One\"\n"; got != want {
		t.Fatalf("table=%q, want %q", got, want)
	}
}

func TestNativeRequestAllowlistValidatesM4Parameters(t *testing.T) {
	for _, request := range []native.Request{
		{ID: "1", Operation: "api.versions", Params: json.RawMessage(`{}`)},
		{ID: "2", Operation: "courses.page", Params: json.RawMessage(`{"bookmark":"20"}`)},
		{ID: "3", Operation: "content.list", Params: json.RawMessage(`{"courseId":"6605"}`)},
		{ID: "7", Operation: "students.list", Params: json.RawMessage(`{"courseId":"886700"}`)},
	} {
		if err := native.ValidateRequest(request); err != nil {
			t.Fatalf("valid request rejected: %+v: %v", request, err)
		}
	}
	for _, request := range []native.Request{
		{ID: "4", Operation: "proxy", Params: json.RawMessage(`{"url":"https://example.com"}`)},
		{ID: "5", Operation: "content.list", Params: json.RawMessage(`{"courseId":"../1"}`)},
		{ID: "6", Operation: "courses.page", Params: json.RawMessage(`{"bookmark":"x","url":"https://example.com"}`)},
		{ID: "8", Operation: "students.list", Params: json.RawMessage(`{"courseId":"886700","next":"https://example.com"}`)},
	} {
		if err := native.ValidateRequest(request); !errors.Is(err, native.ErrInvalidFrame) {
			t.Fatalf("unsafe request accepted: %+v", request)
		}
	}
}
