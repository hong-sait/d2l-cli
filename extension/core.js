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
  function sanitizedReport(results, browser) {
    return JSON.stringify({ generatedAt: new Date().toISOString(), browser: String(browser || "unknown").slice(0, 80), results }, null, 2);
  }
  return { BASE, TESTS, request, runContent, runWorker, sanitizedReport };
});
