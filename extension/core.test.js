"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { TESTS, request, runContent, runWorker, fetchWhoami, fetchReadOperation, searchStudents, SEARCH_VERSION, sanitizedReport } = require("./core.js");

function response({ status = 200, type = "application/json", url = "https://learn.sait.ca/d2l/api/lp/1.63/users/whoami", body = { Identifier: "redacted" }, redirected = false } = {}) {
  return { status, ok: status >= 200 && status < 300, url, redirected,
    headers: { get: name => name.toLowerCase() === "content-type" ? type : null },
    json: async () => body };
}

test("fixed endpoint set contains only four approved GET checks", () => {
  assert.deepEqual(TESTS.map(t => t.path), [
    "/d2l/api/versions/", "/d2l/api/lp/1.63/users/whoami",
    "/d2l/api/lp/1.63/enrollments/myenrollments/", "/d2l/api/le/1.99/6605/content/root/"
  ]);
});

test("versions check accepts Brightspace product-version array and records only latest lp/le", async () => {
  const body = [
    { ProductCode: "lp", LatestVersion: "1.63", SupportedVersions: ["1.63"] },
    { ProductCode: "le", LatestVersion: "1.99", SupportedVersions: ["1.99"] }
  ];
  const result = await request(TESTS[0], async () => response({ url: "https://learn.sait.ca/d2l/api/versions/", body }),
    { context: "content script", credentials: "same-origin" });
  assert.equal(result.result, "PASS");
  assert.deepEqual(result.advertisedVersions, { lp: "1.63", le: "1.99" });
});

test("fixture-course 403 is blocked rather than auth success or product failure", async () => {
  const result = await request(TESTS[3], async () => response({ status: 403, url: "https://learn.sait.ca/d2l/api/le/1.99/6605/content/root/" }),
    { context: "content script", credentials: "same-origin" });
  assert.equal(result.result, "BLOCKED");
});

test("fixture-course 403 becomes AUTH_REQUIRED when session endpoints show logout", async () => {
  const fetchImpl = async url => {
    if (url.endsWith("/d2l/api/versions/")) return response({
      url, body: [
        { ProductCode: "lp", LatestVersion: "1.63", SupportedVersions: ["1.63"] },
        { ProductCode: "le", LatestVersion: "1.99", SupportedVersions: ["1.99"] }
      ]
    });
    if (url.includes("whoami") || url.includes("myenrollments")) return response({ status: 403, type: "text/html", url });
    return response({ status: 403, type: "text/plain", url });
  };
  const results = await runContent(fetchImpl);
  assert.equal(results.find(row => row.id === "M1-04").result, "AUTH_REQUIRED");
  assert.equal(results.find(row => row.id === "M1-06").result, "AUTH_REQUIRED");
});

test("network failure returns a bounded message without throwing", async () => {
  const result = await request(TESTS[1], async () => { throw new TypeError("Failed to fetch"); },
    { context: "content script", credentials: "same-origin" });
  assert.equal(result.result, "FAIL");
  assert.match(result.error, /Network request failed/);
  assert.doesNotMatch(result.error, /Failed to fetch/);
});

test("HTML login response is AUTH_REQUIRED even when HTTP 200", async () => {
  const result = await request(TESTS[1], async () => response({ type: "text/html", body: "<html>private</html>" }),
    { context: "content script", credentials: "same-origin" });
  assert.equal(result.status, 200);
  assert.equal(result.result, "AUTH_REQUIRED");
  assert.doesNotMatch(JSON.stringify(result), /private|html>/i);
});

test("redirect to SSO is AUTH_REQUIRED and worker report keeps final origin only", async () => {
  const result = await runWorker(async () => response({ url: "https://login.example.test/signin", redirected: true, type: "text/html" }));
  assert.equal(result.result, "AUTH_REQUIRED");
  assert.equal(result.finalOrigin, "https://login.example.test");
  assert.equal(result.jsonReturned, false);
  assert.equal(result.path, TESTS[1].path);
});

test("service-worker diagnostic records HTTP JSON without applying profile-field validation", async () => {
  const result = await runWorker(async () => response({ body: { Identifier: "redacted" } }));
  assert.equal(result.id, "M1-07");
  assert.equal(result.result, "PASS");
  assert.equal(result.jsonReturned, true);
  assert.equal(result.finalOrigin, "https://learn.sait.ca");
});

test("sanitized report cannot contain fetched profile values", async () => {
  const result = await request(TESTS[1], async () => response({ body: { Identifier: "PRIVATE_PROFILE_SENTINEL", UniqueName: "private.unique" } }),
    { context: "content script", credentials: "same-origin" });
  const report = sanitizedReport([result], "test browser");
  assert.doesNotMatch(report, /PRIVATE_PROFILE_SENTINEL/);
  assert.match(report, /M1-04/);
});

test("whoami bridge returns only the allowlisted identity fields", async () => {
  const result = await fetchWhoami(async () => response({ body: {
    Identifier: "12345", UniqueName: "student.example", FirstName: "Private", LastName: "Person",
    Email: "private@example.invalid"
  } }));
  assert.deepEqual(result, { ok: true, data: { Identifier: "12345", UniqueName: "student.example" } });
  assert.doesNotMatch(JSON.stringify(result), /Private|private@example/);
});

