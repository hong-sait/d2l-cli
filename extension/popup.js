"use strict";
const runButton = document.getElementById("run");
const copyButton = document.getElementById("copy");
const statusMessage = document.getElementById("message");
const resultBody = document.getElementById("results");
const reportBox = document.getElementById("report");
const nativeButton = document.getElementById("native");
const nativeMessage = document.getElementById("native-message");
const searchForm = document.getElementById("student-search-form");
const searchButton = document.getElementById("student-search-button");
const searchInput = document.getElementById("student-query");
const searchStatus = document.getElementById("student-search-status");
const studentResultBody = document.getElementById("student-results");
const SEARCH_CACHE_KEY = "studentSearchCache";
let searchExpiryTimer = null;

for (const [tabId, panelId] of [["search-tab", "search-panel"], ["testing-tab", "testing-panel"]]) {
  document.getElementById(tabId).addEventListener("click", () => selectTab(tabId, panelId));
}
function selectTab(tabId, panelId) {
  for (const [id, panel] of [["search-tab", "search-panel"], ["testing-tab", "testing-panel"]]) {
    const selected = id === tabId;
    document.getElementById(id).setAttribute("aria-selected", String(selected));
    document.getElementById(id).tabIndex = selected ? 0 : -1;
    document.getElementById(panel).hidden = !selected;
  }
  document.getElementById(tabId).focus();
}

