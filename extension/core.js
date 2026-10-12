(function (root, factory) {
  const api = factory();
  if (typeof module === "object" && module.exports) module.exports = api;
  root.D2LM1 = api;
})(typeof globalThis !== "undefined" ? globalThis : this, function () {
  "use strict";

  const BASE = "https://learn.sait.ca";
  const TESTS = [
    { id: "M1-03", path: "/d2l/api/versions/", validate: versions },
    { id: "M1-04", path: "/d2l/api/lp/1.63/users/whoami", validate: profile },
    { id: "M1-05", path: "/d2l/api/lp/1.63/enrollments/myenrollments/", validate: enrollments },
    { id: "M1-06", path: "/d2l/api/le/1.99/6605/content/root/", validate: content }
  ];

  function versions(data) {
    if (!Array.isArray(data)) return false;
    const products = new Map(data.filter(item => item && typeof item === "object")
      .map(item => [item.ProductCode, item]));
    return ["lp", "le"].every(code => {
      const item = products.get(code);
      return item && typeof item.LatestVersion === "string" && Array.isArray(item.SupportedVersions);
    });
  }
  function profile(data) {
    return !!data && typeof data === "object" && !Array.isArray(data) &&
      (typeof data.Identifier === "string" || typeof data.Identifier === "number") &&
      typeof data.UniqueName === "string";
  }
  function enrollments(data) {
    return !!data && Array.isArray(data.Items) && !!data.PagingInfo && typeof data.PagingInfo === "object";
  }
  function content(data) {
    return !!data && typeof data === "object" &&
      (Array.isArray(data) || (!Array.isArray(data) && Object.keys(data).length > 0));
  }

  function safeError(error) {
    if (error && error.name === "AbortError") return "Request timed out.";
    if (error && error.message === "Failed to fetch") return "Network request failed; check connectivity and Brightspace access.";
    return "Request failed; check connectivity, sign-in, and extension permissions.";
  }

  async function request(test, fetchImpl, options) {
    const started = Date.now();
    const result = { id: test.id, context: options.context, method: "GET", path: test.path,
      status: null, contentType: "", elapsedMs: 0, result: "FAIL", error: "" };
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), options.timeoutMs || 10000);
    try {
      const response = await fetchImpl(BASE + test.path, {
        method: "GET", credentials: options.credentials,
        redirect: "follow", cache: "no-store", signal: controller.signal,
        headers: { "Accept": "application/json" }
      });
      result.status = response.status;
      result.contentType = (response.headers.get("content-type") || "").split(";")[0].trim().slice(0, 80);
      const isJson = /(?:^|\/)json$/i.test(result.contentType) || /\+json$/i.test(result.contentType);
      if (options.includeFinalOrigin) {
        try { result.finalOrigin = new URL(response.url).origin; } catch (_) { result.finalOrigin = "unknown"; }
        result.jsonReturned = isJson;
      }
      if (response.redirected || !response.url.startsWith(BASE + "/")) {
        result.result = "AUTH_REQUIRED";
        result.error = "Redirected away from Brightspace; sign in and retry.";
      } else if (!response.ok) {
        if (test.id === "M1-06" && response.status === 403) {
          result.result = "BLOCKED";
          result.error = "Fixture course access unavailable; ask the project owner to approve an authorized substitute.";
        } else {
          result.result = (response.status === 401 || response.status === 403) ? "AUTH_REQUIRED" : "FAIL";
          result.error = result.result === "AUTH_REQUIRED" ? "Sign in to Brightspace and retry." : `HTTP ${response.status}.`;
        }
      } else if (!isJson) {
        result.result = "AUTH_REQUIRED";
        result.error = "Expected JSON but received a non-JSON response; sign in and retry.";
      } else {
        let data;
        try { data = await response.json(); }
        catch (_) {
          result.error = "The server returned invalid JSON.";
          return result;
        }
        if (!test.validate(data)) result.error = "JSON response did not match the expected structure.";
        else {
          result.result = "PASS";
          if (test.id === "M1-03") {
            const products = new Map(data.map(item => [item.ProductCode, item]));
            result.advertisedVersions = { lp: products.get("lp").LatestVersion, le: products.get("le").LatestVersion };
          }
        }
      }
    } catch (error) {
      result.error = safeError(error);
    } finally {
      clearTimeout(timer);
      result.elapsedMs = Date.now() - started;
    }
    return result;
  }

  async function runContent(fetchImpl) {
    const results = [];
    for (const test of TESTS) results.push(await request(test, fetchImpl, { context: "content script", credentials: "same-origin" }));
    const sessionAuthRequired = results.some(result =>
      (result.id === "M1-04" || result.id === "M1-05") && result.result === "AUTH_REQUIRED");
    const fixtureResult = results.find(result => result.id === "M1-06");
    if (sessionAuthRequired && fixtureResult && fixtureResult.result === "BLOCKED") {
      fixtureResult.result = "AUTH_REQUIRED";
      fixtureResult.error = "Sign in to Brightspace and retry.";
    }
    return results;
  }
  async function runWorker(fetchImpl) {
    const test = { ...TESTS[1], id: "M1-07" };
    return request({ ...test, validate: () => true }, fetchImpl,
      { context: "service worker", credentials: "include", includeFinalOrigin: true });
  }
  async function fetchWhoami(fetchImpl) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 10000);
    try {
      const response = await fetchImpl(BASE + "/d2l/api/lp/1.63/users/whoami", {
        method: "GET", credentials: "include", redirect: "follow", cache: "no-store",
        signal: controller.signal, headers: { "Accept": "application/json" }
      });
      if (response.redirected || !response.url.startsWith(BASE + "/")) {
        return { ok: false, error: { code: "auth_required", message: "Sign in to Brightspace and retry." } };
      }
      if (response.status === 401 || response.status === 403) {
        return { ok: false, error: { code: "auth_required", message: "Sign in to Brightspace and retry." } };
      }
      if (!response.ok) {
        return { ok: false, error: { code: "http_error", message: `Brightspace returned HTTP ${response.status}.` } };
      }
      const contentType = (response.headers.get("content-type") || "").split(";")[0].trim();
      if (!/(?:^|\/)json$/i.test(contentType) && !/\+json$/i.test(contentType)) {
        return { ok: false, error: { code: "invalid_response", message: "Brightspace returned a non-JSON response." } };
      }
      let data;
      try { data = await response.json(); }
      catch (_) { return { ok: false, error: { code: "invalid_response", message: "Brightspace returned invalid JSON." } }; }
      if (!profile(data)) {
        return { ok: false, error: { code: "invalid_response", message: "Brightspace returned an unexpected profile structure." } };
      }
      const identifier = String(data.Identifier);
      const uniqueName = data.UniqueName;
      const safeField = value => value.length <= 256 && !/[\u0000-\u001f\u007f]/.test(value);
      if (!safeField(identifier) || !safeField(uniqueName)) {
        return { ok: false, error: { code: "invalid_response", message: "Brightspace returned unsafe profile fields." } };
      }
      return { ok: true, data: { Identifier: identifier, UniqueName: uniqueName } };
    } catch (error) {
      return { ok: false, error: { code: error && error.name === "AbortError" ? "timeout" : "network_error",
        message: error && error.name === "AbortError" ? "Brightspace request timed out." : "Brightspace request failed; check connectivity and sign-in." } };
    } finally { clearTimeout(timer); }
  }
  async function fetchReadOperation(operation, params, fetchImpl) {
    let path;
    let target = null;
    if (operation === "api.versions") path = "/d2l/api/versions/";
    else if (operation === "courses.page") {
      const bookmark = typeof params.bookmark === "string" ? params.bookmark : "";
      target = { product: "lp", route: version => `/d2l/api/lp/${version}/enrollments/myenrollments/` + (bookmark ? `?bookmark=${encodeURIComponent(bookmark)}` : "") };
    } else if (operation === "content.list" && typeof params.courseId === "string" && /^\d{1,20}$/.test(params.courseId)) {
      target = { product: "le", route: version => `/d2l/api/le/${version}/${params.courseId}/content/root/` };
    } else if (operation === "students.list" && typeof params.courseId === "string" && /^\d{1,20}$/.test(params.courseId)) {
      target = { product: "le", route: version => `/d2l/api/le/${version}/${params.courseId}/classlist/` };
    } else return { ok: false, error: { code: "invalid_request", message: "Unsupported read-only operation." } };
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 15000);
    try {
      if (target) {
        const versionsResponse = await fetchImpl(BASE + "/d2l/api/versions/", { method: "GET", credentials: "include", redirect: "follow", cache: "no-store", signal: controller.signal, headers: { "Accept": "application/json" } });
        if (versionsResponse.redirected || !versionsResponse.url.startsWith(BASE + "/") || versionsResponse.status === 401 || versionsResponse.status === 403) return { ok: false, error: { code: "auth_required", message: "Sign in to Brightspace and retry." } };
        if (!versionsResponse.ok) return { ok: false, error: { code: versionsResponse.status === 429 ? "rate_limited" : "http_error", message: versionsResponse.status === 429 ? "Brightspace rate limit reached; retry later." : `Brightspace returned HTTP ${versionsResponse.status}.` } };
        const versionsType = (versionsResponse.headers.get("content-type") || "").split(";")[0].trim();
        if (!/(?:^|\/)json$/i.test(versionsType) && !/\+json$/i.test(versionsType)) return invalidData();
        let versionsBody; try { versionsBody = await versionsResponse.json(); } catch (_) { return invalidData(); }
        if (!Array.isArray(versionsBody)) return invalidData();
        const product = versionsBody.find(item => item && item.ProductCode === target.product);
        const version = product && product.LatestVersion;
        if (typeof version !== "string" || !/^\d{1,3}\.\d{1,3}$/.test(version)) return invalidData();
        path = target.route(version);
      }
      const response = await fetchImpl(BASE + path, { method: "GET", credentials: "include", redirect: "follow", cache: "no-store", signal: controller.signal, headers: { "Accept": "application/json" } });
      if (response.redirected || !response.url.startsWith(BASE + "/")) return { ok: false, error: { code: "auth_required", message: "Sign in to Brightspace and retry." } };
      if (response.status === 401 || response.status === 403) return { ok: false, error: { code: "auth_required", message: "Sign in to Brightspace or check course access." } };
      if (response.status === 429) return { ok: false, error: { code: "rate_limited", message: "Brightspace rate limit reached; retry later." } };
      if (!response.ok) return { ok: false, error: { code: "http_error", message: `Brightspace returned HTTP ${response.status}.` } };
      const type = (response.headers.get("content-type") || "").split(";")[0].trim();
      if (!/(?:^|\/)json$/i.test(type) && !/\+json$/i.test(type)) return { ok: false, error: { code: "invalid_response", message: "Brightspace returned a non-JSON response." } };
      let body;
      try { body = await response.json(); } catch (_) { return { ok: false, error: { code: "invalid_response", message: "Brightspace returned invalid JSON." } }; }
      if (operation === "api.versions") {
        if (!Array.isArray(body)) return invalidData();
        const products = new Map(body.filter(x => x && (x.ProductCode === "lp" || x.ProductCode === "le")).map(x => [x.ProductCode, x]));
        const versions = ["lp", "le"].map(productCode => ({ productCode, latestVersion: String(products.get(productCode) && products.get(productCode).LatestVersion || "") }));
        if (versions.some(item => !/^\d{1,3}\.\d{1,3}$/.test(item.latestVersion))) return invalidData();
        return { ok: true, data: { versions } };
      }
      if (operation === "courses.page") {
        if (!body || !Array.isArray(body.Items) || !body.PagingInfo || typeof body.PagingInfo.HasMoreItems !== "boolean") return invalidData();
        const courses = body.Items.map(x => {
          const org = x && x.OrgUnit || {};
          const access = x && x.Access || {};
          const active = typeof access.IsActive === "boolean" ? access.IsActive : (typeof org.IsActive === "boolean" ? org.IsActive : (typeof (x && x.IsActive) === "boolean" ? x.IsActive : null));
          const role = access.ClasslistRoleName || (x && x.Role && (x.Role.Name || x.Role.Code)) || "";
          return { id: String(org.Id || ""), name: String(org.Name || ""), code: String(org.Code || ""), typeCode: String(org.Type && org.Type.Code || ""), role: String(role), active };
        });
        return { ok: true, data: { courses, paging: { bookmark: String(body.PagingInfo.Bookmark || ""), hasMoreItems: body.PagingInfo.HasMoreItems } } };
      }
      if (operation === "students.list") {
        if (!Array.isArray(body) || body.length > 2000) return invalidData();
        const students = body.filter(item => {
          const role = String(item && item.ClasslistRoleDisplayName || "").trim();
          return /^(learner|learners|student|students)$/i.test(role);
        }).map(item => ({ studentId: item && typeof item.Username === "string" && /^\d{9}$/.test(item.Username) ? item.Username : "",
          displayName: item && typeof item.DisplayName === "string" ? item.DisplayName : "" }))
          .filter(item => item.displayName.length > 0 && item.displayName.length <= 256 && !/[\u0000-\u001f\u007f]/.test(item.displayName) &&
            item.studentId.length <= 128 && !/[\u0000-\u001f\u007f]/.test(item.studentId));
        return { ok: true, data: { students } };
      }
      const modules = Array.isArray(body) ? body : (Array.isArray(body.Modules) ? body.Modules : null);
      if (!modules) return invalidData();
      const content = modules.map(x => ({ id: String(x && x.Id || ""), title: String(x && x.Title || ""), type: String(x && x.Type === 0 ? "module" : x && x.Type === 1 ? "topic" : "unknown") }));
      return { ok: true, data: { content } };
    } catch (error) {
      return { ok: false, error: { code: error && error.name === "AbortError" ? "timeout" : "network_error", message: error && error.name === "AbortError" ? "Brightspace request timed out." : "Brightspace request failed; check connectivity and sign-in." } };
    } finally { clearTimeout(timer); }
  }
  function currentTerm(now) {
    const date = now instanceof Date ? now : new Date();
    const month = date.getMonth() + 1;
    const season = month <= 4 ? "Winter" : month <= 8 ? "Sprint" : "Fall";
    return `${season} ${date.getFullYear()}`;
  }
  function normalizeSearch(value) {
    return String(value || "").normalize("NFKD").replace(/[\u0300-\u036f]/g, "")
      .toLocaleLowerCase().replace(/[^\p{L}\p{N}]+/gu, " ").trim();
  }
  function editDistance(left, right) {
    const previous = Array.from({ length: right.length + 1 }, (_, index) => index);
    for (let i = 1; i <= left.length; i++) {
      let diagonal = previous[0];
      previous[0] = i;
      for (let j = 1; j <= right.length; j++) {
        const above = previous[j];
        previous[j] = Math.min(previous[j] + 1, previous[j - 1] + 1,
          diagonal + (left[i - 1] === right[j - 1] ? 0 : 1));
        diagonal = above;
      }
    }
    return previous[right.length];
  }
  function fuzzyNameScore(queryTerms, name) {
    const nameTerms = name.split(/\s+/).filter(Boolean);
    const used = new Set();
    let score = 0;
    for (const queryTerm of queryTerms) {
      let best = null;
      for (let i = 0; i < nameTerms.length; i++) {
        if (used.has(i)) continue;
        const candidate = nameTerms[i];
        const length = Math.max(queryTerm.length, candidate.length);
        const distance = editDistance(queryTerm, candidate);
        const threshold = queryTerm.length < 3 ? 0 : Math.min(2, Math.max(1, Math.floor(queryTerm.length * 0.25)));
        const score = queryTerm.length >= 2 && candidate.startsWith(queryTerm)
          ? 0.1 + 0.1 * (candidate.length - queryTerm.length) / length
          : distance <= threshold ? distance / length : null;
        if (score !== null && (!best || score < best.score)) best = { index: i, score };
      }
      if (!best) return null;
      used.add(best.index);
      score += best.score;
    }
    return score;
  }
  function dedupeStudentMatches(matches) {
    const unique = new Map();
    for (const match of matches) {
      const name = String(match.name || "").trim().replace(/\s+/g, " ");
      const course = String(match.course || "").trim().replace(/\s+/g, " ");
      const key = `${name.toLocaleLowerCase()}\u0000${course.toLocaleLowerCase()}`;
      const previous = unique.get(key);
      if (!previous || (!previous.studentId && match.studentId)) unique.set(key, match);
    }
    return [...unique.values()];
  }
  async function searchStudents(query, fetchImpl, now) {
    const normalized = normalizeSearch(query);
    if (!normalized || normalized.length > 128) return { ok: false, error: { message: "Enter a student name (up to 128 characters)." } };
    const terms = normalized.split(/\s+/).filter(Boolean);
    const term = currentTerm(now);
    const courses = [];
    let bookmark = "";
    for (let page = 0; page < 100; page++) {
      const response = await fetchReadOperation("courses.page", { bookmark }, fetchImpl);
      if (!response.ok) return response;
      const data = response.data;
      courses.push(...data.courses.filter(course => normalizeSearch(course.name).startsWith(normalizeSearch(term))));
      if (!data.paging.hasMoreItems) break;
      if (!data.paging.bookmark || data.paging.bookmark === bookmark) {
        return { ok: false, error: { code: "invalid_response", message: "Brightspace course list pagination did not advance." } };
      }
      bookmark = data.paging.bookmark;
      if (page === 99) return { ok: false, error: { code: "limit_exceeded", message: "Too many course pages to search safely." } };
    }
    const candidates = [];
    for (const course of courses) {
      if (!/^\d{1,20}$/.test(course.id)) continue;
      const response = await fetchReadOperation("students.list", { courseId: course.id }, fetchImpl);
      if (!response.ok) return { ...response, error: { ...response.error, message: `${response.error.message} Course: ${course.code || course.name}.` } };
      for (const student of response.data.students) {
        const name = normalizeSearch(student.displayName);
        candidates.push({ studentId: student.studentId, name: student.displayName, courseId: course.id,
          course: course.name, code: course.code, normalizedName: name });
      }
    }
    const exactMatches = dedupeStudentMatches(candidates.filter(match => {
      const nameTerms = match.normalizedName.split(/\s+/);
      return terms.every(token => nameTerms.includes(token));
    }));
    if (exactMatches.length) {
      return { ok: true, data: { term, searchedCourses: courses.length, exactMatchFound: true, matches: exactMatches } };
    }
    const similar = candidates.map(match => ({ match, score: fuzzyNameScore(terms, match.normalizedName) }))
      .filter(item => item.score !== null)
      .sort((a, b) => a.score - b.score || a.match.name.localeCompare(b.match.name));
    const suggestions = dedupeStudentMatches(similar.map(item => item.match)).slice(0, 25);
    return { ok: true, data: { term, searchedCourses: courses.length, exactMatchFound: false, matches: suggestions } };
  }
  function invalidData() { return { ok: false, error: { code: "invalid_response", message: "Brightspace returned an unexpected response structure." } }; }
  function sanitizedReport(results, browser) {
    return JSON.stringify({ generatedAt: new Date().toISOString(), browser: String(browser || "unknown").slice(0, 80), results }, null, 2);
  }
  return { BASE, TESTS, request, runContent, runWorker, fetchWhoami, fetchReadOperation, searchStudents, sanitizedReport };
});
