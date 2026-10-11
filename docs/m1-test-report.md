# M1 browser authentication feasibility report

**Current state: NOT COMPLETE.** This file records actual observations only. Baseline browser and PowerShell evidence in the development plan is not counted as M1 execution. The project owner has explicitly requested M2 work with Edge deferred; that request does not close or waive the M1 release gate.

## Manual run procedure

1. Confirm institutional permission to load and run an unpacked extension against the listed Brightspace resources. Use a test browser profile.
2. In Chrome, open `chrome://extensions`; in Edge, open `edge://extensions`. Enable Developer mode and select **Load unpacked**, choosing this repository's `extension/` directory. Inspect the extension card and errors.
3. Sign in normally to `https://learn.sait.ca` and keep the Brightspace tab open. Reload the tab after loading/reloading the extension so the content script is installed.
4. Click the extension toolbar action. Confirm the displayed tab is on `https://learn.sait.ca`; click **Run read-only tests**. The extension performs four fixed GETs from the content script and a separate `whoami` diagnostic from its service worker. It shows status, content type, elapsed time, result and redacted error, not response payloads. **Copy sanitized report** copies those fields only.
5. Record the browser name/version and observed outcomes in the table. For M1-06, use `BLOCKED` only if the tester is not authorized for fixture course 6605; do not interpret that as an auth pass/fail. A course substitution requires owner approval and documentation first.
6. Reload the Brightspace tab and rerun; restart the browser and rerun after normal sign-in. If a restored tab reports that the content script did not respond, reload that tab once and rerun. Sign out and rerun `whoami`; sign in again and rerun. M1-10 must be `AUTH_REQUIRED`, never success. Open a non-Brightspace tab and verify a wrong-origin refusal and no request.
7. Inspect extension source, extension storage, console, and copied report for secrets or response bodies. Simulate network and non-JSON responses with `node --test extension/core.test.js`.

## Results

| Test ID | Browser/version | Status | HTTP status | Context | Redacted notes |
|---|---|---|---|---|---|
| M1-01 | Chrome 154.0.0.0 | PASS | — | Extension manager | User screenshot shows “Brightspace M1 Authentication Check” v0.1.0 enabled with site access scoped to `https://learn.sait.ca/*`; no extension error indicator is visible. |
| M1-02 | Not run | NOT RUN | — | Edge | User authorized Chrome only; extension has not been loaded in Edge. |
| M1-03 | Chrome 154.0.0.0 | PASS | 200; `application/json` | Content script | 74 ms. Advertised latest versions recorded: `lp=1.63`, `le=1.99`. |
| M1-04 | Chrome 154.0.0.0 | PASS | 200; `application/json` | Content script | 104 ms. Required profile structure passed; no profile fields were included in the report. |
| M1-05 | Chrome 154.0.0.0 | PASS | 200; `application/json` | Content script | 153 ms. `Items` and `PagingInfo` structure passed; enrolment records were not included in the report. |
| M1-06 | Chrome 154.0.0.0 | PASS | 200; `application/json` | Content script | 88 ms. Course 6605 root content returned expected JSON structure; no course content was included in the report. |
| M1-07 | Chrome 154.0.0.0 | PASS | 200; `application/json` | Service worker | 75 ms; final origin `https://learn.sait.ca`; JSON returned. Diagnostic remained separate from content-script results. |
| M1-08 | Chrome 154.0.0.0 | PASS | M1-04: 200; M1-05: 200 | Content script | Brightspace tab was reloaded after extension installation; rerun succeeded (104 ms and 153 ms). |
| M1-09 | Chrome 154.0.0.0 | PASS (after tab reload) | Content `whoami`: 200; worker: 200; `application/json` | Content script; service worker | After browser restart, the restored Brightspace tab initially had no content-script listener. Reloading the tab reinjected the script; rerun passed M1-03 through M1-06 (76/80/140/96 ms) and the service-worker request (72 ms; final origin `https://learn.sait.ca`; JSON returned). The prior popup labeled the worker row M1-04; its ID is corrected to M1-07 for future runs. |
| M1-10 | Chrome 154.0.0.0 | PASS | `whoami`: 403 `text/html`; enrollments: 403 `text/html` | Content script; service worker | Signed-out run returned `AUTH_REQUIRED` for content-script `whoami` (73 ms), enrollments (71 ms), and service-worker `whoami` (239 ms; final origin `https://learn.sait.ca`, JSON false). The unauthenticated course request was incorrectly labeled `BLOCKED` in that run; code now reclassifies it as `AUTH_REQUIRED` when session endpoints fail. |
| M1-11 | Chrome 154.0.0.0 | PASS | 200; `application/json` | Content script | After normal sign-in, `whoami` returned 200 JSON in 78 ms; the full rerun also passed versions, enrollments, course 6605 content, and the service-worker diagnostic. |
| M1-12 | Chrome 154.0.0.0 | PASS | No request issued | Popup | User screenshot shows the explicit wrong-origin refusal on the SSO page and states that no requests were made. |
| M1-13 | Chrome 154.0.0.0 | PASS | — | Extension source, DevTools Application/Console, copied report | User screenshots of the extension service worker DevTools show no Local Storage, Session Storage, Extension Storage, or IndexedDB entries; the Console is empty and reports “No issues.” Source audit found no cookie/authorization handling, storage APIs, or console logging. Supplied copied reports contain sanitized metadata only. |
| M1-14 | Node.js v22.15.0 | PASS (mock) | 200 HTML / network error / 403 fixture | Automated mock | `node --test extension/core.test.js`: 9 passed, 0 failed; includes simulated network error, HTML login at 200, redirect, payload redaction, version parsing, fixture 403 classification during authenticated and signed-out states, and service-worker diagnostic. |
| M1-15 | Chrome 154.0.0.0 | PASS (static) | — | Manifest/source inspection | MV3 manifest, only activeTab and exact Brightspace host permission; four hard-coded paths, GET only, fixed message types. Chrome details screenshot confirms scoped site access. |

## Unverified assumptions and limits

- Live Chrome 154.0.0.0 tests M1-03 through M1-08 passed from the signed-in content script/service worker after the user reloaded the extension and Brightspace tab. The earlier initial run did not reach the content script and applied an incorrect schema validator to M1-07; the corrected run supersedes it.
- The service worker uses `credentials: "include"` only for its isolated diagnostic; its cookie behavior is not assumed to match the page context.
- Test output is held in popup memory and is not written to extension storage. API response bodies are parsed only for shallow structural checks and are discarded.
- This report does not establish institutional approval or Edge compatibility. Chrome extension storage and console were inspected in user-supplied DevTools screenshots for M1-13; no extension storage entries or console messages were visible. The wrong-origin refusal was observed in the user's screenshot, and signed-out behavior was observed in the supplied report. After browser restart, reloading the restored Brightspace tab was required before content-script checks succeeded.
- The M1 release gate remains open until both browsers and all required lifecycle/security checks have actual recorded evidence, and the user explicitly confirms M1 acceptance.
