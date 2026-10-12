package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"text/tabwriter"
	"time"

	"d2l-cli/internal/bridge"
	"d2l-cli/internal/ipc"
	"d2l-cli/internal/native"
)

// expectedExtensionOrigin is set by the Chrome host installer at build time.
var expectedExtensionOrigin string

func main() {
	args := os.Args[1:]
	if len(args) > 0 && (args[0] == "native-host" || strings.HasPrefix(args[0], "chrome-extension://")) {
		if expectedExtensionOrigin == "" {
			fmt.Fprintln(os.Stderr, "native-host is not registered for a specific extension ID")
			os.Exit(2)
		}
		if err := runNativeBridgeHost(nativeHostArgs(args), os.Stdin, os.Stdout, os.Stderr, expectedExtensionOrigin, ""); err != nil {
			fmt.Fprintln(os.Stderr, "native-host: request rejected or browser disconnected")
			os.Exit(1)
		}
		return
	}
	if err := runCLI(args, os.Stdout, os.Stderr, ""); err != nil {
		os.Exit(1)
	}
}

func nativeHostArgs(args []string) []string {
	// Chrome launches the registered executable directly and supplies the
	// extension origin as argv[1]. The explicit subcommand is also accepted
	// for manual protocol testing.
	if len(args) > 0 && args[0] == "native-host" {
		return args[1:]
	}
	return args
}

func runNativeHost(args []string, in io.Reader, out, diagnostics io.Writer, expectedOrigin string) error {
	if err := validateNativeHostArgs(args, diagnostics, expectedOrigin); err != nil {
		return err
	}
	return native.Serve(in, out)
}

func runNativeBridgeHost(args []string, in io.Reader, out, diagnostics io.Writer, expectedOrigin, socketPath string) error {
	if err := validateNativeHostArgs(args, diagnostics, expectedOrigin); err != nil {
		return err
	}
	if socketPath == "" {
		var err error
		socketPath, err = ipc.SocketPath()
		if err != nil {
			return err
		}
	}
	return bridge.Serve(context.Background(), in, out, socketPath)
}

func validateNativeHostArgs(args []string, diagnostics io.Writer, expectedOrigin string) error {
	if len(args) == 0 || args[0] != expectedOrigin || !native.IsExtensionOrigin(expectedOrigin) {
		fmt.Fprintln(diagnostics, "native-host: caller origin is not allowlisted")
		return native.ErrUnauthorizedOrigin
	}
	if len(args) > 1 {
		// Chrome on Windows may append --parent-window. It is not used by this host.
		for _, arg := range args[1:] {
			if len(arg) < len("--parent-window=") || arg[:len("--parent-window=")] != "--parent-window=" {
				fmt.Fprintln(diagnostics, "native-host: unexpected launch argument")
				return native.ErrUnauthorizedOrigin
			}
		}
	}
	return nil
}

var requestCounter atomic.Uint64

func newRequestID() string {
	return fmt.Sprintf("cli-%d-%d", time.Now().UnixNano(), requestCounter.Add(1))
}

