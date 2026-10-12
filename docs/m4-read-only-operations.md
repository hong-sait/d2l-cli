# M4 — Read-only API operations

M4 exposes allowlisted read-only operations through the existing Chrome/Edge Native Messaging bridge:

```sh
d2l api versions
d2l courses list [--all] [--active] [--format table|json|csv]
d2l content list --course 6605 [--format table|json|csv]
d2l students list --course CPSY-302-B [--format table|json|csv]
d2l students search "Doe, John" [--format table|json|csv]
```

All API requests originate in the extension service worker, use `GET`, and include browser credentials. The extension accepts only the operation names `api.versions`, `courses.page`, `content.list`, and `students.list`; it constructs URLs from the fixed Brightspace origin and fixed route templates. Course IDs are validated as decimal digits. The CLI cannot pass an arbitrary URL or HTTP method.

`api versions` discovers the current `lp` and `le` versions from `/d2l/api/versions/`. Course listing, content listing, and student listing do their own version discovery before calling the corresponding fixed route. The extension returns only course ID, name, code, type code, role, and active state to the CLI for course listing; the CLI displays only ID and name. Root content returns only item ID, title, and module/topic type. Student listing uses the verified classlist API, filters for classlist roles labeled `Learner` or `Student`, and returns the nine-digit `Username` as the student ID plus the display name. Other user fields and raw API response bodies are not sent over Native Messaging.

`d2l students list --course` accepts an enrolled course's numeric org-unit ID or a course code such as `CPSY-302-B`, `cpsy302b`, or `302b`. Code lookup uses the current calendar term: January–April is Winter, May–August is Spring, and September–December is Fall, using the current year. A short code must uniquely identify a course in that term. If an exact section is absent, an interactive terminal asks whether to show other available sections and then requires an explicit course code or ID selection. It never silently substitutes a different section. In non-interactive use, it stops and directs the user to choose an ID from `d2l courses list`.

`d2l students search NAME` checks classlists for all courses whose names begin with the current calendar term (for example, Fall 2026 during September–December 2026). It accepts comma-form or natural name order, such as `"Doe, John"` or `John Doe`; a single first or last name token is also supported. Multiword matching ignores order and punctuation and requires each supplied name token to appear in the display name. Output includes one row per matching course enrollment, so a student enrolled in multiple current-term courses appears more than once. The search performs read-only classlist requests for each current-term course visible to the signed-in account.

Student records are fetched only from the verified permission-scoped [Brightspace classlist endpoint](https://docs.valence.desire2learn.com/res/enroll.html) `/d2l/api/le/{version}/{courseId}/classlist/`. The response is a JSON array; the extension caps it at 2,000 entries and projects only student IDs and display names. The displayed student ID comes from `ClasslistUser.Username` and is included only when it contains exactly nine digits. A 403/404 remains an access or visibility error; the CLI does not attempt another route or extract browser credentials. Brightspace classlist visibility and privacy settings apply to the ID and display name; either may be absent if not visible in the classlist.

Course listing follows `PagingInfo.Bookmark` while `HasMoreItems` is true. The CLI rejects missing or repeated course IDs/bookmarks and stops after 100 pages. It filters `OrgUnit.Type.Code` to Course Offering. By default, it shows course names beginning with the current or a future calendar term, such as `Fall 2026` or `Winter 2027` in October 2026. `--all` includes past courses and offerings without a recognized term prefix. `--active` additionally keeps records whose active state is true and can be combined with `--all`. Table output is tab-delimited for readable terminal use. JSON is one array of objects. CSV includes a header row and quotes fields according to RFC 4180 escaping rules.

## Error behavior

- Brightspace 401/403 or an SSO redirect produces a sanitized sign-in/access error.
- HTTP 429 reports that the rate limit was reached and advises retrying later.
- Other non-success HTTP statuses, invalid JSON, and unexpected structures fail with concise messages.
- A missing bridge or browser disconnect exits nonzero; `d2l status` continues to report connected/disconnected state.
- No retry loop is used. These are safe GETs, but retry timing and `Retry-After` handling remain reliability work for M5.

## Local verification

Run `go test ./...` and `node --test extension/core.test.js`. The Go IPC tests require permission to bind Unix-domain sockets, which may need execution outside a restricted sandbox. These tests exercise the local bridge and formatting logic; they do not establish live API results.

## Live check

After building/registering the updated host and reloading the unpacked extension, sign in to Brightspace and run the commands above. Use a course ID you are authorized to access. Course listing may include closed courses; `--active` filters by the API's active flag.

Student listing worked in the user's Chrome session for `CPSY-302-A`, `CPSY-302-B`, and `CPSY-302-C`. The user also verified that `/d2l/api/le/1.99/886700/classlist/` returns a JSON array for `CPSY-352-A`. The revised single-request CLI operation needs a live check after reloading the extension. If no exact section exists in the current term, the command asks whether to show alternatives and then requires an explicit choice. For a non-interactive shell, use `d2l courses list` to select the numeric course ID.

For an existing installation managed by this repository, rebuild it with `./installers/install-chrome-native-host.sh <extension-id> --upgrade`. The installer checks the existing binary's Go build metadata and extension ID, then keeps timestamped backups before replacing it. Reload the extension and restart Chrome afterward.

## User-provided live evidence (2026-10-11)

- `d2l courses list` returned course offerings, showing that the list command and course-type filter worked. The `role` and `active` columns were blank.
- `d2l courses list --active` returned only its header, so active filtering failed.
- `d2l content list --course 887055` returned 10 root modules with IDs and titles.
- `d2l students list` returned nine-digit student IDs and names for `CPSY-302-A`, `CPSY-302-B`, and `CPSY-302-C`.
- A browser request to `/d2l/api/le/1.99/886700/classlist/` returned a JSON array for `CPSY-352-A`. The paged variant also returned `Objects` and a `Next` link. The former CLI failed for this course, so the student operation was changed to use the verified array route.

The enrollment mapper initially looked for role and active fields in the wrong part of the response. Brightspace documents them under `Access.ClasslistRoleName` and `Access.IsActive` ([MyOrgUnitInfo schema](https://docs.valence.desire2learn.com/res/enroll.html#enrollment-myorgunitinfo)). The extension projection and regression test have been corrected; rerun the course commands after updating/reloading the extension and host. M4 acceptance stays open until `--active` returns active offerings. The revised student-list path also needs a live check against `CPSY-352-A`.
