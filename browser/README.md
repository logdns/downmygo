# downmygo 的浏览器扩展

扩展把浏览器里的链接交给 downmygo，并带上站点需要的 Cookie，所以需要登录的文件也能下。

## 安装

1. 先启动一次 downmygo，让它生成 API 令牌。
2. 在浏览器里加载扩展：
   - **Chrome / Edge / Brave**：打开 `chrome://extensions`，打开「开发者模式」，点「加载已解压的扩展程序」，选这个 `browser/` 目录。
   - **Firefox**：打开 `about:debugging#/runtime/this-firefox`，「临时载入附加组件」，选 `browser/manifest.json`。
3. 记下扩展 ID（`chrome://extensions` 里每个扩展下面那串字母）。
4. 注册本机程序，让浏览器能找到 downmygo：

```sh
downmygo install-host <扩展ID>
```

它会为 Chrome、Edge、Brave、Firefox 各写一份 `app.downmygo.host.json`，指向 downmygo 可执行文件。之后重启浏览器。

## 用法

- 右键链接、视频、图片 → **Download with downmygo**。
- 点工具栏图标 → 把当前页面地址发给 downmygo。
- 打开「Take over the browser's downloads」后，浏览器自己的下载会被取消并转给 downmygo。

## 捕获规则

在 downmygo 的设置里可以写规则（`state.json` 的 `rules`）：只收某些站点、某些扩展名、大于某个大小的文件。没有规则时收下所有链接。

```json
[
  { "host": "example.com", "extensions": ["zip", "mkv"], "minSize": 1048576 },
  { "host": "cdn.example.net", "exclude": "ads" }
]
```

## 站点登录

需要登录的站点，在 `state.json` 的 `sites` 里写一份 Cookie，扩展和下载都会用：

```json
[
  { "host": "example.com", "cookies": "sid=abc123", "referer": "https://example.com/" }
]
```

`host` 按后缀匹配，`cdn.example.com` 会用 `example.com` 的登录。

## 协议

扩展与本机程序之间是 Chrome 的 native messaging：4 字节小端长度 + JSON。

```json
{ "type": "add", "url": "https://…", "referer": "https://…", "cookies": [ … ] }
{ "type": "ping" }
{ "type": "grab", "page": "https://…" }
```

回答：

```json
{ "ok": true, "message": "Added a.zip (1a2b3c)" }
{ "ok": true, "version": "1.0.0" }
{ "ok": true, "links": ["https://…"] }
```