func runCLI(args []string, stdout, stderr io.Writer, socketPath string) error {
	if len(args) == 1 && args[0] == "--help" {
		fmt.Fprintln(stdout, "Usage: d2l <status|whoami|api versions|courses list|content list --course ID|students list --course CODE|students search NAME>")
		return nil
	}
	command, format, activeOnly, courseID, err := parseCLI(args)
	if err != nil {
		fmt.Fprintln(stderr, "Usage: d2l status | whoami | api versions | courses list [--all] [--active] [--format table|json|csv] | content list --course ID [--format table|json|csv] | students list --course CODE|ID [--format table|json|csv] | students search NAME [--format table|json|csv]")
		return err
	}
	if socketPath == "" {
		socketPath, err = ipc.SocketPath()
		if err != nil {
			fmt.Fprintln(stderr, "Browser bridge is disconnected. Open Brightspace in Chrome or Edge and try again.")
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if command == "status" || command == "whoami" {
		operation := "ping"
		if command == "whoami" {
			operation = "whoami"
		}
		response, err := callBridge(ctx, socketPath, operation, json.RawMessage(`{}`))
		return printSimpleResponse(command, response, err, stdout, stderr)
	}
	if command == "api versions" {
		response, err := callBridge(ctx, socketPath, "api.versions", json.RawMessage(`{}`))
		if err != nil || !response.OK {
			return reportBridgeError(response, err, stderr)
		}
		return writeVersions(stdout, response.Data.Versions, format)
	}
	if command == "content list" {
		params, _ := json.Marshal(map[string]string{"courseId": courseID})
		response, err := callBridge(ctx, socketPath, "content.list", params)
		if err != nil || !response.OK {
			return reportBridgeError(response, err, stderr)
		}
		return writeContent(stdout, response.Data.Content, format)
	}
	if command == "students list" {
		course, notice, err := resolveStudentCourse(ctx, socketPath, courseID, time.Now())
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return fmt.Errorf("course resolution failed")
		}
		if notice != "" {
			fmt.Fprintln(stderr, notice)
		}
		students, err := listStudents(ctx, socketPath, course.ID)
		if err != nil {
			fmt.Fprintln(stderr, "Could not list students: "+safeCLIErr(err))
			return err
		}
		return writeStudents(stdout, students, format)
	}
	if command == "students search" {
		result, err := searchCurrentTermStudents(ctx, socketPath, courseID, time.Now())
		if err != nil {
			fmt.Fprintln(stderr, "Could not search students: "+safeCLIErr(err))
			return err
		}
		if !result.ExactMatchFound {
			if len(result.Matches) == 0 {
				fmt.Fprintln(stderr, "No exact match or similar names found.")
			} else {
				plural := "s"
				if len(result.Matches) == 1 {
					plural = ""
				}
				fmt.Fprintf(stderr, "No exact match found. Showing %d similar name suggestion%s.\n", len(result.Matches), plural)
			}
		}
		return writeStudentMatches(stdout, result.Matches, format)
	}
	courses, err := listCourses(ctx, socketPath, activeOnly)
	if err != nil {
		fmt.Fprintln(stderr, "Could not list courses: "+safeCLIErr(err))
		return err
	}
	allCourses := false
	for _, arg := range args {
		if arg == "--all" {
			allCourses = true
			break
		}
	}
	if !allCourses {
		courses = currentAndFutureCourses(courses, time.Now())
	}
	return writeCourses(stdout, courses, format)
}

func printSimpleResponse(command string, response native.Response, err error, stdout, stderr io.Writer) error {
	if err != nil || !response.OK {
		message := "Browser bridge is disconnected. Open Brightspace in Chrome or Edge and try again."
		if response.Error != nil && response.Error.Code == "auth_required" {
			message = "Brightspace sign-in is required. Sign in in the browser and retry."
		}
		if command == "status" {
			fmt.Fprintln(stdout, "Browser bridge: disconnected")
			return nil
		}
		fmt.Fprintln(stderr, message)
		return fmt.Errorf("browser bridge request failed")
	}
	if command == "status" {
		if response.Data == nil || response.Data.Status != "ready" {
			fmt.Fprintln(stdout, "Browser bridge: disconnected")
			return nil
		}
		fmt.Fprintln(stdout, "Browser bridge: connected")
		return nil
	}
	if response.Data == nil || response.Data.Identifier == "" || response.Data.UniqueName == "" {
		fmt.Fprintln(stderr, "Browser returned an incomplete whoami response.")
		return fmt.Errorf("invalid whoami response")
	}
	fmt.Fprintf(stdout, "Identifier: %s\nUnique name: %s\n", response.Data.Identifier, response.Data.UniqueName)
	return nil
}

func callBridge(ctx context.Context, socket, operation string, params json.RawMessage) (native.Response, error) {
	return ipc.Call(ctx, socket, native.Request{ID: newRequestID(), Operation: operation, Params: params})
}

func reportBridgeError(response native.Response, err error, stderr io.Writer) error {
	message := "Browser bridge is disconnected. Open Brightspace in Chrome or Edge and try again."
	if err == nil && response.Error != nil {
		message = response.Error.Message
	}
	fmt.Fprintln(stderr, message)
	return fmt.Errorf("browser API request failed")
}

func safeCLIErr(err error) string {
	if err == nil {
		return "request failed"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "browser request timed out; reload the unpacked extension and restart Chrome, then retry"
	}
	switch err.Error() {
	case "Unsupported read-only operation.":
		return "the extension does not support this operation yet; reload the unpacked extension and restart Chrome"
	case "Sign in to Brightspace and retry.", "Sign in to Brightspace or check course access.",
		"Brightspace rate limit reached; retry later.", "Brightspace returned a non-JSON response.",
		"Brightspace returned invalid JSON.", "Brightspace returned an unexpected response structure.",
		"Brightspace request timed out.", "Brightspace request failed; check connectivity and sign-in.":
		return err.Error()
	}
	if statusErr := regexp.MustCompile(`^Brightspace returned HTTP (\d{3})\.$`); statusErr.MatchString(err.Error()) {
		return err.Error()
	}
	return "browser bridge request failed"
}

func parseCLI(args []string) (command, format string, active bool, course string, err error) {
	all := false
	if len(args) == 1 && args[0] == "status" {
		return "status", "table", false, "", nil
	}
	if len(args) == 1 && args[0] == "whoami" {
		return "whoami", "table", false, "", nil
	}
	if len(args) == 2 && args[0] == "api" && args[1] == "versions" {
		return "api versions", "table", false, "", nil
	}
	if len(args) < 2 {
		return "", "", false, "", fmt.Errorf("unsupported command")
	}
	command = args[0] + " " + args[1]
	if command != "courses list" && command != "content list" && command != "students list" && command != "students search" {
		return "", "", false, "", fmt.Errorf("unsupported command")
	}
	for i := 2; i < len(args); i++ {
		if command == "students search" && !strings.HasPrefix(args[i], "--") {
			if course != "" {
				course += " "
			}
			course += args[i]
			continue
		}
		switch args[i] {
		case "--all":
			if command != "courses list" || all {
				return "", "", false, "", fmt.Errorf("invalid option")
			}
			all = true
		case "--active":
			if command != "courses list" || active {
				return "", "", false, "", fmt.Errorf("invalid option")
			}
			active = true
		case "--course":
			if (command != "content list" && command != "students list") || course != "" || i+1 >= len(args) {
				return "", "", false, "", fmt.Errorf("invalid course")
			}
			i++
			course = args[i]
			if command == "content list" && !decimalID(course) || command == "students list" && !validCourseSelector(course) {
				return "", "", false, "", fmt.Errorf("invalid course")
			}
		case "--format":
			if i+1 >= len(args) {
				return "", "", false, "", fmt.Errorf("missing format")
			}
			i++
			format = args[i]
			if format != "table" && format != "json" && format != "csv" {
				return "", "", false, "", fmt.Errorf("unsupported format")
			}
		default:
			return "", "", false, "", fmt.Errorf("unsupported option")
		}
	}
	if format == "" {
		format = "table"
	}
	if (command == "content list" || command == "students list" || command == "students search") && course == "" {
		if command == "students search" {
			return "", "", false, "", fmt.Errorf("student name is required")
		}
		return "", "", false, "", fmt.Errorf("course is required")
	}
	if command == "students search" && !validStudentNameQuery(course) {
		return "", "", false, "", fmt.Errorf("invalid student name")
	}
	return command, format, active, course, nil
}

func decimalID(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}

var courseSelectorPattern = regexp.MustCompile(`(?i)^(?:[a-z]{2,8}-?)?\d{3}-?[a-z]$`)

func validCourseSelector(s string) bool { return decimalID(s) || courseSelectorPattern.MatchString(s) }

func listCourses(ctx context.Context, socket string, activeOnly bool) ([]native.Course, error) {
	return collectCourses(activeOnly, func(bookmark string) (native.Response, error) {
		params, _ := json.Marshal(map[string]string{"bookmark": bookmark})
		return callBridge(ctx, socket, "courses.page", params)
	})
}

var (
	courseCodePattern = regexp.MustCompile(`(?i)([a-z]{2,8})-?(\d{3})-?([a-z])`)
	selectorPattern   = regexp.MustCompile(`(?i)^(?:([a-z]{2,8})-?)?(\d{3})-?([a-z])$`)
)

type courseCode struct{ subject, number, section string }

func parseCourseCode(value string) (courseCode, bool) {
	m := selectorPattern.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return courseCode{}, false
	}
	return courseCode{strings.ToUpper(m[1]), m[2], strings.ToUpper(m[3])}, true
}