test("whoami bridge maps expired sign-in to an actionable sanitized error", async () => {
  const result = await fetchWhoami(async () => response({ status: 403, type: "text/html", body: "private page" }));
  assert.equal(result.ok, false);
  assert.equal(result.error.code, "auth_required");
  assert.match(result.error.message, /Sign in/);
  assert.doesNotMatch(JSON.stringify(result), /private page|html>/i);
});

test("courses page uses only the fixed enrollment route and projects allowlisted course fields", async () => {
  let seen;
  const result = await fetchReadOperation("courses.page", { bookmark: "7&other=x" }, async (url, options) => {
    seen = { url, options };
    if (url.endsWith("/d2l/api/versions/")) return response({ url, body: [{ ProductCode: "lp", LatestVersion: "1.63" }] });
    return response({ url, body: { Items: [{ User: { Identifier: "private" }, OrgUnit: { Id: 42, Name: "Course", Code: "ABC", Type: { Code: "Course Offering" } }, Access: { IsActive: true, ClasslistRoleName: "Learner" } }], PagingInfo: { Bookmark: "42", HasMoreItems: true } } });
  });
  assert.equal(result.ok, true);
  assert.deepEqual(result.data, { courses: [{ id: "42", name: "Course", code: "ABC", typeCode: "Course Offering", role: "Learner", active: true }], paging: { bookmark: "42", hasMoreItems: true } });
  assert.equal(seen.options.method, "GET");
  assert.match(seen.url, /bookmark=7%26other%3Dx$/);
  assert.doesNotMatch(JSON.stringify(result), /private/);
});

test("student list uses the verified classlist route and returns only student IDs and names", async () => {
  const seen = [];
  const result = await fetchReadOperation("students.list", { courseId: "886700" }, async (url, options) => {
    seen.push({ url, options });
    if (url.endsWith("/d2l/api/versions/")) return response({ url, body: [{ ProductCode: "le", LatestVersion: "1.99" }] });
    return response({ url, body: [
      { Username: "000123456", DisplayName: "Example, Student", ClasslistRoleDisplayName: "Student", Email: "private@example.invalid" },
      { Username: "000654321", DisplayName: "Example, Teacher", ClasslistRoleDisplayName: "Instructor" }
    ] });
  });
  assert.equal(result.ok, true);
  assert.deepEqual(result.data, { students: [{ studentId: "000123456", displayName: "Example, Student" }] });
  assert.equal(seen.length, 2);
  assert.equal(seen[1].url, "https://learn.sait.ca/d2l/api/le/1.99/886700/classlist/");
  assert.equal(seen[1].options.method, "GET");
  assert.doesNotMatch(JSON.stringify(result), /private@example/);
});

test("read operations reject arbitrary operation names and unsafe course IDs before fetch", async () => {
  let calls = 0;
  const fetchImpl = async () => { calls++; throw new Error("unexpected"); };
  assert.equal((await fetchReadOperation("proxy", { url: "https://example.com" }, fetchImpl)).ok, false);
  assert.equal((await fetchReadOperation("content.list", { courseId: "1/../2" }, fetchImpl)).ok, false);
  assert.equal((await fetchReadOperation("students.list", { courseId: "1/../2" }, fetchImpl)).ok, false);
  assert.equal(calls, 0);
});

test("student search visits only distinct courses in the current term", async () => {
  const fall = [
    ["884171", "Fall 2026 Advanced Servers (CPSY-302-A)"],
    ["886700", "Fall 2026 IT Service Management (CPSY-352-A)"],
    ["887055", "Fall 2026 Introduction to Hardware (CMPH-209-C)"],
    ["892219", "Fall 2026 Advanced Servers (CPSY-302-B)"],
    ["900145", "Fall 2026 Advanced Servers (CPSY-302-C)"]
  ];
  const winter = Array.from({ length: 5 }, (_, index) => [String(916177 + index), `Winter 2027 Example (${index})`]);
  const fallSections = fall.map(([id, name]) => [`${id}1`, name]);
  const visited = [];
  const fetchImpl = async url => {
    if (url.endsWith("/d2l/api/versions/")) return response({ url, body: [
      { ProductCode: "lp", LatestVersion: "1.63" }, { ProductCode: "le", LatestVersion: "1.99" }
    ] });
    if (url.includes("/enrollments/myenrollments/")) return response({ url, body: {
      Items: [
        ...fall.map(([id, name]) => ({ OrgUnit: { Id: id, Name: name, Type: { Code: "Course Offering" } } })),
        ...winter.map(([id, name]) => ({ OrgUnit: { Id: id, Name: name, Type: { Code: "Course Offering" } } })),
        ...fallSections.map(([id, name]) => ({ OrgUnit: { Id: id, Name: name, Type: { Code: "Section" } } })),
        { OrgUnit: { Id: fall[0][0], Name: fall[0][1], Type: { Code: "Course Offering" } } }
      ],
      PagingInfo: { Bookmark: "", HasMoreItems: false }
    } });
    const match = url.match(/\/([0-9]+)\/classlist\/$/);
    assert.ok(match, `Unexpected request: ${url}`);
    visited.push(match[1]);
    return response({ url, body: [] });
  };
  const result = await searchStudents("Jane", fetchImpl, new Date(2026, 9, 11));
  assert.equal(result.ok, true);
  assert.equal(result.data.searchVersion, SEARCH_VERSION);
  assert.equal(result.data.term, "Fall 2026");
  assert.equal(result.data.searchedCourses, 5);
  assert.deepEqual(visited, fall.map(([id]) => id));
});
