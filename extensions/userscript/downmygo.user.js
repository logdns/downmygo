// ==UserScript==
// @name         downmygo
// @namespace    https://github.com/logdns/downmygo
// @version      1.0.0
// @description  Send a page, a link or the links of a page to downmygo, with the cookies the site needs.
// @author       downmygo
// @homepageURL  https://github.com/logdns/downmygo
// @supportURL   https://github.com/logdns/downmygo/issues
// @match        http://*/*
// @match        https://*/*
// @grant        GM_xmlhttpRequest
// @grant        GM_getValue
// @grant        GM_setValue
// @grant        GM_registerMenuCommand
// @grant        GM_notification
// @connect      127.0.0.1
// @connect      localhost
// @run-at       document-idle
// @noframes
// ==/UserScript==

(function () {
  "use strict";

  const DEFAULT_PORT = 8463;
  // The kinds of file the grabber keeps; edit this list to taste.
  const EXTENSIONS = [
    "zip", "rar", "7z", "tar", "gz", "xz", "iso",
    "mp4", "mkv", "webm", "avi", "mov", "m4v", "ts",
    "mp3", "flac", "wav", "m4a", "aac", "ogg", "opus",
    "pdf", "epub", "mobi", "doc", "docx", "xls", "xlsx", "ppt", "pptx",
    "exe", "msi", "dmg", "pkg", "deb", "rpm", "apk", "torrent",
  ];

  const port = () => Number(GM_getValue("port", DEFAULT_PORT)) || DEFAULT_PORT;

  function token() {
    let value = GM_getValue("token", "");
    if (!value) {
      value = window.prompt(
        "downmygo's API token, from Options → Advanced → API token:",
        ""
      );
      if (value) GM_setValue("token", value.trim());
    }
    return (value || "").trim();
  }

  // api calls the app's local API. GM_xmlhttpRequest is used so the call
  // is not stopped by the page's cross-origin rules.
  function api(path, method, body) {
    return new Promise((resolve) => {
      const key = token();
      if (!key) {
        resolve({ ok: false, message: "no token" });
        return;
      }
      GM_xmlhttpRequest({
        method: method || "GET",
        url: "http://127.0.0.1:" + port() + path,
        headers: {
          Authorization: "Bearer " + key,
          "Content-Type": "application/json",
        },
        data: body ? JSON.stringify(body) : undefined,
        timeout: 20000,
        onload: (r) => {
          let parsed = null;
          try {
            parsed = JSON.parse(r.responseText || "{}");
          } catch (e) {
            parsed = null;
          }
          if (r.status === 401) {
            GM_setValue("token", "");
            resolve({ ok: false, message: "the token was refused; set it again" });
            return;
          }
          if (r.status >= 400) {
            resolve({ ok: false, message: (r.responseText || "").trim() || "HTTP " + r.status });
            return;
          }
          resolve(Object.assign({ ok: true }, parsed || {}));
        },
        onerror: () => resolve({ ok: false, message: "the app is not answering" }),
        ontimeout: () => resolve({ ok: false, message: "the app did not answer in time" }),
      });
    });
  }

  // add sends one link, with the cookies this page can see.
  async function add(url, referer) {
    const reply = await api("/api/add", "POST", {
      url,
      referer: referer || location.href,
      duplicate: true,
    });
    if (reply.ok) {
      toast("Sent to downmygo: " + (reply.name || url));
      return true;
    }
    // The app may be closed: the deep link opens it instead.
    if (reply.message === "the app is not answering") {
      location.href = "downmygo://add?url=" + encodeURIComponent(url);
      return false;
    }
    toast("downmygo: " + reply.message, true);
    return false;
  }

  // linksOf lists the links of the page a filter keeps.
  function linksOf() {
    const seen = new Set();
    const out = [];
    for (const a of document.querySelectorAll("a[href], video[src], audio[src], source[src]")) {
      let raw = a.getAttribute("href") || a.getAttribute("src") || "";
      if (!raw || raw.startsWith("#") || /^(javascript|mailto|tel|data|blob):/i.test(raw)) continue;
      let url;
      try {
        url = new URL(raw, location.href);
      } catch (e) {
        continue;
      }
      if (url.protocol !== "http:" && url.protocol !== "https:") continue;
      url.hash = "";
      const name = decodeURIComponent(url.pathname.split("/").pop() || "");
      const ext = (name.split(".").pop() || "").toLowerCase();
      if (!EXTENSIONS.includes(ext)) continue;
      const key = url.href;
      if (seen.has(key)) continue;
      seen.add(key);
      out.push({ url: key, name });
    }
    return out;
  }

  async function grab() {
    const links = linksOf();
    if (links.length === 0) {
      toast("No links on this page match the file types.", true);
      return;
    }
    const answer = window.confirm(
      "Send " + links.length + " links from this page to downmygo?"
    );
    if (!answer) return;
    let added = 0;
    for (const link of links) {
      if (await add(link.url, location.href)) added++;
    }
    toast("Added " + added + " of " + links.length + " links.");
  }

  // toast says what happened, in the page's own corner.
  function toast(text, bad) {
    let box = document.getElementById("downmygo-toast");
    if (!box) {
      box = document.createElement("div");
      box.id = "downmygo-toast";
      box.style.cssText = [
        "position:fixed", "right:16px", "bottom:16px", "z-index:2147483647",
        "max-width:360px", "padding:10px 14px", "border-radius:10px",
        "font:13px/1.4 -apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif",
        "color:#fff", "box-shadow:0 8px 24px rgba(0,0,0,.24)", "transition:opacity .3s",
        "pointer-events:none",
      ].join(";");
      document.documentElement.appendChild(box);
    }
    box.style.background = bad ? "#D92D20" : "#10131A";
    box.textContent = text;
    box.style.opacity = "1";
    clearTimeout(box._timer);
    box._timer = setTimeout(() => (box.style.opacity = "0"), 3000);
  }

  // The menu: what a right click on the extension icon would offer.
  GM_registerMenuCommand("Send this page to downmygo", () => add(location.href, location.href));
  GM_registerMenuCommand("Grab the links of this page", grab);
  GM_registerMenuCommand("Set the API token", () => {
    GM_setValue("token", "");
    token();
  });
  GM_registerMenuCommand("Set the API port", () => {
    const value = window.prompt("downmygo's local API port:", String(port()));
    if (value) GM_setValue("port", Number(value) || DEFAULT_PORT);
  });

  // Alt+D sends the page, Alt+Shift+D grabs its links.
  window.addEventListener("keydown", (e) => {
    if (!e.altKey || e.ctrlKey || e.metaKey) return;
    if (e.code !== "KeyD") return;
    e.preventDefault();
    if (e.shiftKey) grab();
    else add(location.href, location.href);
  });
})();