func codesForCourse(course native.Course) []courseCode {
	var found []courseCode
	for _, source := range []string{course.Code, course.Name} {
		for _, match := range courseCodePattern.FindAllStringSubmatch(source, -1) {
			found = append(found, courseCode{strings.ToUpper(match[1]), match[2], strings.ToUpper(match[3])})
		}
	}
	return found
}

func currentAcademicTerm(now time.Time) string {
	term := "Fall"
	switch now.Month() {
	case time.January, time.February, time.March, time.April:
		term = "Winter"
	case time.May, time.June, time.July, time.August:
		term = "Spring"
	}
	return fmt.Sprintf("%s %d", term, now.Year())
}

var academicTermNamePattern = regexp.MustCompile(`(?i)^\s*(Winter|Spring|Fall)\s+(\d{4})(?:\s|$)`)

func currentAndFutureCourses(courses []native.Course, now time.Time) []native.Course {
	currentSeason := 2
	if now.Month() <= time.April {
		currentSeason = 0
	} else if now.Month() <= time.August {
		currentSeason = 1
	}
	currentOrder := now.Year()*3 + currentSeason
	result := make([]native.Course, 0, len(courses))
	for _, course := range courses {
		match := academicTermNamePattern.FindStringSubmatch(course.Name)
		if match == nil {
			continue
		}
		year, err := strconv.Atoi(match[2])
		if err != nil {
			continue
		}
		season := 2
		switch strings.ToLower(match[1]) {
		case "winter":
			season = 0
		case "spring":
			season = 1
		}
		if year*3+season >= currentOrder {
			result = append(result, course)
		}
	}
	return result
}

