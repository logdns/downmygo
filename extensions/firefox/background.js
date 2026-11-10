// downmygo's browser side: a context menu item, an optional takeover of
// the browser's own downloads, and the native messaging pipe to the app.
//
// The same file ships in the Chrome and the Firefox extension; the
// manifests differ only in how the background runs.

const HOST = "app.downmygo.host";

// send hands one message to the app, and answers its reply.
function send(message) {
  return new Promise((resolve) => {
    try {
      chrome.runtime.sendNativeMessage(HOST, message, (reply) => {
        if (chrome.runtime.lastError) {
          resolve({ ok: false, message: chrome.runtime.lastError.message });
          return;
        }
        resolve(reply || { ok: false, message: "no answer" });
      });
    } catch (e) {
      resolve({ ok: false, message: String(e) });
    }
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

// badge says what happened, on the toolbar button.
function badge(text, color) {
  if (!chrome.action || !chrome.action.setBadgeText) return;
  chrome.action.setBadgeBackgroundColor({ color: color || "#12B76A" });
  chrome.action.setBadgeText({ text });
  setTimeout(() => chrome.action.setBadgeText({ text: "" }), 4000);
}

function report(reply, url) {
  if (reply && reply.ok) {
    badge("✓");
    return;
  }
  const why = (reply && reply.message) || "no answer";
  console.warn("downmygo:", why, url);
  badge("!", "#D92D20");
}

chrome.runtime.onInstalled.addListener(() => {
  chrome.contextMenus.removeAll(() => {
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
    chrome.contextMenus.create({
      id: "downmygo-grab",
      title: "Grab the links of this page",
      contexts: ["page"],
    });
  });
});

chrome.contextMenus.onClicked.addListener(async (info, tab) => {
  if (info.menuItemId === "downmygo-grab") {
    const reply = await send({ type: "grab", page: tab ? tab.url : info.pageUrl });
    if (reply && reply.ok) {
      const links = reply.links || [];
      for (const link of links) {
        await addLink(link, tab ? tab.url : "", "");
      }
      badge(String(links.length));
    } else {
      report(reply, info.pageUrl);
    }
    return;
  }
  const url = info.linkUrl || info.srcUrl || info.pageUrl;
  if (!url) return;
  report(await addLink(url, tab ? tab.url : "", ""), url);
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
  report(await addLink(item.url, item.referrer || "", item.filename || ""), item.url);
});

chrome.runtime.onMessage.addListener((message, sender, respond) => {
  if (!message) return false;
  if (message.type === "add") {
    addLink(message.url, message.referer, message.filename).then(respond);
    return true;
  }
  if (message.type === "grab") {
    send({ type: "grab", page: message.page }).then(respond);
    return true;
  }
  if (message.type === "ping") {
    send({ type: "ping" }).then(respond);
    return true;
  }
  return false;
});
