# Mister Download Manager：完整逻辑、功能与特点分析

> 分析对象：`github.com/anefzaoui/misterdownloadmanager`（下文简称 MDM）
> 分析版本：仓库 `main`，Cargo workspace `0.5.0`
> 目的：为 downmygo 的重构提供一份可执行的产品与技术底稿。本文只做**行为与架构分析**，不复制其代码；MDM 以 GPL-3.0-or-later 发布，downmygo 是独立实现。

---

## 1. 一句话定位

MDM 是一个**用 Rust 从零重写、界面与工作流刻意对齐 Internet Download Manager（IDM）** 的免费下载管理器：多连接加速、断点续传、浏览器接管、视频/图库/种子下载、队列与定时、限速与 VPN 绑定、远程控制、CLI/API/MCP，覆盖 Windows、Linux，macOS 为实验性支持。

它的产品策略不是“做一个更好的新界面”，而是：

> **让 IDM 老用户一眼认出窗口，然后发现它更快、免费、无广告、开源。**

这决定了它的功能取舍、界面布局、默认值和文案风格。

---

## 2. 目标用户与场景

| 用户 | 特征 | 关键诉求 |
|---|---|---|
| 主力：Windows IDM 用户 | 用过或正在用 IDM，试用到期/使用破解版，非技术用户居多 | 同样的窗口和菜单、更快的下载、真正免费、不要注册 |
| 次级：Linux 用户 | GNOME / KDE / Xfce / Cinnamon，X11 与 Wayland | 可用的下载器、托盘、深色模式、浏览器接管 |
| 次级：macOS 用户 | Apple Silicon / Intel | 能运行（实验性），原生感 |
| 帮助页读者 | 边用边查 | 菜单在哪、选项什么意思、失败怎么办 |
| 贡献者 | 代码、翻译、bug 报告 | 清晰的 crate 边界、测试、i18n 流程 |

非目标：企业级下载调度、云盘同步、媒体库管理、商业 CDN 加速。

---

## 3. 技术栈与规模

| 维度 | 选择 |
|---|---|
| 语言 | Rust 2024，MSRV 1.95 |
| UI | `egui` / `eframe 0.37`（storytold 的 fork），OpenGL(glow) 或 wgpu（Windows D3D12 / Linux Vulkan / macOS Metal） |
| 异步/网络 | Tokio、HTTP/1.1 手写连接池、rustls/系统证书 |
| 种子 | `librqbit` |
| 视频/图库 | 外部程序 `yt-dlp`、`ffmpeg`、`gallery-dl` |
| 解压 | 进程内 zip/tar/gz/xz；7z/RAR 调用系统 7-Zip |
| 数据 | JSON（原子写）、系统钥匙串、TOML/JSON 主题文件 |
| 浏览器扩展 | Chromium MV3 + Firefox WebExtension |
| 网站 | 纯 HTML/CSS/JS，Python 构建，GitHub Pages |
| 许可 | GPL-3.0-or-later |

代码量（约）：

| Crate | 行数 | 角色 |
|---|---:|---|
| `app` | 74,000 | 桌面应用：列表、分类、队列、调度、对话框、托盘、设置、主题、绘制 |
| `engine` | 14,100 | 单文件 HTTP(S)/FTP(S) 下载引擎 |
| `cli` | 1,500 | 无窗口下载、视频、种子、自动化命令 |
| `crash` | 2,400 | 崩溃/卡死报告、minidump、符号化 |
| `debrid` | 3,500 | Real-Debrid / AllDebrid / Premiumize |
| `extract` | 1,500 | 压缩包解压 |
| `geo` | 375 | IP → 国家（DB-IP Lite，本地查表） |
| `ipc` | 2,700 | 单实例、数据目录、浏览器捕获规则 |
| `labserver` | 5,600 | 可注入故障的 HTTP Range / FTP 测试服务器 |
| `mcp` | 672 | MCP Server，给 AI 助手用 |
| `nmhost` | 1,900 | 浏览器 Native Messaging Host |
| `remote` | 2,000 | 手机/局域网远程控制 HTTPS 服务 |
| `tools` | 8,800 | yt-dlp/gallery-dl/ffmpeg 封装、代理中继 |
| `torrent` | 3,700 | BitTorrent / magnet |
| 合计 | ≈133,000 | 不含扩展、网站、文档、工具脚本 |

