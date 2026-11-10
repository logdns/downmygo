// The popup: the current tab's link, a button, grabbing, and the
// takeover switch.

const $ = (id) => document.getElementById(id);

async function currentTab() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  return tab;
}

function ask(message) {
  return new Promise((resolve) => {
    chrome.runtime.sendMessage(message, (reply) => {
      if (chrome.runtime.lastError) {
        resolve({ ok: false, message: chrome.runtime.lastError.message });
        return;
      }
      resolve(reply || { ok: false });
    });
  });
}

function say(text, kind) {
  const note = $("note");
  note.textContent = text;
  note.style.color = kind === "bad" ? "var(--bad)" : "";
}

document.addEventListener("DOMContentLoaded", async () => {
  const tab = await currentTab();
  $("url").value = (tab && tab.url) || "";
  $("url").select();

  const reply = await ask({ type: "ping" });
  const state = $("state");
  if (reply.ok) {
    state.textContent = "v" + reply.version;
    state.className = "state ok";
  } else {
    state.textContent = "not running";
    state.className = "state bad";
    say("Start downmygo, then run `downmygo install-host " +
      (chrome.runtime.id || "EXTENSION_ID") + "` once so the browser can reach it.", "bad");
  }

  const { takeOver } = await chrome.storage.local.get("takeOver");
  $("takeover").checked = !!takeOver;
  $("takeover").addEventListener("change", (e) => {
    chrome.storage.local.set({ takeOver: e.target.checked });
  });

  $("send").addEventListener("click", async () => {
    const url = $("url").value.trim();
    if (!url) return;
    const answer = await ask({ type: "add", url, referer: (tab && tab.url) || "" });
    say(answer && answer.ok ? "Sent: " + (answer.message || "added") : "Could not send: " + ((answer && answer.message) || "no answer"),
      answer && answer.ok ? "ok" : "bad");
  });

  $("grab").addEventListener("click", async () => {
    const page = (tab && tab.url) || $("url").value.trim();
    const answer = await ask({ type: "grab", page });
    if (!answer || !answer.ok) {
      say("Could not read the page: " + ((answer && answer.message) || "no answer"), "bad");
      return;
    }
    let added = 0;
    for (const link of answer.links || []) {
      const r = await ask({ type: "add", url: link, referer: page });
      if (r && r.ok) added++;
    }
    say("Added " + added + " of " + (answer.links || []).length + " links.", "ok");
  });
});
