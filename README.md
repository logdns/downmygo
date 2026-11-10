<p align="center">
  <img src="resources/icon.png" width="112" alt="downmygo">
</p>

<h1 align="center">downmygo</h1>
<p align="center"><b>下载，即刻启程。</b></p>

<p align="center">
  Windows · macOS · Linux &nbsp;|&nbsp; 原生界面，无 WebView &nbsp;|&nbsp; 单二进制 &nbsp;|&nbsp; MIT
</p>

<p align="center">
  <a href="https://github.com/logdns/downmygo/actions/workflows/ci.yml"><img src="https://github.com/logdns/downmygo/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/logdns/downmygo/releases"><img src="https://img.shields.io/github/v/release/logdns/downmygo" alt="release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT"></a>
</p>

<p align="center">
  简体中文 · <a href="README.en.md">English</a>
</p>

<p align="center">
  <img src="docs/screens/main-light-zh-Hans.png" alt="downmygo" width="920">
</p>

---

## 它是什么

用 **Go + [MyGo](https://github.com/egoist/mygo)** 写的跨平台下载管理器。界面由 MyGo 在 GPU 上原生绘制，不启动 WebView；整个应用是一个二进制。

不是 IDM 的复刻，也不是任何下载器的 fork：保留下载器最核心的工作流，用更克制的界面重做一遍。

## 功能

**下载** · 多连接（1–32，剩余最大段动态二分）· 断点续传 · 单流回退 · SHA-256 校验 · 重试 · 代理 · 限速与流量上限

**种子** · 内置 BitTorrent 客户端（DHT、tracker、peer）· `magnet:` 与 `.torrent` 直接下载 · 暂停/继续 · 做种（按分享率或时长）· 上传限速

**站点** · yt-dlp 视频、gallery-dl 图库（**没装就自己装**）· 浏览器扩展（Chrome / Firefox）+ 油猴脚本 · 站点登录 · 捕获规则 · 链接抓取 · 链接导入

**订阅与解压** · RSS / Atom 订阅 · zip / 7z / rar / tar / gz / bz2 / xz 自动解压（拒绝路径穿越）

**网络** · 加密 DNS（DoH / DoT）· VPN 网卡绑定 · debrid（Real-Debrid / AllDebrid）

**自动化** · 本地 API + 手机远程页 · MCP 服务 · 命令行 · `downmygo://` 与 `magnet:` 深链

**外观** · 三种语言（English / 简体中文 / 繁體中文）· 浅色/深色/跟随系统 · 主题文件 · 自适应布局

**更新** · 检查 GitHub Release、校验 SHA-256、下载并替换自身（可静默）

<p align="center">
  <img src="docs/screens/main-dark.png" alt="downmygo，深色" width="920">
</p>

## 下载

[Releases](https://github.com/logdns/downmygo/releases) 里按平台取：

| 平台 | 文件 |
|---|---|
| macOS | `downmygo-<版本>.dmg`（universal，arm64 + x86_64） |
| Windows | `downmygo-setup-<版本>-<架构>.exe`（安装包）或 `downmygo-<版本>-windows-<架构>.exe`（免安装） |
| Linux | `downmygo_<版本>_<架构>.deb` 或 `downmygo-<版本>-linux-<架构>.tar.gz` |

## 构建

需要 Go 1.27+。

```sh
git clone https://github.com/logdns/downmygo
cd downmygo
go run .                 # 开发运行
go tool mygo build       # 打包，产物在 build/
go test ./...            # 测试
```

## 命令行

```sh
downmygo get URL -o DIR -c 8 --checksum sha256:<hex>
downmygo import links.txt      # 导入链接列表
downmygo grab https://…        # 列出页面上的链接
downmygo info                  # 版本、API 端口、工具状态
downmygo mcp                   # 以 MCP 服务运行（stdio）
downmygo install-host <扩展ID>  # 注册浏览器扩展的本机程序
```

进度写 stderr，最终路径写 stdout。退出码：0 成功、1 失败、2 用法错误。命令行保持英文。

## 本地 API

```sh
TOKEN=$(python3 -c 'import json;print(json.load(open("'"$HOME"'/Library/Application Support/downmygo/state.json"))["settings"]["apiToken"])')
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8463/api/downloads
curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
     -d '{"url":"https://example.com/a.zip"}' http://127.0.0.1:8463/api/add
```

设置 → 高级 → 「允许局域网内的手机控制」后，手机打开 `http://<电脑IP>:8463` 即可。

## 文档

| 文档 | 内容 |
|---|---|
| [架构](docs/01-architecture.md) | 目录、线程模型、下载引擎、种子、持久化、UI、API、构建 |
| [功能与状态](docs/02-features.md) | 每项功能做到什么程度、程序逻辑、已知边界 |
| [设计](docs/03-design.md) | 品牌、颜色、布局、自适应、语言 |
| [参考：MDM](docs/04-mdm.md) | 对 Mister Download Manager 的分析与功能对照 |
| [插件](extensions/README.md) | Chrome / Firefox 扩展与油猴脚本：安装、商店文案、素材、打包 |

## 许可

MIT。见 [LICENSE](LICENSE)。downmygo 是独立实现，不含任何其他下载器的代码、图标或文案。
