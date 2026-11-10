# downmygo 的浏览器扩展与用户脚本

三个可以单独发布的插件，都在这个目录里，各自自包含、可直接打包上传：

| 目录 | 是什么 | 发布到 |
|---|---|---|
| [`chrome/`](chrome/) | Chrome / Edge / Brave 扩展（Manifest V3） | Chrome Web Store、Edge Add-ons |
| [`firefox/`](firefox/) | Firefox 扩展（Manifest V3，event page） | addons.mozilla.org |
| [`userscript/`](userscript/) | 油猴脚本（Tampermonkey / Violentmonkey / Greasemonkey） | Greasy Fork、GitHub |

它们都做同一件事：把链接交给本机的 downmygo，并带上站点需要的 Cookie。

## 先决条件

本机装有 downmygo，并已启动过一次（它会生成 API 令牌、注册本机程序）。

扩展通过 **Native Messaging** 与本机程序通信，需要注册一次：

```sh
# Chrome / Edge / Brave：扩展 ID 在 chrome://extensions 里
downmygo install-host <扩展ID>

# Firefox：ID 写在 manifest 里，固定为 downmygo@logdns.github.io
downmygo install-host downmygo@logdns.github.io
```

`install-host` 会为 Chrome、Edge、Brave、Firefox 各写一份 `app.downmygo.host.json`，指向 downmygo 可执行文件。之后重启浏览器。

用户脚本不走 Native Messaging，直接调本地 API（`http://127.0.0.1:8463`），第一次使用时输入一次令牌即可。

## 打包

```sh
make extensions          # 生成 dist/downmygo-chrome-1.0.0.zip 与 dist/downmygo-firefox-1.0.0.zip
```

`make extensions` 会先检查两个扩展共用的 `background.js`、`popup.html`、`popup.js` 是否一致，再打包。

## 素材

每个扩展的 `store/` 里是商店要的图，由 `go run ./tools/makestore` 生成：

| 文件 | 尺寸 | 用在哪 |
|---|---|---|
| `icon-128.png` | 128×128 | 商店图标 |
| `small-tile-440x280.png` | 440×280 | Chrome 小型宣传图 |
| `marquee-1400x560.png` | 1400×560 | Chrome 顶部宣传图 |
| `screenshot-1280x800.png` | 1280×800 | 商店截图（英文） |
| `screenshot-zh-1280x800.png` | 1280×800 | 商店截图（中文） |

图标与宣传图和应用本身用同一套几何（`internal/app/icon.go`），改图标后重跑 `make icon` 与 `go run ./tools/makestore`。

## 版本

扩展版本在各自的 `manifest.json` 里，用户脚本在 `@version` 里。发新版时两处一起改。
