# downmygo：逻辑、功能与特点

这份文档把程序讲清楚：它怎么跑起来，一次下载从头到尾经过什么，每个模块负责什么，以及它和别的下载器不一样的地方。

- [1. 一句话](#1-一句话)
- [2. 设计原则](#2-设计原则)
- [3. 程序逻辑](#3-程序逻辑)
- [4. 功能清单](#4-功能清单)
- [5. 特点](#5-特点)
- [6. 多语言](#6-多语言)
- [7. 打包与发布](#7-打包与发布)
- [8. 目录结构](#8-目录结构)
- [9. 边界与路线图](#9-边界与路线图)

---

## 1. 一句话

downmygo 是一个用 Go 写的跨平台下载管理器：界面由 MyGo 的 `ui` 包在 GPU 上原生绘制，不启动 WebView；多连接、断点续传、队列、调度、限速、CLI、本地 API、手机远程页都在一个几 MB 的二进制里。

## 2. 设计原则

| 原则 | 具体做法 |
|---|---|
| 原生 | 不嵌浏览器；窗口、菜单、托盘、通知都是系统控件。 |
| 一个二进制 | 没有运行时依赖，没有前端构建产物；`go build` 出来的文件就是应用。 |
| 数据是文件 | 全部状态在数据目录的 `state.json`，原子写入，可直接备份、迁移、手改。 |
| 失败要能继续 | 分段表最多丢 2 秒；崩溃、断网、重启后从原位置续传。 |
| 不打扰 | 默认不弹窗、不推荐、不捆绑；通知、托盘、剪贴板监听都可关。 |
| 说人话 | 三种语言、系统主题、跟随系统语言；错误信息说清楚发生了什么。 |

## 3. 程序逻辑

### 3.1 启动

```
main.go
  ├─ 参数是 get/URL？ → internal/cli 跑完退出（同一个二进制）
  ├─ store.Open(数据目录) → state.json（损坏时备份并重建）
  ├─ app.New(state) → 恢复列表；上次没下完的标记为「已暂停」
  ├─ ApplyLanguage() → 按设置或系统语言显示
  ├─ mygo.App.WhenReady
  │    ├─ NewWindow（尺寸、位置记忆）
  │    ├─ 菜单、托盘（模板图标，随系统明暗自动反色）
  │    ├─ 远程控制开着 → StartAPI
  │    ├─ 剪贴板监听
  │    └─ 命令行/深链带来的链接 → 加入列表
  └─ mygo.App.Run() → 主线程循环：输入 → View() → 绘制
```

主线程之外只有三类 goroutine：下载引擎（每个连接一个）、事件循环（把引擎事件送回主线程）、定时器（1 秒一跳：速度、调度、限速窗口、自动保存）。

### 3.2 一次下载

```
AddForm ──probe──▶ 预检：大小、文件名、Range、ETag、Last-Modified
   │
   ├─ 服务器支持 Range → 分段计划（默认 8 段）→ 每段一个连接
   └─ 不支持 → 单流回退，界面标注「单连接」
        │
        ▼
   写入 <文件>.dmpart ──每 2 秒──▶ <文件>.dmpart.json（分段表，原子替换）
        │
        ├─ 某段先完成 → 把剩余最大的段一分为二，连接不闲着
        ├─ 出错 → 按 MaxRetries 重试该段，退避等待
        ├─ 用户暂停 → 关闭连接，保留分段表
        └─ 全部完成 → 校验（可选 SHA-256）→ 改名到位 → 设置文件时间（可选）→ 通知
```

### 3.3 状态机

```
Queued ──▶ Connecting ──▶ Downloading ──▶ Complete
   │            │              │
   │            └──────────────┴──▶ Error（可 Resume）
   ├──▶ Waiting（队列未启动 / 达到流量限额）
   └──▶ Paused（用户暂停 / 重启恢复）
```

`Waiting` 与 `Paused` 的区别：前者是程序在等条件（队列、限额），条件满足会自动继续；后者是用户让停的，只有用户能让它继续。

### 3.4 队列与调度

- 每个下载属于一个队列（默认主队列）；队列有自己的并行数。
- 队列可以设置开始/停止时间、一次性日期、开机启动、完成后睡眠/休眠/关机/退出。
- 调度器每秒检查一次，到点启动或停止；跨天的时间段（23:00–07:00）也成立。

### 3.5 限速

两层令牌桶：全局一个、每个下载一个，取较小者。另有「每 N 小时最多 M MB」的流量窗口，超了就进入 `Waiting`，窗口滚动后自动放行。

### 3.6 本地 API 与远程页

- 只监听回环地址；打开「允许手机控制」后监听所有网卡。
- Bearer token 保护；`/` 页面只在带 token 打开时把 token 注入页面，避免同网段设备直接读到。
- 接口：`health`、`downloads`、`add`、`pause`、`resume`、`remove`、`queues`、`queue`、`options`。
- 状态同时给出机器可读的 `status` 和本地化的 `statusText`。

### 3.7 深链与单实例

`downmygo://add?url=...` 或 `downmygo://https://...` 交给已运行的实例；第二个实例把参数递过去后立即退出。

## 4. 功能清单

### 下载

- 多连接（1–32，默认 8），剩余最大段动态二分
- 断点续传，分段表原子保存
- 服务器预检：大小、文件名、Range、ETag、Last-Modified
- 不支持 Range 时自动单流，并在界面标注
- 可选 SHA-256 校验；可选按服务器时间设置文件时间
- 重试次数、Referer、Cookie、User-Agent、自定义保存目录与文件名
- 同名文件自动改名，不覆盖不是它创建的文件

### 列表与整理

- 名称、大小、进度、速度、状态、时间六列，可排序、可搜索、可筛选
- 全部 / 未完成 / 已完成 / 分类 / 队列
- 右键菜单：打开、在文件夹中显示、复制链接、加入队列、移除、删除文件
- 详情卡：进度、分段可视化、大小、已下载、速度、剩余时间、连接数、状态
- 分类：常规、压缩包、文档、音乐、视频、程序、种子；可增删改，按扩展名自动归类

### 队列与调度

- 主队列、同步队列、自建队列
- 每队列并行数、开始/停止时间、一次性日期
- 开机启动队列、完成后睡眠/休眠/关机/退出、为队列唤醒电脑

### 网络

- 全局与单下载限速
- 每 N 小时 M MB 的流量上限
- 代理（system / socks5 / http）、网卡绑定地址（VPN 断网保护）

### 系统集成

- 托盘（模板图标，随明暗反色）、通知、单实例、深链、窗口位置记忆
- 关闭/最小化到托盘
- 剪贴板监听：复制链接后提示下载

### 命令行

```sh
downmygo get https://example.com/file.zip -o ~/Downloads -c 8
downmygo get https://example.com/file.zip --checksum sha256:<hex>
downmygo https://example.com/file.zip
```

进度写 stderr，最终路径写 stdout；退出码 0 成功、1 失败、2 用法错误。命令行保持英文，便于脚本与日志检索。

### 本地 API 与远程页

见 [3.6](#36-本地-api-与远程页)。手机浏览器打开 `http://<电脑IP>:8463` 即可添加、暂停、继续、删除。

### 浏览器

- 扩展（`browser/`）：右键链接/视频/图片 → 交给 downmygo；弹窗发送当前页；可接管浏览器下载
- Native Messaging：`downmygo native-host`，4 字节长度 + JSON；`downmygo install-host <ID>` 写各浏览器清单
- 站点登录：按域名后缀匹配的 Cookie 与 Referer，扩展也会传浏览器的 Cookie
- 捕获规则：按域名、扩展名、大小、排除词过滤

### 订阅与抓取

- RSS / Atom 订阅：定时检查，按扩展名或 enclosure 取件，记住已取条目
- 链接抓取：读页面、按类型过滤、勾选加入
- 导入：文本每行一个链接，或 JSON 列表

### 外部下载器与解压

- yt-dlp：视频站点交给它；没装时自动下载官方二进制，进度显示在列表里
- gallery-dl：图库站点交给它；没装时自动安装（Windows 用官方 exe，其他平台用 PyPI wheel + 系统 python3）
- 解压：zip / tar / tar.gz / tar.bz2，完成后自动解压（可选删除压缩包），拒绝路径穿越
- 种子 / magnet：内置 BitTorrent 客户端（DHT、tracker、peer），`magnet:`、`.torrent` 文件与链接都能直接下载，可暂停/继续/删除文件

### 网络与生态

- 加密 DNS：DoH（RFC 8484）与 DoT，任意端点
- VPN 网卡绑定：本地地址绑定，断隧道即断流量
- debrid：Real-Debrid / AllDebrid 换直链，失败回退原链接
- MCP：`downmygo mcp`，7 个工具
- 主题文件：JSON 覆盖 19 个颜色角色与圆角/间距
- 自动更新：查 GitHub 最新 Release，比较版本，打开发布页

### 外观

- 浅色 / 深色 / 跟随系统
- 强调色可改（十六进制）
- 品牌渐变、空状态、详情卡、状态栏
- 窄窗口自适应：侧边栏收起为筛选菜单，列按宽度取舍，工具栏按钮按宽度收成图标

## 5. 特点

1. **原生而非套壳**：没有 WebView，窗口是 GPU 绘制的，启动快、内存小、系统集成完整。
2. **一个二进制**：`go build` 的结果就是应用；没有 node_modules、没有运行时。
3. **状态即文件**：`state.json` 可读、可备份、可脚本处理；配合本地 API 能接自动化。
4. **分段调度聪明**：不是固定 8 段等分，而是「谁先完成就把最大的段再分一半」，慢服务器和快服务器都能吃满。
5. **三种语言、三种平台**：简体中文、繁体中文、英文；macOS / Windows / Linux 同一份代码。
6. **克制的界面**：没有广告位、没有推荐、没有账号；所有开关都能关。
7. **可验证**：引擎有端到端测试（`httptest` 的 Range 服务器、慢服务器、暂停恢复），界面有无窗口渲染测试，三种语言都有断言。

## 6. 多语言

- 文案在 `locales/*.json`（ARB 格式），`en.json` 是源语言，`zh-Hans.json`、`zh-Hant.json` 是翻译。
- `go tool mygo generate` 生成 `locales/catalog_gen.go`，每个键一个带类型的函数；生成文件要提交。
- 设置 → 通用 → 语言：跟随系统 / English / 简体中文 / 繁體中文，切换立即生效，菜单与托盘一起换。
- 内置分类与队列的名字按语言显示；用户自建的名字保持原样。
- 日期、时间、数字、复数按 CLDR 规则格式化（`i18n.FormatDate`、ICU 复数）。
- 服务器返回码、磁盘错误等技术信息保留英文原文，便于搜索与上报。
- 远程页跟随应用语言；API 同时返回 `status`（机器值）与 `statusText`（本地化）。

## 7. 打包与发布

```sh
go tool mygo build                     # 当前平台，产物在 build/
go tool mygo build -platform windows/amd64,linux/amd64
go run ./tools/makeicon                # 重画图标：Icon Composer 文档 + resources/icon.png
go run ./cmd/preview                   # 重画 docs/screens 的截图（三种语言 + 窄窗口）
```

- 图标源文件是 `resources/downmygo.icon`（Icon Composer 文档）；`tools/makeicon` 画出图层，调用 Icon Composer 的 `ictool` 导出 PNG，再按 macOS 图标网格留边。
- CI：`.github/workflows/ci.yml` 跑格式、vet、测试（含 race）、并检查生成的 catalog 是否最新；随后三平台各构建一次。
- 发布：`.github/workflows/build.yml` 在打 `v*` 标签时构建 macOS（universal + dmg）、Windows（amd64/arm64 + NSIS 安装包）、Linux（amd64/arm64 + deb/tar.gz/PKGBUILD），上传到草稿 Release。

## 8. 目录结构

```
main.go                 入口：CLI 分流、窗口、菜单、托盘、单实例、深链
internal/model          下载/分类/队列/设置的数据结构与默认值
internal/store          数据目录、原子 JSON、损坏修复
internal/engine         预检、分段计划、worker、续传、校验、完成
internal/limiter        令牌桶
internal/app            状态机、视图、队列、调度、通知、API
internal/ui             品牌主题、图标、应用标记
internal/cli            downmygo get
internal/version        版本号
locales                 三种语言的文案与生成的访问器
resources               图标（Icon Composer 文档与 PNG）
cmd/preview             无窗口渲染截图
tools/makeicon          画图标并导出
docs                    分析、产品、架构、功能对照、本文
```

## 9. 边界与路线图

现在只做 HTTP/HTTPS。FTP、种子、视频嗅探、图库、解压、浏览器扩展、加密 DNS、debrid、MCP、自动更新都在路线图上，按 1.1–1.3 推进，不影响 1.0 作为日常下载器使用。

明确不做：账号与云同步、广告与捆绑、破解或绕过付费服务、内置浏览器、移动端原生 App（远程页足够）。