func resolveStudentCourse(ctx context.Context, socket, selector string, now time.Time) (native.Course, string, error) {
	if decimalID(selector) {
		return native.Course{ID: selector}, "", nil
	}
	query, _ := parseCourseCode(selector)
	courses, err := listCourses(ctx, socket, false)
	if err != nil {
		return native.Course{}, "", fmt.Errorf("Could not list your courses. Sign in to Brightspace and retry.")
	}
	term := currentAcademicTerm(now)
	var inTerm, exact []native.Course
	for _, course := range courses {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(course.Name)), strings.ToLower(term)) {
			continue
		}
		inTerm = append(inTerm, course)
		for _, code := range codesForCourse(course) {
			if code.number == query.number && code.section == query.section && (query.subject == "" || code.subject == query.subject) {
				exact = append(exact, course)
				break
			}
		}
	}
	if len(exact) == 1 {
		return exact[0], "", nil
	}
	if len(exact) > 1 {
		return native.Course{}, "", fmt.Errorf("More than one %s course matches in %s. Use a numeric course ID from `d2l courses list`.", selector, term)
	}

	var alternatives []native.Course
	for _, course := range inTerm {
		for _, code := range codesForCourse(course) {
			if code.number == query.number && code.section != query.section && (query.subject == "" || code.subject == query.subject) {
				alternatives = append(alternatives, course)
				break
			}
		}
	}
	if len(alternatives) == 0 {
		return native.Course{}, "", fmt.Errorf("No exact course %s or alternate section was found for %s.", selector, term)
	}
	if !isInteractiveStdin() {
		return native.Course{}, "", fmt.Errorf("No exact course %s was found for %s. Other sections are available; rerun with a numeric course ID after choosing one from `d2l courses list`.", selector, term)
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprintf(os.Stderr, "No exact match for %s in %s. Check other sections? [y/N] ", selector, term)
	answer, _ := reader.ReadString('\n')
	if strings.ToLower(strings.TrimSpace(answer)) != "y" && strings.ToLower(strings.TrimSpace(answer)) != "yes" {
		return native.Course{}, "", fmt.Errorf("No course selected.")
	}
	fmt.Fprintln(os.Stderr, "Available sections:")
	for _, course := range alternatives {
		fmt.Fprintf(os.Stderr, "  %s\t%s\t%s\n", course.ID, course.Name, course.Code)
	}
	fmt.Fprint(os.Stderr, "Enter the exact course code or numeric course ID to use (or q to cancel): ")
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)
	if strings.EqualFold(choice, "q") || choice == "" {
		return native.Course{}, "", fmt.Errorf("No course selected.")
	}
	for _, course := range alternatives {
		if course.ID == choice || courseCodeMatchesSelection(course, choice) {
			return course, fmt.Sprintf("Using explicitly selected alternate course %s (%s).", course.Code, course.ID), nil
		}
	}
	return native.Course{}, "", fmt.Errorf("Selection did not match an available course. Use one of the exact codes or IDs shown.")
}

