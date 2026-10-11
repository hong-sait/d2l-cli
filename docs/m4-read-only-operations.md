# M4 — Read-only API operations

M4 adds three allowlisted read operations to the existing Chrome/Edge Native Messaging bridge:

```sh
d2l api versions
d2l courses list [--active] [--format table|json|csv]
d2l content list --course 6605 [--format table|json|csv]
```

All API requests originate in the extension service worker, use `GET`, and include browser credentials. The extension accepts only the operation names `api.versions`, `courses.page`, and `content.list`; it constructs URLs from the fixed Brightspace origin and fixed route templates. The course ID is validated as decimal digits. The CLI cannot pass an arbitrary URL or HTTP method.

`api versions` discovers the current `lp` and `le` versions from `/d2l/api/versions/`. Course listing and content listing do their own version discovery before calling the corresponding fixed route. The extension returns only course ID, name, code, type code, role, and active state; root content returns only item ID, title, and module/topic type. User records and raw API response bodies are not sent over Native Messaging.

Course listing follows `PagingInfo.Bookmark` while `HasMoreItems` is true. The CLI rejects missing or repeated course IDs/bookmarks and stops after 100 pages. It filters `OrgUnit.Type.Code` to Course Offering; `--active` additionally keeps records whose active state is true. Table output is tab-delimited for readable terminal use. JSON is one array of objects. CSV includes a header row and quotes fields according to RFC 4180 escaping rules.

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

For an existing installation managed by this repository, rebuild it with `./installers/install-chrome-native-host.sh <extension-id> --upgrade`. The installer checks the existing binary's Go build metadata and extension ID, then keeps timestamped backups before replacing it. Reload the extension and restart Chrome afterward.

## User-provided live evidence (2026-10-11)

- `d2l courses list` returned course offerings, showing that the list command and course-type filter worked. The `role` and `active` columns were blank.
- `d2l courses list --active` returned only its header, so active filtering failed.
- `d2l content list --course 887055` returned 10 root modules with IDs and titles.

The enrollment mapper initially looked for role and active fields in the wrong part of the response. Brightspace documents them under `Access.ClasslistRoleName` and `Access.IsActive` ([MyOrgUnitInfo schema](https://docs.valence.desire2learn.com/res/enroll.html#enrollment-myorgunitinfo)). The extension projection and regression test have been corrected; rerun the course commands after updating/reloading the extension and host. M4 acceptance stays open until `--active` returns active offerings and the output columns are populated.
