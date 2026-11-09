// downmygo's browser side: a context menu item, an optional takeover of
// the browser's own downloads, and the native messaging pipe to the app.

const HOST = "app.downmygo.host";

// send hands one message to the app, and answers its reply.
function send(message) {
  return new Promise((resolve) => {
    chrome.runtime.sendNativeMessage(HOST, message, (reply) => {
      if (chrome.runtime.lastError) {
        resolve({ ok: false, message: chrome.runtime.lastError.message });
        return;
      }
      resolve(reply || { ok: false, message: "no answer" });
    });
  });
}

// cookiesFor reads the cookies the site would send, so a login carries
// over to the download.
async function cookiesFor(url) {
  try {
    const list = await chrome.cookies.getAll({ url });
    return list.map((c) => ({
      name: c.name,
      value: c.value,
      domain: c.domain,
      path: c.path,
      secure: c.secure,
      httpOnly: c.httpOnly,
      expirationDate: c.expirationDate,
    }));
  } catch (e) {
    return [];
  }
}

async function addLink(url, referer, filename) {
  const cookies = await cookiesFor(url);
  return send({ type: "add", url, referer: referer || "", filename: filename || "", cookies });
}

chrome.runtime.onInstalled.addListener(() => {
  chrome.contextMenus.create({
    id: "downmygo-link",
    title: "Download with downmygo",
    contexts: ["link", "video", "audio", "image"],
  });
  chrome.contextMenus.create({
    id: "downmygo-page",
    title: "Download this page with downmygo",
    contexts: ["page"],
  });
});

chrome.contextMenus.onClicked.addListener(async (info, tab) => {
  const url = info.linkUrl || info.srcUrl || info.pageUrl;
  if (!url) return;
  const reply = await addLink(url, tab ? tab.url : "", "");
  notify(reply, url);
});

// The browser's own downloads can be taken over when the user asks.
chrome.downloads.onCreated.addListener(async (item) => {
  const { takeOver } = await chrome.storage.local.get("takeOver");
  if (!takeOver) return;
  if (!/^https?:/i.test(item.url)) return;
  try {
    await chrome.downloads.cancel(item.id);
    await chrome.downloads.erase({ id: item.id });
  } catch (e) {
    // It may have finished already; sending it anyway is harmless.
  }
  const reply = await addLink(item.url, item.referrer || "", item.filename || "");
  notify(reply, item.url);
});

chrome.runtime.onMessage.addListener((message, sender, respond) => {
  if (message && message.type === "add") {
    addLink(message.url, message.referer, message.filename).then(respond);
    return true;
  }
  if (message && message.type === "grab") {
    send({ type: "grab", page: message.page }).then(respond);
    return true;
  }
  if (message && message.type === "ping") {
    send({ type: "ping" }).then(respond);
    return true;
  }
  return false;
});

// notify says what happened, in the browser's own notifications.
function notify(reply, url) {
  if (!reply || reply.ok) return;
  console.warn("downmygo:", reply.message, url);
  if (chrome.action && chrome.action.setBadgeText) {
    chrome.action.setBadgeText({ text: "!" });
    chrome.action.setBadgeBackgroundColor({ color: "#D92D20" });
    setTimeout(() => chrome.action.setBadgeText({ text: "" }), 4000);
  }
}
