# d2l-cli Development Plan

**Status:** Implementation specification for Codex  
**Initial platform:** Bash terminal + Go executable + Microsoft Edge / Google Chrome (Manifest V3)  
**Target instance:** `https://learn.sait.ca`  
**Initial release:** Read-only MVP

## 1. Goal

Build `d2l`, a distributable, single-binary Go CLI for repetitive Brightspace D2L administration. The initial integration uses a Chrome/Edge extension to execute allowlisted requests within an authenticated Brightspace browser context. Bash is the initial shell interface; no Python or Node.js runtime should be required for end users.

Use documented Brightspace APIs when available. Do not bypass institutional controls, extract cookies/tokens, or rely on undocumented write endpoints. Confirm institutional permission for automation and extension installation.

## 2. Evidence from completed tests

| Request | Context | Observed result |
|---|---|---|
| `GET /d2l/api/versions/` | Browser and unauthenticated Windows PowerShell | Successful JSON; latest `lp=1.63`, `le=1.99` |
| `GET /d2l/api/lp/1.63/users/whoami` | Authenticated browser navigation | User JSON returned |
| Same `whoami` request | DevTools `fetch(..., {credentials: 'same-origin'})` on Brightspace page | HTTP 200, user JSON |
| Same `whoami` request | PowerShell without browser credentials | HTTP 403 |
| `GET /d2l/api/lp/1.63/enrollments/myenrollments/` | Authenticated browser | Enrolment JSON; `PagingInfo.HasMoreItems=true`, bookmark present |
| `GET /d2l/api/le/1.99/6605/content/root/` | Authenticated browser | Root content JSON; module metadata and nested structure |

**Important limitations:** Browser navigation and DevTools access do **not** prove that a Manifest V3 content script or service worker can issue the same authenticated request. No CLI authentication, extension-to-page authentication, or write access has been demonstrated. The `6605` course is a test fixture only, not a default course. Avoid embedding personal profile data from the test responses in source or fixtures.

## 3. Architecture

```text
Bash terminal
    |
    v
Go executable: d2l (CLI mode)
    |
    v
Authenticated local IPC broker (same Go executable, separate process/mode)
    ^
    |  IPC, request IDs, timeouts
    v
Go executable: d2l native-host (launched by browser)
    ^
    |  Chrome/Edge Native Messaging (length-prefixed JSON)
    v
Manifest V3 extension service worker
    |
    v
Brightspace content script / permitted page context
    |
    v
https://learn.sait.ca/d2l/api/...
```

Chrome/Edge starts the native messaging host, not the CLI. Design a local broker so an independently launched CLI can send requests to a browser-connected host. Prefer a permission-restricted Unix domain socket on Unix-like systems; implement an appropriate Windows IPC backend when targeting Windows. Define a secure broker lifecycle, single-instance behavior, disconnection recovery, and request routing before writing production code. Native host stdout is exclusively for protocol frames; diagnostics go to stderr.

## 4. Technology decisions

- **Go:** CLI, API models, Native Messaging protocol, IPC, output formatting, error handling.
- **Bash:** Initial command-line environment and scripting; `d2l` is a native executable, not a Bash script.
- **Browser extension:** Plain JavaScript, Manifest V3, Edge and Chrome.
- **CLI framework:** Cobra is optional but recommended; compiled into the binary.
- **Dependencies:** Prefer Go standard library for JSON, IPC, CSV, HTTP, and message framing. No Python runtime.
- **Distribution:** One Go binary per OS/architecture plus separately installed extension and native host registration manifest.

## 5. Mandatory first milestone: extension authentication feasibility

1. Create minimal Manifest V3 extension with host permissions restricted to `https://learn.sait.ca/*`.
2. On an authenticated Brightspace tab, attempt read-only `GET /d2l/api/lp/1.63/users/whoami` from a content script using `credentials: 'same-origin'`.
3. Verify actual HTTP status, response content type, and JSON structure. Handle HTML login redirects as authentication failures.
4. Test `myenrollments` and course `6605` root content similarly.
5. Test extension service worker behavior separately; do not assume its request origin and cookies match a page content script.
6. Test after reload, logout, login, browser restart, and session expiration.
7. Do not proceed to the full IPC bridge until authenticated extension requests succeed.

**Acceptance:** A content script can obtain expected JSON through an authorized authenticated browser context, without exporting session secrets. If it cannot, stop and document findings; investigate supported OAuth or institution-approved alternatives rather than bypassing browser protections.

### M1 acceptance test specification (Codex must implement and document)

**Scope:** Test only the browser extension and the four already-observed read-only API routes. Do not build the Go host or CLI during M1. The earlier browser and PowerShell results are baseline evidence, not substitutes for these tests.

