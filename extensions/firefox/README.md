# downmygo · Firefox 扩展

把浏览器里的链接交给本机的 downmygo，并带上站点需要的 Cookie。

Firefox 版与 Chrome 版是同一份代码，只有 `manifest.json` 不同：Firefox 用 event page（`background.scripts`），并固定了一个扩展 ID `downmygo@logdns.github.io`，Native Messaging 的注册要用它。

## 本地安装

1. 启动一次 downmygo。
2. 打开 `about:debugging#/runtime/this-firefox` → 「临时载入附加组件」→ 选这个目录的 `manifest.json`。
3. 注册本机程序：

```sh
downmygo install-host downmygo@logdns.github.io
```

4. 重启 Firefox。

临时载入的扩展在重启后消失；要长期使用，请在 `about:config` 里把 `xpinstall.signatures.required` 设为 `false` 后安装打包好的 `.zip`（重命名为 `.xpi`），或直接发布到 addons.mozilla.org。

## 用法

与 Chrome 版一致：右键链接/视频/图片、右键页面、工具栏弹窗。

## 权限说明（AMO 审核用）

| 权限 | 为什么 |
|---|---|
| `contextMenus` | 右键菜单 |
| `cookies` | 把站点的 Cookie 一起发给本机，登录才能下的文件才下得下来；只发给 `127.0.0.1` |
| `downloads` | 「接管浏览器下载」时取消浏览器的下载 |
| `storage` | 记住「接管」开关 |
| `activeTab` | 读取当前标签页地址 |
| `nativeMessaging` | 与本机的 downmygo 通信 |
| `host_permissions: <all_urls>` | 用户可能在任意站点上右键下载 |

没有远程代码，没有分析统计，除本机 `127.0.0.1` 外没有任何网络请求。

## AMO 文案

**名称**：`downmygo`

**摘要**（≤250 字符）

```
Send links, videos and galleries to downmygo, the download manager on your computer, with the cookies the site needs. The extension talks only to 127.0.0.1 through native messaging.
```

**说明**

```
downmygo is a download manager for macOS, Windows and Linux. This extension hands a link to the app on the same computer, so the file downloads there — with the site's cookies, so a file behind a login comes down too.

• Right-click a link, video, image or page: "Download with downmygo".
• Grab every downloadable link on a page in one go.
• Turn on "Take over the browser's downloads" and the browser's own downloads go to downmygo.

Requires the downmygo app: https://github.com/logdns/downmygo

The extension talks only to 127.0.0.1, through native messaging. Nothing is sent anywhere else, and there is no remote code.
```

**类别**：Download Management
**许可**：MIT
**隐私政策**：不需要收集任何数据；若表单要求，可写「This add-on collects no data.」

## 素材

`store/` 目录：

| 文件 | 尺寸 | 用途 |
|---|---|---|
| `icon-128.png` | 128×128 | 列表图标 |
| `tile-440x280.png` | 440×280 | 宣传图（可选） |
| `screenshot-1280x800.png` | 1280×800 | 截图（英文） |
| `screenshot-zh-1280x800.png` | 1280×800 | 截图（中文） |

## 打包

```sh
make extensions        # dist/downmygo-firefox-1.0.0.zip
```

上传到 AMO 时把 `.zip` 改名为 `.xpi` 也可以。