function cell(row, value) {
  const td = document.createElement("td");
  td.textContent = value == null ? "" : String(value);
  row.appendChild(td);
}
function render(results) {
  resultBody.replaceChildren();
  for (const result of results) {
    const row = document.createElement("tr");
    for (const value of [result.id, result.context, `${result.method} ${result.path}`, result.status,
      result.contentType, result.elapsedMs,
      [result.result, result.error, result.advertisedVersions ? `API latest: lp ${result.advertisedVersions.lp}, le ${result.advertisedVersions.le}` : ""].filter(Boolean).join(" — "),
      result.finalOrigin ? `${result.finalOrigin}; JSON ${result.jsonReturned ? "yes" : "no"}` : ""]) cell(row, value);
    resultBody.appendChild(row);
  }
}
function sendTab(tabId, type) {
  return new Promise(resolve => chrome.tabs.sendMessage(tabId, { type }, response => {
    if (chrome.runtime.lastError) resolve({ ok: false, error: "Reload the Brightspace tab, then try again." });
    else resolve(response || { ok: false, error: "No response from the extension." });
  }));
}
function sendWorker() {
  return new Promise(resolve => chrome.runtime.sendMessage({ type: "D2L_M1_RUN_WORKER" }, response => {
    if (chrome.runtime.lastError) resolve({ ok: false, error: "Service-worker diagnostic did not respond." });
    else resolve(response || { ok: false, error: "Service-worker diagnostic did not respond." });
  }));
}
function sendStudentSearch(query) {
  return new Promise(resolve => chrome.runtime.sendMessage({ type: "D2L_STUDENT_SEARCH", query }, response => {
    if (chrome.runtime.lastError) resolve({ ok: false, error: { message: "Student search service did not respond. Reload the extension and retry." } });
    else resolve(response || { ok: false, error: { message: "Student search service did not respond." } });
  }));
}
function renderStudents(matches) {
  const unique = new Map();
  for (const match of matches) {
    const key = `${String(match.name || "").trim().replace(/\s+/g, " ")}\u0000${String(match.course || "").trim().replace(/\s+/g, " ")}`;
    const previous = unique.get(key);
    if (!previous || (!previous.studentId && match.studentId)) unique.set(key, match);
  }
  const displayedMatches = [...unique.values()];
  studentResultBody.replaceChildren();
  if (!displayedMatches.length) {
    const row = document.createElement("tr");
    const td = document.createElement("td");
    td.colSpan = 3;
    td.textContent = "No matching students found.";
    row.appendChild(td);
    studentResultBody.appendChild(row);
    return displayedMatches;
  }
  for (const match of displayedMatches) {
    const row = document.createElement("tr");
    cell(row, match.studentId);
    cell(row, match.name);
    const courseCell = document.createElement("td");
    if (/^\d{1,20}$/.test(String(match.courseId || ""))) {
      const link = document.createElement("a");
      link.href = `${D2LM1.BASE}/d2l/home/${match.courseId}`;
      link.target = "_blank";
      link.rel = "noopener noreferrer";
      link.textContent = match.course || "Open course";
      courseCell.appendChild(link);
    } else {
      courseCell.textContent = match.course || "";
    }
    row.appendChild(courseCell);
    studentResultBody.appendChild(row);
  }
  return displayedMatches;
}
function readSearchCache() {
  return new Promise(resolve => chrome.storage.session.get(SEARCH_CACHE_KEY, result => {
    if (chrome.runtime.lastError) resolve(null);
    else resolve(result && result[SEARCH_CACHE_KEY] || null);
  }));
}
function writeSearchCache(cache) {
  return new Promise(resolve => chrome.storage.session.set({ [SEARCH_CACHE_KEY]: cache }, () => resolve(!chrome.runtime.lastError)));
}
function removeSearchCache() {
  return new Promise(resolve => chrome.storage.session.remove(SEARCH_CACHE_KEY, () => resolve()));
}
function clearSearchResult(expired) {
  if (searchExpiryTimer) clearTimeout(searchExpiryTimer);
  searchExpiryTimer = null;
  studentResultBody.replaceChildren();
  const row = document.createElement("tr");
  const td = document.createElement("td");
  td.colSpan = 3;
  td.textContent = expired ? "Search result expired. Search again." : "No search yet.";
  row.appendChild(td);
  studentResultBody.appendChild(row);
  searchStatus.className = "status";
  searchStatus.textContent = expired ? "Search result expired after one minute. Run the search again." : "Open a Brightspace tab at learn.sait.ca to search.";
}
function showCachedSearch(cache) {
  searchInput.value = cache.query;
  const displayed = renderStudents(cache.matches);
  const courseSummary = `Searched ${cache.searchedCourses} ${cache.term} course${cache.searchedCourses === 1 ? "" : "s"}.`;
  if (cache.exactMatchFound) {
    searchStatus.className = "status match-exact";
    searchStatus.textContent = `${courseSummary} Found ${displayed.length} exact match${displayed.length === 1 ? "" : "es"}.`;
  } else if (displayed.length) {
    searchStatus.className = "status match-suggestions";
    searchStatus.textContent = `No exact match found. Showing ${displayed.length} similar name suggestion${displayed.length === 1 ? "" : "s"}. ${courseSummary}`;
  } else {
    searchStatus.className = "status match-suggestions";
    searchStatus.textContent = `No exact match or similar names found. ${courseSummary}`;
  }
}
async function restoreSearchCache() {
  const cache = await readSearchCache();
  if (!cache) return;
  const expiresAt = Number(cache.expiresAt);
  if (cache.version !== D2LM1.SEARCH_VERSION || !Number.isFinite(expiresAt) || expiresAt <= Date.now() || !Array.isArray(cache.matches) ||
      typeof cache.query !== "string" || typeof cache.term !== "string") {
    await removeSearchCache();
    clearSearchResult(true);
    return;
  }
  showCachedSearch(cache);
  searchExpiryTimer = setTimeout(async () => {
    await removeSearchCache();
    clearSearchResult(true);
  }, expiresAt - Date.now());
}
function browserVersion() {
  const ua = navigator.userAgent;
  const match = ua.match(/(Edg|Chrome)\/([\d.]+)/);
  if (match) return `${match[1] === "Edg" ? "Edge" : "Chrome"} ${match[2]}`;
  const safari = ua.match(/Version\/([\d.]+).*Safari/);
  return safari ? `Safari ${safari[1]}` : "Browser version unavailable";
}

restoreSearchCache().catch(() => {});