**Test prerequisites**

1. Obtain institutional permission to test an unpacked extension and access these course resources.
2. Use a test browser profile with Chrome and/or Edge; install the unpacked Manifest V3 extension through `chrome://extensions` or `edge://extensions` with Developer Mode enabled.
3. Sign in to `https://learn.sait.ca` normally. Keep a Brightspace tab open on that origin.
4. Make the extension UI display a **Run read-only tests** action, a result table, and a **Copy sanitized report** action. Do not run tests automatically on unrelated sites.
5. Each test result must record test ID, execution context (content script vs service worker), method, endpoint path (not full URL with secrets), status, response content type, elapsed milliseconds, pass/fail, and a concise redacted error. Do not display or persist full `whoami`, enrolment, or course-content payloads in test reports.
6. The implementation must use `fetch` with explicit credential handling and validate the final response. A redirect to HTML/SSO is a failure even if the final HTTP status is 200. Do not assume a background service worker has the same cookie behavior as a content script.

**Required functional tests**

| ID | Procedure | Expected result / pass condition |
|---|---|---|
| M1-01 | Load the unpacked extension in Chrome; inspect extension errors. | Manifest V3 loads with no errors, and only necessary permissions are requested. |
| M1-02 | Repeat M1-01 in Edge. | Extension loads without browser-specific code changes. |
| M1-03 | While signed in, click **Run read-only tests** on a `learn.sait.ca` tab; call `GET /d2l/api/versions/` from the content script. | HTTP 200, JSON response, `lp` and `le` products found. Record advertised versions, without hardcoding them as permanent values. |
| M1-04 | From the content script, call `GET /d2l/api/lp/1.63/users/whoami` with `credentials: 'same-origin'`. | HTTP 200 and JSON with expected profile structure; no HTML/SSO response. Do not expose profile fields in the report. |
| M1-05 | From the content script, call `GET /d2l/api/lp/1.63/enrollments/myenrollments/`. | HTTP 200 and JSON containing an `Items` array and `PagingInfo` object. Only the first page is needed for M1; do not dump student/user records. |
| M1-06 | From the content script, call `GET /d2l/api/le/1.99/6605/content/root/`, provided the signed-in tester is authorized for course `6605`. | HTTP 200 and JSON content structure. If not authorized, mark **BLOCKED / fixture unavailable**, not an authentication success or a product failure; choose another authorized course only after documenting the substitution. |
| M1-07 | Execute the same `whoami` request from the extension service worker as a separate diagnostic. | Record HTTP status, final URL origin, content type, and whether JSON was returned. A service-worker failure does not fail M1 if the content-script route passes; the production design must use the proven context. |
| M1-08 | Reload the Brightspace tab and rerun M1-04 and M1-05. | Requests continue to succeed without manually copying any credential. |
| M1-09 | Restart the browser, sign in if prompted, and rerun M1-04. | Success after normal login; if session has expired, show a clear login-required error until the user signs in. |
| M1-10 | Sign out in the browser, then rerun `whoami` without signing in. | Must **not** report authenticated success. HTTP 401/403, SSO redirect, or HTML login page is classified as `AUTH_REQUIRED`, with no retry loop or credential extraction. |
| M1-11 | Restore the signed-in session and rerun M1-04. | Returns to HTTP 200 JSON without reinstalling the extension or modifying secrets. |
| M1-12 | Run the test UI on a non-Brightspace tab. | Refuse execution with a clear wrong-origin message; no API requests are issued. |
| M1-13 | Inspect extension source, storage, console output, and copied report. | No cookies, `Authorization` headers, session IDs, passwords, tokens, or complete personal/course response bodies are extracted, logged, or stored. |
| M1-14 | Simulate a network failure and a non-JSON response using mocked `fetch` in automated tests. | Both produce bounded, actionable errors; no crash, false success, infinite retries, or raw sensitive response dump. |
| M1-15 | Inspect permissions and request dispatch. | Host permissions are restricted to `https://learn.sait.ca/*`; operations use fixed allowlisted GET paths, not arbitrary URLs or JavaScript from messages. |

**Execution-context rules**

- The content script is the primary candidate for authenticated same-origin requests. Explicitly record the result from this context.
- The service worker is a **separate diagnostic**; do not silently substitute its result for the content-script result.
- If a content script fails but the browser DevTools console succeeds, document the difference (status, redirect, cookies/credentials behavior, CSP or extension permission errors) without copying secrets or bypassing protections.
- If a page-context bridge is considered, it requires a separate security review and test: constrain operations, validate `window.postMessage` origins and message shapes, and never inject untrusted code or expose session material. Do not implement it automatically as a fallback.

**Evidence and test report**

