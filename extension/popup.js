"use strict";
const runButton = document.getElementById("run");
const copyButton = document.getElementById("copy");
const statusMessage = document.getElementById("message");
const resultBody = document.getElementById("results");
const reportBox = document.getElementById("report");
const nativeButton = document.getElementById("native");
const nativeMessage = document.getElementById("native-message");

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
function browserVersion() {
  const ua = navigator.userAgent;
  const match = ua.match(/(Edg|Chrome)\/([\d.]+)/);
  if (match) return `${match[1] === "Edg" ? "Edge" : "Chrome"} ${match[2]}`;
  const safari = ua.match(/Version\/([\d.]+).*Safari/);
  return safari ? `Safari ${safari[1]}` : "Browser version unavailable";
}

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