另有 2,277 个文件、约 110 MB 仓库体积，包含 86 种语言目录、帮助页、基准数据、截图工具、Windows/Linux VM 实验室脚本。

---

## 4. 总体架构

### 4.1 Crate 依赖方向

```
app ─┬─ engine ── (纯网络/文件)
     ├─ ipc ──── (数据目录、单实例、捕获规则)
     ├─ torrent
     ├─ tools
     ├─ debrid
     ├─ extract
     ├─ geo
     └─ crash

cli ─┬─ engine
     ├─ torrent
     └─ tools

mcp ──► ipc / automation API
remote ─► ipc / automation API
nmhost ─► ipc
```

原则：**依赖向内**。`engine` 不知道 UI；`app` 只通过事件（进度、分段表、完成、失败）观察引擎。`cli`、`mcp`、`nmhost`、`remote` 都复用同一套命令与数据层。

### 4.2 进程与窗口模型

- 主进程：一个 egui 窗口绘制到固定像素坐标，每个对话框一个 immediate viewport。
- 单实例：带 token 的本地 socket；第二次启动把命令行参数交给第一次启动。
- `--headless`：同一个 `App` 不创建 eframe，只跑 `tick`，用于 NAS/Docker。
- 浏览器扩展通过 native messaging 启动 `mdm-nmhost` 子进程，再经 IPC 交给主进程。
- 崩溃监控是独立进程，生成 out-of-process minidump。

### 4.3 数据流

```
用户/浏览器/CLI/手机/AI
        │
        ▼
   Command（ipc::Command）
        │
        ▼
   App 状态机（列表、队列、设置、选择）
        │
        ├──► engine::Download（probe → planner → workers → state）
        │         └── events（Progress / Done / Failed）
        ├──► torrent（librqbit）
        ├──► tools（yt-dlp / gallery-dl / ffmpeg）
        ├──► debrid（换直链）
        └──► extract（完成后解压）
```

---

## 5. 下载引擎：最核心的逻辑

### 5.1 一次下载的完整生命周期

```
1. 规范化 URL / 校验 scheme
2. probe()
   ├─ GET Range: bytes=0-0
   ├─ 206 → size（Content-Range）、resumable=true、ETag、Last-Modified
   ├─ 200 → size（Content-Length）、resumable=false（服务器忽略 Range）
   ├─ 401/403/404/429 → 明确错误
   └─ Content-Disposition / URL path → 文件名
3. 选择保存路径、分类、文件名去重
4. 创建 <name>.mdm.part（稀疏文件，预分配到最终大小）
5. Planner：分段表
6. N 个 Worker：每个一条 HTTP/1.1 连接，Range 请求
7. 写入：WriteAt(offset)，不拼接临时块
8. 进度：每 400ms 左右发事件；分段表原子保存
9. 全部完成 → 校验大小 → 可选 SHA-256 → rename 到最终文件
10. 可选：按服务器 Last-Modified 设置文件时间
```

关键设计：**没有临时目录、没有最后拼接**。每个连接直接写最终文件的对应偏移，因此大文件完成时几乎是瞬时的。

### 5.2 分段规划（Planner）

- 纯函数、可单测、随机模拟测试。
- 初始一段；当有空闲连接时，把**剩余最大的段一分为二**。
- 段小于阈值（约 1 MiB）不再拆分，避免请求开销。
- 慢连接会被丢弃，它负责的段重新进入待分配队列。
- 这样连接不会“早早干完自己的那份然后闲着”，而是持续参与最长的剩余部分。