Create `docs/m1-test-report.md` with a table containing each test ID, browser/version, PASS/FAIL/BLOCKED/NOT RUN, observed HTTP status (where applicable), execution context, and brief redacted notes. Include instructions for manually running tests and a summary of any unverified assumptions. Automated mock tests supplement but do not replace a live browser acceptance run. Never fabricate live test results in Codex.

**M1 release gate (all required):**

- Chrome **and** Edge load the extension (M1-01/02).
- Signed-in content-script `whoami` returns HTTP 200 JSON in both browsers (M1-04).
- Signed-in `versions` and `myenrollments` requests pass (M1-03/05).
- Content retrieval passes for an authorized test course, or the fixture is explicitly blocked and the project owner approves a substitute before the gate closes (M1-06).
- Reload/restart and logout/login scenarios are handled (M1-08 through M1-11).
- Origin restrictions, secret protection, error handling, and allowlisting pass (M1-12 through M1-15).
- The service-worker diagnostic is recorded (M1-07), regardless of whether it succeeds.
- `docs/m1-test-report.md` contains real observed results, and the user explicitly confirms M1 acceptance.

**Stop condition:** If content-script authentication cannot be demonstrated in an approved context, mark M1 **FAILED/BLOCKED**. Do not proceed to M2, scrape credentials, replay session cookies from Bash, or claim success based solely on DevTools tests. Investigate official OAuth application registration or institution-approved alternatives.

## 6. Native Messaging and IPC milestone

Implement `d2l native-host`:

- Read and write Chrome Native Messaging frames: 4-byte unsigned message length in native byte order followed by UTF-8 JSON.
- Enforce browser protocol size limits and stricter application payload limits.
- Validate message schema, operation names, argument types, course IDs, request IDs, and response sizes.
- Correlate requests and responses; handle disconnects, cancellation, timeouts, and concurrent CLI invocations.
- Restrict host registration to the installed extension ID(s); Edge and Chrome may need separate registration.
- Keep IPC local and current-user-only; authenticate local peers as appropriate.
- Never expose arbitrary URL fetching, arbitrary JavaScript execution, cookie extraction, or unbounded proxy requests.

Proposed message schema (illustrative):

```json
{
  "id": "request-123",
  "operation": "whoami",
  "params": {}
}
```

```json
{
  "id": "request-123",
  "ok": true,
  "data": {"Identifier": "REDACTED"}
}
```

The broker/host should send only approved operation names and sanitized API results.

## 7. CLI commands for MVP

| Command | Purpose | Backend |
|---|---|---|
| `d2l status` | Report extension, native host, IPC, session and API status | Bridge diagnostics |
| `d2l api versions` | Show supported product API versions | Proven public endpoint |
| `d2l whoami` | Show authenticated user | Proven browser endpoint |
| `d2l courses list` | List accessible course offerings | Proven browser endpoint; pagination required |
| `d2l courses list --active` | Filter active course offerings | Client-side response filtering |
| `d2l courses list --format json` | Machine-readable course list | CLI formatting |
| `d2l courses list --format csv` | CSV export | CLI formatting |
| `d2l content list --course 6605` | List root content for course | Proven browser endpoint |

Examples:

```bash
d2l status
d2l api versions
d2l whoami
d2l courses list --active
d2l courses list --format csv > courses.csv
d2l content list --course 6605
```

Default human-readable output should be concise; `--format json` and `--format csv` must be stable for shell pipelines. Use stdout for data, stderr for errors, and nonzero exit codes for failures. Never output student records or personally identifying information unless explicitly requested through an approved operation.

### API-specific details

- Discover supported versions dynamically; retain tested `lp=1.63` and `le=1.99` as initial compatibility targets, not perpetual constants.
- `myenrollments` includes non-course organizational units. Filter course offerings using `OrgUnit.Type.Code` and report the enrolment role and active status when requested.
- Follow `PagingInfo.Bookmark` while `HasMoreItems` is true, using the documented endpoint's pagination parameter. Add duplicate detection, max-page safety, and tests.
- Initial content listing is root-only. Recursive content traversal is out of scope until relevant endpoints are verified.
- Handle HTTP 401/403, 404, 429, 5xx, expired sessions, malformed JSON, and network failures distinctly. Retry only safe requests and respect `Retry-After` when present.

## 8. Suggested repository layout

