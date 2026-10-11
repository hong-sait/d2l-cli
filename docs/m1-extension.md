# M1 unpacked extension

The test extension is in `extension/`. It is a plain JavaScript Manifest V3 package intended for Chrome and Edge. It requests only `activeTab` plus the `https://learn.sait.ca/*` host permission, installs a content script only on that origin, and does not run API requests until the popup's **Run read-only tests** button is clicked.

The content script uses `fetch` with `credentials: "same-origin"` for the four fixed GET routes. The service worker separately probes `whoami` with `credentials: "include"`; this result is diagnostic and is not substituted for the content-script result. Messages carry only fixed operation names. There is no arbitrary URL input, token/cookie access, response-body display, or extension storage.

The versions endpoint returns an array of product-version records; M1 records only the `LatestVersion` values for `lp` and `le`. The documented `WhoAmIUser` shape includes `Identifier` and `UniqueName`; the content-script check validates those fields without displaying their values. The service-worker diagnostic records the HTTP/content-type/origin and JSON status independently of the profile schema. See [Brightspace API versions](https://docs.valence.desire2learn.com/res/apiprop.html) and [WhoAmI user shape](https://docs.valence.desire2learn.com/res/user.html).

Load the unpacked directory using the procedure in [the M1 test report](m1-test-report.md). After loading or reloading the extension, reload an already-open Brightspace tab before testing. Run mocked error cases with `node --test extension/core.test.js` (Node is for development verification only, not an end-user runtime).