### 5.3 连接与服务器学习

- 默认 8 连接，可调到 32；允许解锁到 128。
- **HostLimits**：记录每个服务器实际接受的连接数。
- 服务器返回 429/503 时自动降低该服务器的连接数，并在界面标记 `(auto)`。
- 用户可为单站点设置例外连接数。
- 服务器只允许 2 条连接时，MDM 就用 2 条，不会硬开 480 条。

### 5.4 断点续传

- 分段表保存到 `<name>.mdm.part.json`，原子写。
- 保存内容：URL、总大小、ETag、Last-Modified、每段 `start/end/done`。
- 恢复时校验：
  - URL 相同；
  - 大小相同；
  - ETag / Last-Modified 未变（可配置忽略修改时间）；
  - 每段范围合法。
- 校验失败则从头开始，绝不把两个版本的文件拼在一起。
- 崩溃、重启、断网后都能续传。

### 5.5 限速

- 每下载一个令牌桶；全局一个令牌桶。
- 支持“只在我用电脑时限速”（空闲检测）。
- 种子另有上传限速，且单种子限速叠加在全局上传限速之上。
- 最小可设 16 KB/s。

### 5.6 校验与文件属性

- 支持 SHA-256 等校验（下载完成后读文件计算）。
- 可选把文件创建/修改时间设为服务器 `Last-Modified`。
- 完成后可打开文件、打开文件夹、播放完成音。

### 5.7 协议与站点适配

- HTTP/HTTPS：主路径。
- FTP/FTPS：复用同一套 planner/worker/state；分段 = `REST` + `RETR`，`SIZE`/`MDTM` 代替响应头；控制连接池化。
- Google Drive 等“链接不是文件地址”的站点：`sites/` 模块把页面链接转换成真实文件地址，处理确认页、错误页、拒绝原因。
- 重定向、等待页 refresh、HTML 中间页自动跟随。

### 5.8 网络能力

- 代理：无 / 系统 / PAC 脚本 / 手动 HTTP/SOCKS5；可从浏览器请求继承代理。
- 网络适配器绑定：把每个连接绑定到指定网卡（VPN），断线即停，形成 kill switch。
- 加密 DNS：DoH / DoT / 明文 TCP；Cloudflare/Google/Quad9/AdGuard/NextDNS/自定义；失败可回退系统 DNS（默认不回退，避免泄漏）。
- 服务器地理位置：本地 IP→国家表 + 延迟分档（近/中/远），不把 IP 发到远端。
- 本地/局域网地址、hosts、`.local` 等保留给系统 DNS。
- 视频/图库工具通过本地中继代理绑定到同一网卡。

### 5.9 错误模型

- 区分“网络错误 / 服务器拒绝 / 需要登录 / 文件不存在 / 忙 / 校验失败 / 磁盘错误”。
- 面向用户的错误文案直接说明下一步，而不是抛技术异常。
- 每段独立重试，超过次数才整体失败。

---

## 6. 功能全景

下面按用户能感知的产品面拆分。✅ 表示已实现，⚠️ 表示有限制。

### 6.1 核心下载

- ✅ 多连接分段下载、动态再平衡。
- ✅ 断点续传（崩溃/重启/断网）。
- ✅ HTTP/HTTPS、FTP/FTPS。
- ✅ 重定向、等待页、Google Drive 等站点适配。
- ✅ 文件名解析（Content-Disposition、RFC 5987、URL）。
- ✅ 文件大小、服务器是否支持续传的预检。
- ✅ 下载完成后校验大小、可选 SHA-256。
- ✅ 按服务器时间设置文件日期。
- ✅ 重复链接处理：询问 / 编号重命名 / 覆盖 / 复用已完成文件或续传。
- ✅ 批量编号下载（`file01..file99`）。
- ✅ 从文本文件、IDM `.ef2` 导出文件导入链接。
- ✅ 拖放链接/文件到窗口。
- ✅ 剪贴板监听：复制链接后弹出下载确认。

