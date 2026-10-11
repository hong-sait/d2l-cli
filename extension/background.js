"use strict";
importScripts("core.js");

let nativePort = null;
let reconnectTimer = null;
let reconnectDelay = 250;
const pendingPortRequests = new Map();

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (!sender || sender.id !== chrome.runtime.id || !sender.url || !sender.url.endsWith("/popup.html") || !message) return;
  if (message.type === "D2L_M1_RUN_WORKER") {
    D2LM1.runWorker(fetch).then(result => sendResponse({ ok: true, result }))
      .catch(() => sendResponse({ ok: false, error: "Unable to run service-worker diagnostic." }));
    return true;
  }
  if (message.type === "D2L_M2_NATIVE_PING") {
    requestNative("ping").then(result => sendResponse(result));
    return true;
  }
});

chrome.runtime.onStartup.addListener(ensureNativePort);
chrome.runtime.onInstalled.addListener(ensureNativePort);
ensureNativePort();

function ensureNativePort() {
  if (nativePort || reconnectTimer) return;
  try {
    const port = chrome.runtime.connectNative("com.sait.d2l");
    nativePort = port;
    port.onMessage.addListener(handleNativeMessage);
    port.onDisconnect.addListener(() => {
      if (nativePort === port) nativePort = null;
      for (const [id, pending] of pendingPortRequests) {
        clearTimeout(pending.timer);
        pending.resolve({ ok: false, error: "Native Messaging host disconnected." });
        pendingPortRequests.delete(id);
      }
      const delay = reconnectDelay;
      reconnectDelay = Math.min(reconnectDelay * 2, 30000);
      reconnectTimer = setTimeout(() => {
        reconnectTimer = null;
        ensureNativePort();
      }, delay);
    });
    requestNative("ping").then(response => {
      if (response.ok) reconnectDelay = 250;
    });
  } catch (_) {
    const delay = reconnectDelay;
    reconnectDelay = Math.min(reconnectDelay * 2, 30000);
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      ensureNativePort();
    }, delay);
  }
}

function requestNative(operation) {
  ensureNativePort();
  if (!nativePort) return Promise.resolve({ ok: false, error: "Native Messaging host is unavailable." });
  const id = `ext-${crypto.randomUUID()}`;
  return new Promise(resolve => {
    const timer = setTimeout(() => {
      pendingPortRequests.delete(id);
      resolve({ ok: false, error: "Native Messaging host did not respond before the timeout." });
    }, 5000);
    pendingPortRequests.set(id, { resolve, timer });
    try { nativePort.postMessage({ id, operation, params: {} }); }
    catch (_) {
      clearTimeout(timer);
      pendingPortRequests.delete(id);
      resolve({ ok: false, error: "Native Messaging host could not accept the request." });
    }
  });
}

function handleNativeMessage(message) {
  if (!message || typeof message.id !== "string") return;
  if (typeof message.operation === "string") {
    handleHostRequest(message);
    return;
  }
  const pending = pendingPortRequests.get(message.id);
  if (!pending) return;
  clearTimeout(pending.timer);
  pendingPortRequests.delete(message.id);
  const validPing = message.ok === true && message.data && message.data.status === "ready";
  pending.resolve(validPing ? { ok: true, message: "Native Messaging host connected." } :
    { ok: false, error: "Native Messaging host returned an invalid response." });
}

async function handleHostRequest(message) {
  const supported = ["ping", "whoami", "api.versions", "courses.page", "content.list"];
  if (!nativePort || !supported.includes(message.operation) ||
      !message.params || typeof message.params !== "object" || Array.isArray(message.params) ||
      Object.keys(message.params).length !== (message.operation === "courses.page" || message.operation === "content.list" ? 1 : 0)) {
    return;
  }
  let response;
  if (message.operation === "ping") response = { id: message.id, ok: true, data: { status: "ready" } };
  else if (message.operation === "whoami") {
    const result = await D2LM1.fetchWhoami(fetch);
    response = result.ok ? { id: message.id, ok: true, data: result.data } :
      { id: message.id, ok: false, error: result.error };
  } else {
    const result = await D2LM1.fetchReadOperation(message.operation, message.params, fetch);
    response = result.ok ? { id: message.id, ok: true, data: result.data } :
      { id: message.id, ok: false, error: result.error };
  }
  try { nativePort.postMessage(response); } catch (_) {}
}