func courseCodeMatchesSelection(course native.Course, selection string) bool {
	if strings.EqualFold(course.Code, selection) {
		return true
	}
	selected, ok := parseCourseCode(selection)
	if !ok {
		return false
	}
	for _, code := range codesForCourse(course) {
		if code == selected {
			return true
		}
	}
	return false
}

func isInteractiveStdin() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func listStudents(ctx context.Context, socket, courseID string) ([]native.Student, error) {
	params, _ := json.Marshal(map[string]string{"courseId": courseID})
	response, err := callBridge(ctx, socket, "students.list", params)
	if err != nil {
		return nil, err
	}
	if !response.OK {
		if response.Error != nil {
			return nil, fmt.Errorf("%s", response.Error.Message)
		}
		return nil, fmt.Errorf("request failed")
	}
	if response.Data == nil || response.Data.Students == nil {
		return nil, fmt.Errorf("invalid classlist response")
	}
	return response.Data.Students, nil
}

type studentMatch struct {
	StudentID string `json:"student_id"`
	Name      string `json:"name"`
	CourseID  string `json:"course_id"`
	Course    string `json:"course"`
}

func validStudentNameQuery(query string) bool {
	query = strings.TrimSpace(query)
	if len(query) < 2 || len(query) > 128 || strings.ContainsAny(query, "\r\n\x00") {
		return false
	}
	return len(nameTokens(query)) > 0
}

var studentNameTokenPattern = regexp.MustCompile(`[\p{L}\p{N}]+`)

func nameTokens(value string) []string {
	value = strings.ToLower(value)
	return studentNameTokenPattern.FindAllString(value, -1)
}