### 6.2 浏览器接管

- ✅ Chromium MV3 扩展（Chrome、Edge、Brave、Opera、Vivaldi）+ Firefox 扩展。
- ✅ 扩展 → Native Messaging → `mdm-nmhost` → IPC → 主程序。
- ✅ 按文件类型、站点排除、最小文件大小决定是否接管。
- ✅ 只有主程序**接受后**才取消浏览器下载；否则浏览器继续。
- ✅ 主程序未运行时，下载还给浏览器。
- ✅ 右键“用 MDM 下载”、选中链接批量下载。
- ✅ 页面媒体面板：HLS/DASH 流、页面已加载的视频/音频。
- ✅ 视频上的悬浮下载按钮：可拖动、按站点记忆位置、可关闭。
- ✅ 浏览器代理/VPN 扩展的代理信息随下载传递。
- ✅ 快捷键强制接管/阻止接管。

### 6.3 视频下载

- ✅ yt-dlp 集成：页面解析、格式选择、播放列表/频道。
- ✅ ffmpeg 集成：合并音视频、转 MP3/Opus/FLAC、字幕封装。
- ✅ 清晰度：最佳 / 指定高度 / 仅音频（原始/MP3/OPUS/FLAC）。
- ✅ 字幕：下载、选择语言、嵌入视频。
- ✅ 登录站点：从浏览器读 Cookie（Firefox 最稳；Chrome 系在 Windows 上锁 Cookie）。
- ✅ 扩展传递页面 Cookie。
- ✅ 首次使用可一键安装 yt-dlp/ffmpeg，并校验签名/校验和。
- ✅ 通过本地中继代理绑定 VPN 网卡；不支持代理的协议（RTMP/RTSP/MMS/FTP）直接拒绝。

### 6.4 图库下载

- ✅ gallery-dl 集成。
- ✅ 按站点规则抓取图片集合。
- ✅ 与视频相同的代理/网卡约束。

### 6.5 BitTorrent

- ✅ magnet 链接、`.torrent` 文件、`.torrent` URL。
- ✅ 文件树选择、跳过不需要的文件。
- ✅ 顺序下载（边下边看）、首尾块优先。
- ✅ 做种、分享率上限、分享时长上限、全局上传限速。
- ✅ 文件校验、重新宣告、追加 tracker、复制 magnet。
- ✅ DHT、UPnP、入站连接、端口、SOCKS5、IP 黑名单。
- ✅ RSS/Atom 订阅 + 自动下载规则（包含/排除、正则、集数、保存目录、是否立即开始）。
- ✅ 种子安全：拒绝危险文件名、不访问本地/局域网 tracker、不覆盖非自己创建的文件。
- ✅ 通过 debrid 服务下载种子（服务端做种，客户端走 HTTP）。

### 6.6 队列与调度

- ✅ 两个内置队列：主下载队列、同步队列（完成后保留）。
- ✅ 自建队列；每队列并行数。
- ✅ 启动/停止队列；失败重试次数；失败移到队尾。
- ✅ 定时：启动时间、停止时间、一次性日期、每周几。
- ✅ 开机启动队列。
- ✅ 唤醒计算机执行队列（Windows 电源计划 / Linux wakealarm / macOS pmset）。
- ✅ 完成后：打开文件、挂断拨号、退出程序、关机/睡眠/休眠（30 秒倒计时）。
- ✅ 流量上限：每 N 小时最多下载 M MB；到量暂停，周期结束自动恢复。

### 6.7 下载列表与分类

