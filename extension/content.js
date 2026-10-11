"use strict";
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (!sender || sender.id !== chrome.runtime.id || !sender.url || !sender.url.endsWith("/popup.html") ||
      !message || message.type !== "D2L_M1_RUN_CONTENT") return;
  D2LM1.runContent(fetch).then(results => sendResponse({ ok: true, results }))
    .catch(() => sendResponse({ ok: false, error: "Unable to run content-script checks." }));
  return true;
});