func studentNameMatches(displayName, query string) bool {
	wanted := nameTokens(query)
	available := nameTokens(displayName)
	if len(wanted) == 0 || len(available) == 0 {
		return false
	}
	// A one-token query searches first or last name. Multi-token queries match
	// each supplied name component regardless of order, including comma form.
	for _, token := range wanted {
		found := false
		for _, candidate := range available {
			if candidate == token {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

type studentSearchResult struct {
	Matches         []studentMatch
	ExactMatchFound bool
}

func nameEditDistance(left, right string) int {
	a, b := []rune(left), []rune(right)
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i, leftRune := range a {
		diagonal := previous[0]
		previous[0] = i + 1
		for j, rightRune := range b {
			above := previous[j+1]
			cost := 1
			if leftRune == rightRune {
				cost = 0
			}
			previous[j+1] = min(previous[j+1]+1, previous[j]+1, diagonal+cost)
			diagonal = above
		}
	}
	return previous[len(b)]
}

func similarNameScore(query, name string) (float64, bool) {
	wanted, available := nameTokens(query), nameTokens(name)
	if len(wanted) == 0 || len(available) == 0 {
		return 0, false
	}
	used := make([]bool, len(available))
	total := 0.0
	for _, token := range wanted {
		bestIndex, bestScore := -1, 0.0
		for index, candidate := range available {
			if used[index] {
				continue
			}
			length := max(len([]rune(token)), len([]rune(candidate)))
			distance := nameEditDistance(token, candidate)
			threshold := 0
			if len([]rune(token)) >= 3 {
				threshold = min(2, max(1, len([]rune(token))/4))
			}
			var score float64
			if len([]rune(token)) >= 2 && strings.HasPrefix(candidate, token) {
				score = 0.1 + 0.1*float64(len([]rune(candidate))-len([]rune(token)))/float64(length)
			} else if distance <= threshold {
				score = float64(distance) / float64(length)
			} else {
				continue
			}
			if bestIndex < 0 || score < bestScore {
				bestIndex, bestScore = index, score
			}
		}
		if bestIndex < 0 {
			return 0, false
		}
		used[bestIndex] = true
		total += bestScore
	}
	return total, true
}

func uniqueStudentMatches(matches []studentMatch) []studentMatch {
	unique := make([]studentMatch, 0, len(matches))
	seen := make(map[string]int, len(matches))
	for _, match := range matches {
		key := strings.ToLower(strings.Join(strings.Fields(match.Name), " ")) + "\x00" + match.CourseID
		if index, exists := seen[key]; exists {
			if unique[index].StudentID == "" && match.StudentID != "" {
				unique[index] = match
			}
			continue
		}
		seen[key] = len(unique)
		unique = append(unique, match)
	}
	return unique
}

func selectStudentMatches(candidates []studentMatch, query string) studentSearchResult {
	exact := make([]studentMatch, 0)
	for _, match := range candidates {
		if studentNameMatches(match.Name, query) {
			exact = append(exact, match)
		}
	}
	if len(exact) > 0 {
		return studentSearchResult{Matches: uniqueStudentMatches(exact), ExactMatchFound: true}
	}
	type scoredMatch struct {
		match studentMatch
		score float64
	}
	similar := make([]scoredMatch, 0)
	for _, match := range candidates {
		if score, ok := similarNameScore(query, match.Name); ok {
			similar = append(similar, scoredMatch{match: match, score: score})
		}
	}
	sort.SliceStable(similar, func(i, j int) bool {
		if similar[i].score != similar[j].score {
			return similar[i].score < similar[j].score
		}
		return similar[i].match.Name < similar[j].match.Name
	})
	ordered := make([]studentMatch, 0, len(similar))
	for _, item := range similar {
		ordered = append(ordered, item.match)
	}
	unique := uniqueStudentMatches(ordered)
	if len(unique) > 25 {
		unique = unique[:25]
	}
	return studentSearchResult{Matches: unique}
}

func searchCurrentTermStudents(ctx context.Context, socket, query string, now time.Time) (studentSearchResult, error) {
	courses, err := listCourses(ctx, socket, false)
	if err != nil {
		return studentSearchResult{}, fmt.Errorf("could not list current courses")
	}
	term := strings.ToLower(currentAcademicTerm(now))
	candidates := []studentMatch{}
	for _, course := range courses {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(course.Name)), term) {
			continue
		}
		students, err := listStudents(ctx, socket, course.ID)
		if err != nil {
			return studentSearchResult{}, fmt.Errorf("could not inspect a current-term course classlist")
		}
		for _, student := range students {
			candidates = append(candidates, studentMatch{StudentID: student.StudentID, Name: student.DisplayName, CourseID: course.ID, Course: course.Name})
		}
	}
	return selectStudentMatches(candidates, query), nil
}

func writeStudentMatches(w io.Writer, matches []studentMatch, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(w)
		encoder.SetEscapeHTML(true)
		return encoder.Encode(matches)
	}
	if format == "csv" {
		writer := csv.NewWriter(w)
		if err := writer.Write([]string{"student_id", "name", "course_id", "course"}); err != nil {
			return err
		}
		for _, match := range matches {
			if err := writer.Write([]string{match.StudentID, match.Name, match.CourseID, match.Course}); err != nil {
				return err
			}
		}
		writer.Flush()
		return writer.Error()
	}
	writer := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "student_id\tname\tcourse_id\tcourse")
	for _, match := range matches {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", match.StudentID, match.Name, match.CourseID, match.Course)
	}
	return writer.Flush()
}