- ✅ 列：文件名、大小、进度、速度、状态、剩余时间、描述、来源、队列、添加时间、完成时间、服务器国家等。
- ✅ 排序、列宽、列顺序、显示/隐藏列、重置列。
- ✅ 筛选：全部/未完成/已完成/分类/队列/状态/来源。
- ✅ 分类：General、Compressed、Documents、Music、Programs、Video、Torrents；可增删改、改文件类型与目录。
- ✅ 搜索（文件名、URL、描述）。
- ✅ 多选（Ctrl/Shift/拖框）、右键菜单、属性、校验和。
- ✅ 双击打开进度窗口；Alt+Enter 属性。
- ✅ 删除：仅列表 / 同时删文件；可关闭确认。
- ✅ 拖放、剪贴板、批量导入。

### 6.8 进度窗口

- ✅ 状态、大小、已下载、速率、剩余时间、是否可续传。
- ✅ 服务器国家/延迟分档、CDN 提示、代理提示。
- ✅ 分段可视化（每个连接的份额）。
- ✅ 暂停/继续/取消/隐藏到托盘。
- ✅ 速度限制（单下载）。
- ✅ 完成后选项（打开文件/文件夹、完成对话框）。
- ✅ 种子的五个标签页：文件、Peers、Trackers、Pieces、General。
- ✅ 视频/图库的专用进度窗口。

### 6.9 网络与隐私

- ✅ 代理、PAC、SOCKS5、认证。
- ✅ 网卡绑定 + kill switch。
- ✅ 加密 DNS、DNS 测试、失败策略。
- ✅ 服务器地理位置本地查表。
- ✅ 站点登录（每站点用户名/密码，系统钥匙串）。
- ✅ 代理密码、debrid 密码进钥匙串。
- ✅ 便携模式：所有数据在程序旁，不写注册表。
- ✅ 无广告、无账号、无遥测（仅网站访问计数）。

### 6.10 远程控制与服务器

- ✅ 手机/局域网 HTTPS 远程控制。
- ✅ 配对码 + 设备 token；自签名证书；只监听私有地址。
- ✅ 页面：添加下载/magnet、列表、暂停/继续/删除、启动/停止队列。
- ✅ 已配对设备管理、撤销。
- ✅ `--headless` + Docker + Unraid，跑在 NAS 上。
- ✅ 服务器模式不支持的功能明确列出。

### 6.11 自动化

- ✅ 应用命令行：兼容 IDM 的开关（`/d /s /n /p /f /q ...`）。
- ✅ 独立 CLI：`get`、`resume`、视频、种子、JSON 输出。
- ✅ 本地自动化 API：JSON，token 保护，列出/添加/暂停/开始/删除。
- ✅ MCP Server：Claude、Cursor、VS Code 等通过 MCP 操作下载。
- ✅ 退出码、脚本友好输出。

### 6.12 Debrid

- ✅ Real-Debrid、AllDebrid、Premiumize（实验性）。
- ✅ 代码/浏览器登录、API token。
- ✅ 文件托管链接换直链。
- ✅ 种子/磁力交给服务端做种，客户端 HTTP 下载。
- ✅ 账号顺序、按链接选择账号、密码进钥匙串。

### 6.13 解压

- ✅ 下载完成自动解压（可关）。
- ✅ 分卷压缩包识别（`.part1.rar`、`.7z.001`）。
- ✅ zip/tar/gz/xz 进程内；7z/RAR 调 7-Zip。
- ✅ 解压后删除压缩包到回收站。
- ✅ 解压目录策略：按压缩包名建文件夹 / 总是建 / 直接放。
- ✅ 密码（仅内存，不落盘）。

### 6.14 链接抓取（Link Grabber）

- ✅ 浏览器右键/选中链接批量抓取。
- ✅ 文本粘贴、文本文件、IDM `.ef2` 导入。
- ✅ 自动分组成“包”：分卷、同页链接、其他。
- ✅ 预检：在线/404/需要登录/忙。
- ✅ 每包：名称、保存目录、密码、完成后解压、删除压缩包。
- ✅ 批量加入队列。

### 6.15 外观、语言、无障碍