```text
d2l-cli/
├── README.md
├── go.mod
├── go.sum
├── main.go
├── Makefile
├── cmd/
│   ├── root.go
│   ├── status.go
│   ├── whoami.go
│   ├── courses.go
│   └── content.go
├── internal/
│   ├── api/
│   │   ├── endpoints.go
│   │   ├── models.go
│   │   └── pagination.go
│   ├── native/
│   │   ├── host.go
│   │   └── protocol.go
│   ├── ipc/
│   │   ├── client.go
│   │   └── server.go
│   └── config/
│       └── config.go
├── extension/
│   ├── manifest.json
│   ├── background.js
│   ├── content.js
│   └── popup.html
├── installers/
│   ├── install.sh
│   ├── install-edge.sh
│   └── install-chrome.sh
├── docs/
│   ├── architecture.md
│   ├── authentication.md
│   ├── api-endpoints.md
│   └── security.md
└── tests/
```

The exact directory layout can evolve as implementation reveals OS-specific requirements.

## 9. Security constraints

1. Read-only allowlisted GET operations only for the MVP.
2. Never transfer browser cookies, bearer tokens, or passwords to the Go binary.
3. Restrict extension host permissions to the Brightspace instance and Native Messaging to known extension IDs.
4. Validate all extension messages and local IPC requests; do not trust messages solely because they originated from an extension.
5. Bind local communications to the current user, avoid public network listeners, and use restrictive filesystem permissions.
6. Redact authentication material and unnecessary personal/student data from logs, test fixtures, and crash reports.
7. Use request timeouts, size limits, pagination caps, and explicit error handling.
8. Obtain institutional approval where required; use the API only within the authenticated user's permissions.
9. Do not implement hidden authentication bypasses or undocumented administrative endpoints.
10. Any future write operation must be separately reviewed, documented, tested against a designated test course, and guarded by dry-run/confirmation/audit controls.

## 10. Build and packaging

```bash
mkdir -p dist
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/d2l-linux-amd64 .
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o dist/d2l-linux-arm64 .
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o dist/d2l-darwin-arm64 .
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o dist/d2l-darwin-amd64 .
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o dist/d2l-windows-amd64.exe .
```

Build targets assume all dependencies support CGO-free compilation. Installation must register browser-specific Native Messaging manifests with absolute executable paths and correct extension IDs. Document unpacked-extension ID stability and any enterprise browser restrictions. A single Go binary does not eliminate extension installation or native host registration.

## 11. Milestones and acceptance criteria

### M1 — Browser authentication feasibility (blocking)
- [ ] Complete M1-01 through M1-15 according to the detailed acceptance-test specification in Section 5.
- [ ] Live content-script `whoami` succeeds in Chrome and Edge.
- [ ] Verified API routes and authentication lifecycle pass the M1 release gate.
- [ ] `docs/m1-test-report.md` contains real, sanitized evidence.
- [ ] User explicitly approves M1 before work starts on M2.

### M2 — Native Messaging
- [x] Go host reads/writes framed JSON correctly.
- [x] Extension connects to registered host.
- [x] Malformed/oversized messages are rejected.

### M3 — Bash CLI integration
- [ ] Secure local IPC bridge works.
- [ ] `d2l whoami` returns data through extension.
- [ ] CLI works across browser restarts and reports disconnected state.

### M4 — Read-only API operations
- [ ] `d2l api versions` works.
- [ ] `d2l courses list` handles pagination and filters course offerings.
- [ ] `d2l content list --course ID` retrieves root content.
- [ ] JSON and CSV output are valid.

### M5 — Reliability and security
- [ ] No cookies or tokens stored, forwarded, or logged.
- [ ] Unsupported operations are rejected.
- [ ] Timeout, rate limit, 403, expired session, and disconnect cases are tested.
- [ ] Unit tests cover protocol framing, pagination, validation, and formatting.

### M6 — Distribution
- [ ] Compile for Linux, macOS, and Windows.
- [ ] Chrome and Edge native-host registration is documented and tested.
- [ ] Fresh install requires no Python/Node runtime.

## 12. Out of scope for the first release

- Creating or editing assignments, quizzes, grades, and content.
- Course copy and bulk modification.
- Automatic extraction of browser cookies or OAuth tokens.
- Arbitrary API proxying from Bash.
- Recursive module traversal without verified API coverage.
- Browser UI automation as a silent fallback.

## 13. Instructions for Codex

Implement incrementally. **Start with M1 only.** Produce a minimal loadable Chrome/Edge Manifest V3 extension and a short test procedure for the authenticated `whoami` request. Do not scaffold a complex Go broker until extension authentication is demonstrated. Once M1 is verified by the user, implement M2 and M3 using one Go executable with `native-host` and CLI modes, then add M4. Maintain tests, document assumptions, and stop at any security or authentication blocker rather than improvising a bypass.

**Documentation references:**
- Brightspace Developer Platform: https://docs.valence.desire2learn.com/
- Chrome Native Messaging: https://developer.chrome.com/docs/extensions/develop/concepts/native-messaging
- Chrome extension samples: https://github.com/GoogleChrome/chrome-extensions-samples