searchForm.addEventListener("submit", async event => {
  event.preventDefault();
  const query = searchInput.value.trim();
  if (!query) { searchStatus.textContent = "Enter a student name to search."; return; }
  searchButton.disabled = true;
  searchStatus.className = "status";
  searchStatus.textContent = "Checking the active tab…";
  studentResultBody.replaceChildren();
  if (searchExpiryTimer) clearTimeout(searchExpiryTimer);
  searchExpiryTimer = null;
  try {
    await removeSearchCache();
    const [tab] = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
    let activeUrl;
    try { activeUrl = new URL(tab && tab.url); } catch (_) { activeUrl = null; }
    if (!activeUrl || activeUrl.origin !== D2LM1.BASE) {
      searchStatus.textContent = "Open a Brightspace tab at https://learn.sait.ca, then search again. No requests were made.";
      return;
    }
    searchStatus.textContent = "Searching current-term courses…";
    const response = await sendStudentSearch(query);
    if (!response.ok) {
      searchStatus.textContent = response.error && response.error.message || "Student search failed; check Brightspace access and retry.";
      return;
    }
    if (!response.data || response.data.searchVersion !== D2LM1.SEARCH_VERSION) {
      searchStatus.textContent = "The extension is still running an older search worker. Reload the extension at chrome://extensions or edge://extensions, then search again.";
      return;
    }
    const displayedMatches = renderStudents(response.data.matches);
    const courseSummary = `Searched ${response.data.searchedCourses} ${response.data.term} course${response.data.searchedCourses === 1 ? "" : "s"}.`;
    if (response.data.exactMatchFound) {
      searchStatus.className = "status match-exact";
      searchStatus.textContent = `${courseSummary} Found ${displayedMatches.length} exact match${displayedMatches.length === 1 ? "" : "es"}.`;
    } else if (displayedMatches.length) {
      searchStatus.className = "status match-suggestions";
      searchStatus.textContent = `No exact match found. Showing ${displayedMatches.length} similar name suggestion${displayedMatches.length === 1 ? "" : "s"}. ${courseSummary}`;
    } else {
      searchStatus.className = "status match-suggestions";
      searchStatus.textContent = `No exact match or similar names found. ${courseSummary}`;
    }
    const cache = {
      version: D2LM1.SEARCH_VERSION,
      query,
      term: response.data.term,
      searchedCourses: response.data.searchedCourses,
      exactMatchFound: response.data.exactMatchFound === true,
      matches: displayedMatches,
      expiresAt: Date.now() + 60_000
    };
    if (await writeSearchCache(cache)) {
      searchExpiryTimer = setTimeout(async () => {
        await removeSearchCache();
        clearSearchResult(true);
      }, 60_000);
    }
  } catch (_) {
    searchStatus.textContent = "Student search failed. Reload the extension and Brightspace tab, then retry.";
  } finally { searchButton.disabled = false; }
});

runButton.addEventListener("click", async () => {
  runButton.disabled = true;
  copyButton.disabled = true;
  statusMessage.textContent = "Checking the active tab…";
  try {
    const [tab] = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
    let activeUrl;
    try { activeUrl = new URL(tab && tab.url); } catch (_) { activeUrl = null; }
    if (!activeUrl || activeUrl.origin !== D2LM1.BASE) {
      statusMessage.textContent = "Wrong origin: open a Brightspace tab at https://learn.sait.ca and run again. No requests were made.";
      return;
    }
    statusMessage.textContent = "Running fixed read-only tests…";
    const content = await sendTab(tab.id, "D2L_M1_RUN_CONTENT");
    const rows = content.ok ? content.results : [{ id: "M1-03–06", context: "content script", method: "GET", path: "(allowlisted routes)", status: "", contentType: "", elapsedMs: 0, result: "FAIL", error: content.error }];
    statusMessage.textContent = "Running separate service-worker diagnostic…";
    const worker = await sendWorker();
    rows.push(worker.ok ? worker.result : { id: "M1-07", context: "service worker", method: "GET", path: "/d2l/api/lp/1.63/users/whoami", status: "", contentType: "", elapsedMs: 0, result: "FAIL", error: worker.error, finalOrigin: "unknown", jsonReturned: false });
    render(rows);
    reportBox.value = D2LM1.sanitizedReport(rows, browserVersion());
    copyButton.disabled = false;
    statusMessage.textContent = "Finished. Payloads and profile/course fields are excluded from the report.";
  } catch (_) {
    statusMessage.textContent = "The extension could not complete the checks. Reload it and the Brightspace tab, then retry.";
  } finally { runButton.disabled = false; }
});
copyButton.addEventListener("click", async () => {
  try {
    await navigator.clipboard.writeText(reportBox.value);
    statusMessage.textContent = "Sanitized report copied.";
  } catch (_) {
    reportBox.focus(); reportBox.select();
    statusMessage.textContent = "Clipboard access unavailable; select and copy the sanitized report above.";
  }
});
nativeButton.addEventListener("click", () => {
  nativeButton.disabled = true;
  nativeMessage.textContent = "Checking the registered native host…";
  chrome.runtime.sendMessage({ type: "D2L_M2_NATIVE_PING" }, response => {
    nativeButton.disabled = false;
    if (chrome.runtime.lastError) {
      nativeMessage.textContent = "Native Messaging check failed. Reload the extension and try again.";
      return;
    }
    nativeMessage.textContent = response && response.ok ? response.message :
      (response && response.error) || "Native Messaging check failed.";
  });
});