- ✅ 浅色/深色/跟随系统。
- ✅ 主题：Classic、Native、High contrast、Metro、Adwaita、Breeze、Luna、Aero、Aqua；支持 TOML/JSON 自定义主题、继承、图标集。
- ✅ 从系统强调色派生 Native 主题；OKLab 混色；WCAG 对比度约束。
- ✅ 界面缩放 25%–200%（真正按缩放重绘，不是位图放大）。
- ✅ 86 种语言；阿拉伯语/希伯来语/波斯语/乌尔都语/维吾尔语 RTL。
- ✅ 数字/日期按区域格式；可自定义日期顺序、12/24 小时、周首日。
- ✅ 键盘全操作、屏幕阅读器（Windows Narrator/NVDA；Linux Orca 尚不支持）。
- ✅ 系统文件图标（Windows Shell / macOS NSWorkspace / Linux icon theme）。
- ✅ 完成/失败/队列开始/停止音效，可自定义 WAV。
- ✅ 托盘、最小化到托盘、关闭到托盘。
- ✅ 便携版。
- ✅ 崩溃报告、更新检查（Sparkle 风格、增量更新）。

### 6.16 网站与文档

- ✅ 静态网站，21 种语言，RTL。
- ✅ 帮助页与程序内 Help 共用文档。
- ✅ 真实截图、基准页、FAQ、下载页。
- ✅ 更新日志、贡献者名单。
- ✅ 每页可翻译；脚本内文案通过 `window.MDM_I18N`。

---

## 7. 界面与交互特点

### 7.1 布局

- 经典三栏：菜单栏 + 工具栏 + 左侧分类树 + 主下载列表 + 底部状态栏。
- 工具栏：Add URL、Add Torrent、Start/Stop Queue、Pause、Remove、Options、Scheduler、Speed Limiter。
- 分类树：All Downloads / Unfinished / Finished / 分类 / 队列。
- 列表：多列、可排序、可筛选、可搜索、右键菜单。
- 进度窗口：信息区 + 分段图 + 速度限制 + 完成选项。
- 12 页 Options：General、File types、Save to、Downloads、Connection、Proxy/Socks、Sites Logins、Accounts、Sounds、Regional、Archives、Remote。

### 7.2 视觉

- 默认 Classic 主题刻意接近 Windows 经典/IDM 观感。
- 深色模式跟随系统。
- 主题不是“换几个颜色”，而是：
  - 调色板角色（`button.hot`、`list.row`、`caption`…）；
  - 从少量种子色在 OKLab 中派生整套颜色；
  - 保证文本 4.5:1、边缘 3:1；
  - 控件形状（文本框、复选框、按钮、进度条、滚动条、焦点框、标题栏）；
  - 图标集（SVG/PNG，16/24/32/48，symbolic 跟随文字色）。
- 字体来自系统：Windows Fonts、macOS 系统字体、Linux fontconfig。

### 7.3 交互细节

- 首次启动：语言选择（旗帜 + 本地名称）。
- 下载中：进度窗口实时显示每个连接的份额。
- 完成：完成对话框 + 托盘通知 + 可选音效。
- 删除：确认 + “同时删除文件” + “不再询问”。
- 重复链接：四种策略。
- 队列：右键“立即开始/停止/计划/编辑”。
- 调度器：时间部分可点击、滚轮/方向键调整。
- 键盘：全快捷键表；对话框 Enter/Esc；列表多选。
- 拖放：链接、文件、种子。
- 托盘：显示窗口、暂停/恢复、退出。
- 便携：数据在程序旁。

---

## 8. 数据、配置与安全

### 8.1 数据目录

