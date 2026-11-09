// The popup: the current tab's link, a button, and the takeover switch.

const $ = (id) => document.getElementById(id);

async function currentTab() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  return tab;
}

async function ping() {
  return new Promise((resolve) => {
    chrome.runtime.sendNativeMessage("app.downmygo.host", { type: "ping" }, (reply) => {
      if (chrome.runtime.lastError) {
        resolve({ ok: false, message: chrome.runtime.lastError.message });
        return;
      }
      resolve(reply || { ok: false });
    });
  });
}

document.addEventListener("DOMContentLoaded", async () => {
  const tab = await currentTab();
  $("url").value = (tab && tab.url) || "";
  $("url").select();

  const reply = await ping();
  $("state").textContent = reply.ok ? "v" + reply.version : "not running";
  if (!reply.ok) {
    $("note").textContent = "Start downmygo, and run `downmygo install-host " +
      (chrome.runtime.id || "EXTENSION_ID") + "` once to connect the extension.";
  }

  const { takeOver } = await chrome.storage.local.get("takeOver");
  $("takeover").checked = !!takeOver;
  $("takeover").addEventListener("change", (e) => {
    chrome.storage.local.set({ takeOver: e.target.checked });
  });

  $("send").addEventListener("click", async () => {
    const url = $("url").value.trim();
    if (!url) return;
    const answer = await chrome.runtime.sendMessage({
      type: "add",
      url,
      referer: (tab && tab.url) || "",
    });
    $("note").textContent = answer && answer.ok
      ? "Sent: " + (answer.message || "added")
      : "Could not send: " + ((answer && answer.message) || "no answer");
  });
});
