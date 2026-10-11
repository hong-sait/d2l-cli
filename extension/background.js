"use strict";
importScripts("core.js");
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (!sender || sender.id !== chrome.runtime.id || !sender.url || !sender.url.endsWith("/popup.html") || !message) return;
  if (message.type === "D2L_M1_RUN_WORKER") {
    D2LM1.runWorker(fetch).then(result => sendResponse({ ok: true, result }))
      .catch(() => sendResponse({ ok: false, error: "Unable to run service-worker diagnostic." }));
    return true;
  }
  if (message.type === "D2L_M2_NATIVE_PING") {
    nativePing().then(result => sendResponse(result));
    return true;
  }
});

function nativePing() {
  return new Promise(resolve => {
    let port;
    let timer;
    let settled = false;
    const finish = result => {
      if (settled) return;
      settled = true;
      if (timer) clearTimeout(timer);
      resolve(result);
    };
    const requestId = `m2-${crypto.randomUUID()}`;
    try {
      port = chrome.runtime.connectNative("com.sait.d2l");
    } catch (_) {
      finish({ ok: false, error: "Native host could not be started; check its installation and extension ID." });
      return;
    }
    timer = setTimeout(() => {
      finish({ ok: false, error: "Native host did not respond before the timeout." });
      try { port.disconnect(); } catch (_) {}
    }, 5000);
    port.onMessage.addListener(response => {
      const valid = response && response.id === requestId && response.ok === true &&
        response.data && response.data.status === "ready";
      if (valid) finish({ ok: true, message: "Native Messaging host connected." });
      else finish({ ok: false, error: "Native host returned an invalid response." });
      try { port.disconnect(); } catch (_) {}
    });
    port.onDisconnect.addListener(() => {
      finish({ ok: false, error: "Native host disconnected; check its registration and extension ID." });
    });
    try {
      port.postMessage({ id: requestId, operation: "ping", params: {} });
    } catch (_) {
      finish({ ok: false, error: "Native host could not accept the ping." });
      try { port.disconnect(); } catch (_) {}
    }
  });
}