| 平台 | 路径 |
|---|---|
| Windows | `%APPDATA%\misterdownloadmanager\` |
| Linux | `~/.local/share/misterdownloadmanager` |
| macOS | `~/Library/Application Support/misterdownloadmanager` |
| 便携 | 程序旁 `data/` |
| 覆盖 | `MDM_DATA_DIR` |

- `downloads.json`：下载列表 + 设置，原子写。
- `rss.json`：RSS 源与规则。
- `themes/`：自定义主题。
- 浏览器 host 读取同一份捕获规则，设置改动对下一次捕获立即生效。

### 8.2 密钥

- debrid 账号、站点登录、代理密码 → 系统钥匙串：
  - Windows Credential Manager；
  - Linux Secret Service（GNOME Keyring、KeePassXC 等）；
  - macOS Keychain。
- 设置里只存 `keychain:<service>/<scope>/<key>` 引用。
- 钥匙串不可用时才回退到设置文件（Windows 上再按账户加密）。
- 便携版不写外部。

### 8.3 单实例与 IPC

- 带 token 的本地 socket。
- 第二次启动把参数交给第一次启动。
- 浏览器 host 与主程序共享捕获规则。

### 8.4 远程控制安全

- 默认关闭，关闭时不监听。
- 只监听私有地址，逐地址绑定。
- HTTPS + 本机自签证书，指纹显示给用户。
- 配对码 + 设备 token；除配对外的每个请求都要 token。
- 校验 `Host`、`Origin`；网页无法调用。
- 只能操作下载，不能读文件系统。

### 8.5 种子安全

- 拒绝系统无法安全写入的文件名。
- 不访问本机/局域网 tracker。
- 不覆盖非自己创建的文件。
- 支持 IP 黑名单、代理、DHT/UPnP 开关。
- 首次使用才启动种子引擎；不用种子不监听。

### 8.6 崩溃与更新

- 独立监控进程，out-of-process minidump。
- 符号化堆栈、报告文本、发布日志。
- 更新检查、增量更新、更新窗口。

---

## 9. 质量工程

- `cargo test`：单元测试 + 对 `mdm-labserver` 的端到端下载测试。
- labserver 可注入：慢连接、断连、拒绝、无 Range、文件变化。
- 基准：7 个场景，IDM 6.42 对比，交替运行，Mann-Whitney p<0.001，CPU 也更低。
- 截图流水线：通过 `/show <window>` 打开测试实例，按可访问性树裁剪标注，输出无损 WebP；CI 检查列表/页面/文件一致。
- i18n 流水线：从源码提取英文、机器翻译、人工审校保护、CI 防回退。
- 贡献者名单从 GitHub API 生成。
- Windows/Linux VM 实验室脚本、像素对比、UI 矩形探测。

---

## 10. 明确的局限（官方承认）

- macOS 构建为实验性。
- Linux 屏幕阅读器（Orca）尚不能读窗口。
- Flatpak 浏览器无法连到主程序。
- Wayland 下无法真正隐藏窗口到托盘。
- FTPS（隐式 TLS）尚不能绑定网卡，因此选择网卡时不允许启动。
- 某些 VPN 重连后 macOS 接口名变化，需要重新选择。
- 主题/图标集不覆盖所有自定义控件。
- 网站只有 21 种语言，程序 86 种。

---

## 11. 值得借鉴的工程决策

1. **引擎与 UI 彻底解耦**：引擎只发事件，UI 只读事件。这让 CLI、headless、远程、MCP 复用同一套逻辑。
2. **纯函数 Planner**：分段策略可单测、可随机模拟，不依赖网络。
3. **没有临时目录**：直接写最终文件的偏移，完成即 rename，极大减少完成时的磁盘开销。
4. **原子状态**：分段表、下载列表都原子写；崩溃最多丢 2 秒进度。
5. **服务器学习**：不硬编码连接数，按服务器反馈降级。
6. **错误面向用户**：每个失败都告诉用户下一步。
7. **主题是系统，不是皮肤**：角色、派生、对比度、控件形状、图标集分层。
8. **i18n 从第一天就是一等公民**：TSV 目录、RTL、区域格式、审校保护。
9. **可复现的基准**：自带测试服务器和脚本，速度声明可验证。
10. **安全默认值**：远程默认关闭、只监听私有地址、钥匙串、种子不碰本地 tracker。

---

## 12. 对 downmygo 的启示

### 12.1 必须保留的“用户预期”

- 三栏布局：分类树 + 下载列表 + 详情/进度。
- 多连接加速 + 断点续传 + 浏览器接管。
- 队列 + 定时 + 限速。
- 完成通知、托盘、深色模式。
- 免费、无广告、无账号。

### 12.2 应该重新设计的部分

| MDM | downmygo 的选择 |
|---|---|
| Rust + egui 自绘 | Go + MyGo 原生 UI（GPU 绘制、单二进制、跨平台） |
| 12 页 Options 平铺 | 分组设置 + 搜索，减少认知负担 |
| IDM 复刻感 | 保留工作流，视觉上做现代、克制、品牌化 |
| 86 种语言一次性铺开 | 先 en/zh-CN，i18n 架构就位，按需扩展 |
| 浏览器扩展 + native messaging | 第一阶段做本地 API + 深链，扩展第二阶段 |
| 种子/视频/图库全内置 | 核心下载先做深，扩展能力按插件化路线推进 |
| GPL 代码 | 独立实现，MIT 许可，不复制任何 MDM 代码/资产 |

### 12.3 风险

- **范围过大**：MDM 13 万行 Rust，downmygo 不能一次复刻。必须按“核心下载 → 队列/调度 → 浏览器 → 种子/视频 → 远程/AI”分阶段。
- **跨平台原生 UI**：MyGo 的 `ui` 包在 macOS/Windows/Linux 上行为需逐平台验证（字体、托盘、文件对话框、通知）。
- **浏览器接管**：扩展 + native messaging 的安装/签名/商店审核是长周期工作。
- **法律与品牌**：不得使用 IDM 的名称、图标、文案；downmygo 必须是自己的一套视觉与文案。
- **GPL 污染**：不能复制 MDM 代码；只能学习其公开行为与架构思想。

---

## 13. 功能清单速查（按优先级）

| 优先级 | 功能 | downmygo 阶段 |
|---|---|---|
| P0 | HTTP(S) 多连接、续传、暂停/恢复、删除 | v1.0 已实现 |
| P0 | 下载列表、分类、搜索、排序、详情 | v1.0 已实现 |
| P0 | 队列、并行数、调度、限速、流量上限 | v1.0 已实现 |
| P0 | 设置、主题、托盘、通知、剪贴板 | v1.0 已实现 |
| P0 | CLI + 本地 JSON API + 远程页面 | v1.0 已实现 |
| P1 | 浏览器扩展 + native messaging | v1.1 |
| P1 | 站点登录/Cookie/Referer/UA | v1.1 |
| P1 | 校验和、文件日期、重复策略 | v1.1（部分已实现） |
| P2 | BitTorrent / magnet / RSS | v1.2 |
| P2 | yt-dlp / ffmpeg 视频 | v1.2 |
| P2 | gallery-dl 图库 | v1.2 |
| P2 | 解压 / 分卷 | v1.2 |
| P3 | debrid | v1.3 |
| P3 | VPN 网卡绑定 / 加密 DNS | v1.3 |
| P3 | MCP / AI 助手 | v1.3 |
| P3 | 完整 i18n / RTL / 无障碍 | 持续 |

---

## 14. 结论

MDM 的真正价值不在“下载器”这三个字，而在于它把**一个成熟桌面工具的完整产品面**做齐了：引擎、协议、浏览器、队列、调度、网络、远程、自动化、主题、语言、无障碍、崩溃、更新、文档、基准。

downmygo 的机会不是“再做一个 MDM”，而是：

> **用 Go + MyGo 的原生 UI 和单二进制交付，做一个更轻、更快启动、视觉更现代、默认更安全的下载管理器；先把核心下载与队列做到无懈可击，再用插件化路线扩展种子、视频、浏览器与 AI。**

这份分析是 downmygo 的产品底稿；具体重构方案见 `02-downmygo-product.md` 与 `03-architecture.md`。