func collectCourses(activeOnly bool, fetchPage func(string) (native.Response, error)) ([]native.Course, error) {
	bookmark := ""
	seenBookmarks := map[string]bool{}
	seenIDs := map[string]bool{}
	all := []native.Course{}
	for page := 0; page < 100; page++ {
		response, err := fetchPage(bookmark)
		if err != nil {
			return nil, err
		}
		if !response.OK {
			if response.Error != nil {
				return nil, fmt.Errorf("%s", response.Error.Message)
			}
			return nil, fmt.Errorf("request failed")
		}
		if response.Data == nil || response.Data.Paging == nil {
			return nil, fmt.Errorf("invalid page")
		}
		for _, course := range response.Data.Courses {
			if course.ID == "" || seenIDs[course.ID] {
				return nil, fmt.Errorf("duplicate or missing course ID")
			}
			seenIDs[course.ID] = true
			if !isCourseOffering(course.TypeCode) {
				continue
			}
			if activeOnly && (course.Active == nil || !*course.Active) {
				continue
			}
			all = append(all, course)
		}
		paging := response.Data.Paging
		if !paging.HasMoreItems {
			return all, nil
		}
		if paging.Bookmark == "" || seenBookmarks[paging.Bookmark] {
			return nil, fmt.Errorf("invalid or repeated paging bookmark")
		}
		seenBookmarks[paging.Bookmark] = true
		bookmark = paging.Bookmark
	}
	return nil, fmt.Errorf("course listing exceeded the 100-page safety limit")
}

func isCourseOffering(code string) bool {
	normalized := strings.ToLower(strings.NewReplacer(" ", "", "-", "", "_", "").Replace(code))
	return normalized == "courseoffering"
}

func writeVersions(out io.Writer, rows []native.APIVersion, format string) error {
	return writeRecords(out, format, []string{"productCode", "latestVersion"}, rows, func() [][]string {
		r := [][]string{}
		for _, v := range rows {
			r = append(r, []string{v.ProductCode, v.LatestVersion})
		}
		return r
	}())
}
func writeCourses(out io.Writer, rows []native.Course, format string) error {
	type courseOutput struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	data := make([]courseOutput, 0, len(rows))
	records := make([][]string, 0, len(rows))
	for _, course := range rows {
		data = append(data, courseOutput{ID: course.ID, Name: course.Name})
		records = append(records, []string{course.ID, course.Name})
	}
	return writeRecords(out, format, []string{"id", "name"}, data, records)
}
func writeContent(out io.Writer, rows []native.ContentItem, format string) error {
	return writeRecords(out, format, []string{"id", "title", "type"}, rows, func() [][]string {
		r := [][]string{}
		for _, v := range rows {
			r = append(r, []string{v.ID, v.Title, v.Type})
		}
		return r
	}())
}
func writeStudents(out io.Writer, rows []native.Student, format string) error {
	return writeRecords(out, format, []string{"student_id", "name"}, rows, func() [][]string {
		r := make([][]string, 0, len(rows))
		for _, student := range rows {
			r = append(r, []string{student.StudentID, student.DisplayName})
		}
		return r
	}())
}
func writeRecords(out io.Writer, format string, header []string, data any, records [][]string) error {
	if format == "json" {
		enc := json.NewEncoder(out)
		enc.SetEscapeHTML(false)
		return enc.Encode(data)
	}
	if format == "csv" {
		for _, row := range append([][]string{header}, records...) {
			for i, field := range row {
				if i > 0 {
					if _, err := io.WriteString(out, ","); err != nil {
						return err
					}
				}
				if _, err := io.WriteString(out, csvField(field)); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(out, "\n"); err != nil {
				return err
			}
		}
		return nil
	}
	if _, err := fmt.Fprintln(out, strings.Join(header, "\t")); err != nil {
		return err
	}
	for _, row := range records {
		if _, err := fmt.Fprintln(out, strings.Join(row, "\t")); err != nil {
			return err
		}
	}
	return nil
}

func csvField(value string) string {
	if !strings.ContainsAny(value, ",\"\r\n") {
		return value
	}
	return "\"" + strings.ReplaceAll(value, "\"", "\"\"") + "\""
}
