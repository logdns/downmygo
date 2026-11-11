# downmygo · Chrome / Edge / Brave 扩展

把浏览器里的链接交给本机的 downmygo，并带上站点需要的 Cookie，所以需要登录的文件也能下。

## 本地安装

1. 启动一次 downmygo，让它生成 API 令牌。
2. 打开 `chrome://extensions`（Edge 是 `edge://extensions`），打开「开发者模式」。
3. 「加载已解压的扩展程序」，选这个目录。
4. 记下扩展 ID，注册本机程序：

```sh
downmygo install-host <扩展ID>
```

5. 重启浏览器。工具栏图标点开应显示 `v1.4.0` 之类的版本号。

## 用法

- 右键链接 / 视频 / 图片 → **Download with downmygo**
- 右键页面 → **Download this page with downmygo**
- 右键页面 → **Grab the links of this page**（把页面上所有可下载链接一起发过去）
- 点工具栏图标 → 发送当前地址、抓取本页链接、开关「接管浏览器下载」

## 权限说明（商店审核用）

| 权限 | 为什么 |
|---|---|
| `contextMenus` | 右键菜单里的三项 |
| `cookies` | 把站点的 Cookie 一起发给本机，登录才能下的文件才下得下来；Cookie 只发给 `127.0.0.1` 上的 downmygo，不上传任何服务器 |
| `downloads` | 用户打开「接管浏览器下载」后，取消浏览器的下载并转给 downmygo |
| `storage` | 记住「接管」开关 |
| `activeTab` | 读取当前标签页的地址，用于弹窗 |
| `nativeMessaging` | 与本机的 downmygo 通信，这是唯一的数据通道 |
| `host_permissions: <all_urls>` | 用户可能在任意站点上右键下载；扩展只在用户操作时读取当前页地址与该链接的 Cookie |

没有远程代码，没有分析统计，没有任何网络请求（除本机 `127.0.0.1` 外）。

## 商店文案

**名称**（≤45 字符）

```
downmygo — send downloads to the app
```

**简短说明**（≤132 字符）

```
Send links, videos and galleries to downmygo, with the cookies the site needs. One click, no copy and paste.
```

**详细说明**

```
downmygo is a download manager for macOS, Windows and Linux. This extension hands a link to the app running on the same computer, so the file downloads there — with the site's cookies, so a file behind a login comes down too.

• Right-click a link, video, image or page: "Download with downmygo".
• Grab every downloadable link on a page in one go.
• Turn on "Take over the browser's downloads" and the browser's own downloads go to downmygo instead.
• The extension talks only to 127.0.0.1, through the browser's native messaging. Nothing is sent anywhere else.

Requires the downmygo app: https://2sb.org/downmygo/
```

**中文说明**

```
downmygo 是 macOS、Windows、Linux 上的下载管理器。这个扩展把链接交给同一台电脑上的应用，由它来下载——并带上站点的 Cookie，所以需要登录的文件也能下。

• 右键链接、视频、图片或页面：「Download with downmygo」。
• 一次抓取页面上所有可下载的链接。
• 打开「接管浏览器下载」后，浏览器自己的下载会转给 downmygo。
• 扩展只通过浏览器的 native messaging 与 127.0.0.1 通信，不向任何服务器发送数据。

需要先安装 downmygo 应用：https://2sb.org/downmygo/
```

**类别**：Productivity
**语言**：English、中文（简体）、中文（繁體）

## 商店素材

`store/` 目录：

| 文件 | 尺寸 | 用途 |
|---|---|---|
| `icon-128.png` | 128×128 | 商店图标 |
| `small-tile-440x280.png` | 440×280 | 小型宣传图 |
| `marquee-1400x560.png` | 1400×560 | 顶部宣传图 |
| `screenshot-1280x800.png` | 1280×800 | 截图（英文） |
| `screenshot-zh-1280x800.png` | 1280×800 | 截图（中文） |

## 打包

```sh
make extensions        # dist/downmygo-chrome-1.0.0.zip
```

压缩包根目录就是 `manifest.json`，直接上传即可。

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
{ "ok": true, "version": "1.4.0" }
{ "ok": true, "links": ["https://…"] }
```
