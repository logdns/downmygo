# downmygo：技术架构

> 目标：用 Go + MyGo 原生 UI，交付一个跨平台、单二进制、可脚本化、可扩展的下载管理器。
> 本文描述**已经落地的代码结构**与后续扩展点。

---

## 1. 技术选型

| 层 | 选择 | 理由 |
|---|---|---|
| 语言 | Go 1.27 | 单二进制、跨平台交叉编译、GC 可控、团队熟悉 |
| UI | MyGo `ui` 包 | 原生 GPU 绘制，无 WebView，启动快、内存低；内置表格/列表/侧边栏/弹窗/主题 |
| 窗口/系统 | MyGo `mygo` 包 | 窗口、菜单、托盘、通知、文件对话框、剪贴板、单实例、深链 |
| HTTP | 标准库 `net/http` | 稳定、可控、无第三方依赖 |
| 持久化 | JSON + 原子写 | 可读、可调试、跨版本迁移简单 |
| CLI | 标准库 `flag` | 零依赖 |
| API | 标准库 `net/http` | 本地 token 保护 |
| 测试 | `testing` + `httptest` + MyGo `ui.Tester` | 引擎端到端、UI 无窗口测试 |

**为什么不用 WebView**：下载管理器是工具型应用，界面元素固定、状态更新频繁。原生 UI 启动更快、内存更低、系统集成更自然；MyGo 的 `ui` 包已经提供表格、虚拟列表、侧边栏、弹窗、主题、无障碍，足够覆盖下载器。

---

## 2. 目录结构

```
downmygo/
├── main.go                    入口：CLI 分流、单实例、窗口、菜单、托盘
├── mygo.json                  MyGo 打包配置（含 i18n 目录）
├── locales/                   三种语言的 ARB 文案 + 生成的 catalog_gen.go
├── resources/
│   ├── icon.png               1024×1024 应用图标（Icon Composer 导出）
│   └── downmygo.icon          Icon Composer 文档：填充 + 图层
├── .github/workflows/         CI（测试 + 三平台构建）与发布
├── cmd/
│   └── preview/               无窗口渲染截图，用于文档与视觉回归
├── internal/
│   ├── model/                 下载、分类、队列、设置；纯数据与规则
│   ├── store/                 数据目录、原子 JSON 读写、损坏修复
│   ├── engine/                HTTP 下载引擎
│   │   ├── probe.go           预检：大小、文件名、可续传、ETag
│   │   ├── planner.go         分段规划（纯函数，可单测）
│   │   ├── name.go            文件名解析与安全化
│   │   ├── engine.go          Manager / job / worker / 进度 / 续传
│   │   └── engine_test.go     本地 Range 服务器端到端测试
│   ├── limiter/               令牌桶限速（单下载 + 全局）
│   ├── cli/                   `downmygo get`
│   ├── ui/                    品牌主题、图标
│   └── app/                   应用状态、视图、队列、调度、API
│       ├── app.go             状态机、添加/暂停/恢复/删除、队列、调度
│       ├── view.go            整个窗口的视图
│       ├── api.go             本地 JSON API + 远程页面
│       ├── names.go           分类/队列/语言的名字与切换
│       ├── icon.go            代码画图标（托盘、窗口标记、图层）
│       └── power.go           关机/睡眠/休眠
├── docs/
│   ├── 01-mdm-analysis.md     MDM 全量分析
│   ├── 02-downmygo-product.md 产品定义与设计系统
│   ├── 03-architecture.md     本文
│   ├── 04-feature-parity.md   功能对照
│   └── screens/               渲染截图
└── tools/makeicon/            画图层、写 Icon Composer 文档、导出 PNG
```

依赖方向：

```
main ──► app ──► engine ──► model
          │       └──────► limiter
          ├─────► store ──► model
          ├─────► ui (theme/icons)
          └─────► mygo / mygo/ui

cli ──► engine / model / store
preview ──► app / ui
app / cli / ui ──► locales ──► mygo/i18n
```

`engine` 不知道 UI；`app` 只通过事件观察引擎；`cli` 与 `app` 复用同一个引擎。

---

## 3. 运行时模型

### 3.1 线程

| 线程/协程 | 职责 |
|---|---|
| 主线程 | MyGo 事件循环；构建视图；所有应用状态读写 |
| 引擎 goroutine | 每个下载 1 个 `run` + N 个 worker + 1 个进度 ticker |
| 事件循环 goroutine | 合并引擎事件，120ms 批量交给主线程 |
| 调度 goroutine | 每 5 秒检查队列时间、流量窗口 |
| 剪贴板 goroutine | 每 900ms 读剪贴板 |
| API goroutine | 本地 HTTP 服务；请求 `onMain` 后等待结果 |
| CLI | 独立进程路径，不启动 UI |

**关键约束**：应用状态只在主线程读写。后台 goroutine 通过 `Window.Update` 或 `RunOnMain` 把变更交给主线程，避免锁和竞态。

### 3.2 事件合并

引擎每 400ms 发一次进度；如果每个事件都重建视图，10 个下载就是 25 次/秒。`eventLoop` 按下载 ID 合并，每 120ms 批量应用一次：

```go
pending := map[string]engine.Event{}
select {
case ev := <-a.events: pending[ev.ID] = ev
case <-ticker.C:
    batch := pending; pending = map[string]engine.Event{}
    win.Update(func() { for _, ev := range batch { a.applyEvent(ev) } })
}
```

效果：视图最多 ~8 次/秒重建，进度条平滑，CPU 低。

---

## 4. 下载引擎

### 4.1 数据结构

```go
type Download struct {
    ID, URL, FinalURL, FileName, Dir string
    Size, Done int64
    Status Status
    Resumable bool
    ETag, LastModified string
    Referer, Cookies, UserAgent, Checksum string
    Connections int
    Segments []Segment   // 运行时
    Speed float64        // 运行时
}

type Segment struct {
    Start, End, Done int64   // End 包含
}
```

### 4.2 预检

`Probe` 发 `GET Range: bytes=0-0`：

- `206` → 解析 `Content-Range` 得到总大小，`resumable=true`，记录 ETag/Last-Modified。
- `200` → 服务器忽略 Range，单流下载，`resumable=false`。
- `401/403/404/429` → 明确错误。
- 文件名：`Content-Disposition`（含 RFC 5987）→ URL path → `download`。
- 请求头固定 `Accept-Encoding: identity`，避免压缩导致字节偏移错位。

### 4.3 分段规划

`InitialPlan(size, n)` 先切成 n 段；`ClaimInto` 在运行中动态再平衡：

```
while 有未完成段:
    选剩余最大的未占用段
    if 剩余 > 1MiB 且 活跃连接 < maxConn:
        从当前进度处一分为二
        返回前半段
    else:
        占用该段
```

性质：

- 连接不会“干完自己那份就闲着”。
- 小段不再拆分，避免请求开销。
- 慢连接释放的段会回到池子，由其他连接接手。
- 纯函数，可用随机模拟测试。

### 4.4 Worker

每个 worker 循环：

1. `ClaimInto` 取一段。
2. 发 `Range: bytes=start+done-end`。
3. 64KB 缓冲读；`limiter.WaitN`；`file.WriteAt(buf, offset)`。
4. 更新段进度与总进度。
5. 完成/失败后释放段；失败按退避重试。

服务器返回 `429/503` 时把 `maxConn` 减半（最低 1），下次再试。

### 4.5 续传

- 分段表写入 `<file>.dmpart.json`，原子写，每 2 秒或暂停时保存。
- 恢复校验：URL、大小、ETag、Last-Modified、每段范围。
- 不匹配则从头开始，绝不拼接两个版本。
- 下载文件是 `<file>.dmpart` 稀疏文件，预分配到最终大小；完成时 `rename`。

### 4.6 完成

1. `file.Sync()` + `Close()`。
2. 可选 SHA-256 校验（流式读文件）。
3. 删除目标同名文件（仅当用户选择覆盖/自动编号后）。
4. `rename(part, final)`。
5. 可选按 `Last-Modified` 设置文件时间。
6. 删除状态文件。
7. 发 `Complete` 事件。

### 4.7 单流回退

服务器不支持 Range 或大小未知时：

- 删除旧 part，`O_TRUNC` 重开。
- 单连接顺序写。
- 暂停后保留 part，但恢复时从头开始（因为无法知道已下载部分的边界）。
- UI 显示“one stream / 不可续传”。

### 4.8 限速

`limiter.Limiter` 是令牌桶：

- `rate=0` 不限速。
- 桶容量 `max(rate/4, 16KiB)`。
- 大于桶的读取按桶大小分批等待，避免“大 buffer 永远等不到足够令牌”的死锁。
- 每下载一个 limiter，全局一个 limiter；`Group.WaitN` 依次等待两者。

### 4.9 错误分类

| 类型 | 处理 |
|---|---|
| DNS/连接/超时 | 可重试，友好文案 |
| 429/503 | 降连接数，可重试 |
| 5xx | 可重试 |
| 4xx（除 429） | 不重试，直接失败 |
| 磁盘写失败 | 不重试，保留 part |
| 校验失败 | 不重试，保留 part 供用户检查 |
| 用户暂停 | 保存状态，状态 Paused |

---

## 5. 应用状态机

### 5.1 状态

```go
type App struct {
    st    *store.State       // 设置、分类、队列、下载
    store *store.Store
    eng   *engine.Manager
    win   *mygo.Window

    filter, query string
    selected int
    sortCol int; sortDesc bool
    addOpen bool; add AddForm
    optionsOpen bool; optionsTab int
    confirm *Confirm

    queueRunning map[string]bool
    lastDone map[string]int64
    limitUsed int64; limitSince time.Time
}
```

### 5.2 下载状态

```
Queued ──► Connecting ──► Downloading ──► Complete
   │            │              │
   │            └──► Error ◄───┘
   ├──► Waiting（队列/限流/流量上限）
   └──► Paused ◄──► Downloading
```

- `Queued`：等待启动。
- `Waiting`：在队列里等并行位，或等待流量窗口。
- `Paused`：用户暂停，或启动时把上次运行中的状态改为暂停。
- `Error`：可重试；用户点 Resume 重新入队。

### 5.3 添加流程

```
OpenAdd(url)
  → 填默认值（目录、分类、连接数、UA）
  → probeAdd()（后台）
      → 成功：填文件名、大小、可续传、分类、重复标记
      → 失败：显示原因，仍可提交
  → commitAdd()
      → 校验 URL
      → 决定文件名/目录/分类
      → 重复策略
      → 文件已存在 → 自动编号
      → 追加到列表
      → StartNow ? start() / pump() : Waiting
      → Save()
```

### 5.4 队列

- `queueRunning` 记录每个队列是否在跑。
- `pump()`：对每个运行中的队列，统计活跃数，按列表顺序启动 `Queued/Waiting` 直到并行数。
- 完成/失败事件后再次 `pump()`。
- 停止队列：暂停该队列所有活跃/排队下载。
- 删除队列：下载移到“无队列”。

### 5.5 调度

每 5 秒：

- 流量窗口：超过 `LimitHours` 则重置 `LimitUsed`，恢复等待中的下载。
- 每个队列：
  - `OnStartup` 且启动 10 秒内 → 启动。
  - `StartAt` 与当前 `HH:MM` 匹配且星期匹配/一次性日期匹配 → 启动。
  - `StopAt` 匹配 → 停止。
  - 队列内全部完成/失败 → 停止并执行完成后动作。
- 保存脏状态。

### 5.6 流量上限

- 每个完成事件把 `Done` 增量累加到 `LimitUsed`。
- 达到 `LimitBytes` → 暂停所有活跃/排队，状态 `Waiting`，错误文案“已达到本时段流量上限”。
- 窗口结束 → 重置并 `pump()`。

---

## 6. 持久化

### 6.1 数据目录

| 平台 | 路径 |
|---|---|
| Windows | `%APPDATA%\downmygo` |
| macOS | `~/Library/Application Support/downmygo` |
| Linux | `$XDG_DATA_HOME/downmygo` 或 `~/.local/share/downmygo` |
| 便携 | 程序旁 `data/`（存在 `portable` 文件） |
| 覆盖 | `DOWNMYGO_DATA_DIR` |

### 6.2 state.json

```json
{
  "version": 1,
  "settings": { "...": "..." },
  "categories": [ { "id": "video", "name": "Video", "folder": "...", "types": ["mp4"] } ],
  "queues": [ { "id": "main", "name": "Main download queue", "parallel": 4 } ],
  "downloads": [ { "id": "...", "url": "...", "fileName": "...", "status": 4 } ],
  "limitUsed": 0,
  "limitSince": "..."
}
```

- 原子写：临时文件 → `fsync` → `rename` → 目录 `fsync`。
- 损坏：保留 `state.json.broken`，用默认值启动。
- 修复：缺失字段补默认；运行中状态改为暂停；分类目录为空时补全。

### 6.3 续传文件

```
<final>.dmpart        正在写的稀疏文件
<final>.dmpart.json   分段表
```

完成后两者都删除。

---

## 7. UI 架构

### 7.1 视图

`App.View(c *ui.Context)` 每帧从状态构建界面：

```
Column
├── Toolbar（品牌、Add URL、播放/暂停/删除/文件夹、Tasks、搜索、设置）
├── Split（侧边栏 236 / 主区）
│   ├── Sidebar（Downloads / Categories / Queues）
│   └── Column
│       ├── Header（标题、数量、Sort、Details）
│       ├── Table（Name/Size/Progress/Speed/Status/Added）
│       └── SplitVertical（表格 / 详情卡）
├── StatusBar（速度、活动数、限速、远程、版本）
└── Dialogs（Add / Options / Confirm / About）
```

- 表格使用 `ui.Table` + `ListState`，只构建可见行。
- 选择、排序、右键菜单由 `ListState` 提供。
- 详情卡使用 `ui.SplitVertical`，高度记忆。
- 弹窗使用 `ui.Modal`，自动管理焦点/Esc/点击外部关闭。
- Toast 使用 `c.Toast`。

### 7.2 主题

`internal/ui.Theme(dark, accent)`：

- 从 MyGo 的 `LightTheme`/`DarkTheme` 出发。
- 覆盖品牌色、表面色、边框、文字、状态色。
- 圆角 10、间距 4、焦点环 2、滚动条 8。
- 强调色支持用户自定义 `#RRGGBB`。

### 7.3 图标

`internal/ui/icons.go`：

- 内嵌 SVG 字符串，`ui.MustParseSVG` 解析一次。
- 线性图标 1.9 描边、24 网格、`currentColor`。
- `ui.Mark` 是应用标记（下载箭头 + 进度条），与图标同一套几何。

应用图标走 Icon Composer：

```
internal/app/icon.go    SDF 画几何：圆角方、箭头、进度条；4× 超采样
        │
        ├─ GlyphPNG(1024) ──► resources/downmygo.icon/Assets/glyph.png
        ├─ TilePNG(1024)  ──► resources/downmygo.icon/Assets/tile.png
        └─ TrayPNG(32)    ──► 菜单栏模板图标（黑色 + 透明，系统自动反色）
                │
                ▼
tools/makeicon 写 icon.json（品牌渐变填充 + 标记图层）
                │
                ▼
ictool（Icon Composer 自带）导出 1024 PNG
                │
                ▼
按 macOS 图标网格缩到 824 并居中 ──► resources/icon.png ──► mygo build 生成 .icns/.ico/.png
```

非 macOS 上没有 Icon Composer 时，`tools/makeicon` 直接用 `IconPNG(1024)` 画同一张图，几何完全一致。

### 7.3.1 语言

- 文案在 `locales/*.json`（ARB + ICU MessageFormat），`en.json` 是源语言，带 `@key` 说明供翻译。
- `go tool mygo generate` 生成 `locales/catalog_gen.go`：每个键一个带类型的函数，复数和数字日期按 CLDR。
- 视图每帧重建，所以 `locales.X()` 直接跟随当前语言；菜单、托盘、窗口标题这类只设置一次的东西在 `mygo.App.OnLocaleChanged` 里重建。
- 内置分类/队列在 `state.json` 里存英文名（它们同时是文件夹名与稳定标识），显示时经 `categoryName`/`queueName` 翻译；用户自建的名字原样显示。
- 远程页面在 `StartAPI` 时按当前语言渲染一次（主线程），每个请求只替换 token，避免在 HTTP goroutine 里等主线程。
- 技术性错误（服务器状态码、磁盘错误）保留英文原文。

### 7.4 快捷键

| 键 | 动作 |
|---|---|
| Cmd/Ctrl+N | 添加 URL |
| Cmd/Ctrl+V | 粘贴链接并打开添加框 |
| Cmd/Ctrl+F | 聚焦搜索 |
| Cmd/Ctrl+, | 设置 |
| Cmd/Ctrl+Shift+O | 设置 |
| Space | 暂停/继续选中 |
| Delete | 删除选中 |
| Enter | 打开/展开选中 |
| Cmd/Ctrl+A | 选中第一行（列表自身支持全选） |

### 7.5 无窗口渲染

`cmd/preview` 用 `ui.Render(view, w, h, scale)` 生成 PNG：

```sh
go run ./cmd/preview docs/screens
```

用于文档截图、视觉回归、设计评审，不需要打开窗口。

---

## 8. 本地 API 与远程页面

### 8.1 启动

- 默认只监听 `127.0.0.1:8463`。
- 设置里打开“允许局域网设备控制”后监听 `0.0.0.0`。
- token 自动生成，保存在 `state.json`。
- 所有 `/api/*` 需要 `Authorization: Bearer <token>` 或 `?token=`。

### 8.2 端点

| 方法 | 路径 | 作用 |
|---|---|---|
| GET | `/api/health` | 健康检查 |
| GET | `/api/downloads` | 列表 + 总速度 |
| POST | `/api/add` | `{url, dir, name, queue, start, referer, cookies}` |
| POST | `/api/pause?id=` | 暂停 |
| POST | `/api/resume?id=` | 继续 |
| POST | `/api/remove?id=&delete=1` | 删除 |
| GET | `/api/queues` | 队列列表 + 运行状态 |
| POST | `/api/queue?id=&action=start\|stop` | 启停队列 |
| GET | `/api/options` | 下载目录、连接数、限速 |
| GET | `/` | 手机远程页面（注入 token） |

### 8.3 线程

HTTP handler 在 goroutine 中运行；所有状态操作通过 `onMain`：

```go
func (a *App) onMain(fn func()) {
    done := make(chan struct{})
    a.win.Update(func() { defer close(done); fn() })
    <-done
}
```

### 8.4 远程页面

- 单文件 HTML/CSS/JS，无外部依赖。
- 自动读取注入的 token。
- 2 秒轮询列表。
- 添加、暂停、继续、删除。
- 响应式，适配手机。
- 只在本机/局域网，不暴露公网。

---

## 9. CLI

```
downmygo get URL [flags]
  -o, --out DIR
  -n, --name NAME
  -c, --connections N
  -q, --quiet
      --checksum sha256:<hex>
      --referer URL
      --cookie VALUE
```

- URL 可以放在 flags 前或后。
- 进度写 stderr，最终路径写 stdout。
- 退出码 0/1/2。
- 复用 `engine.Manager`，与 GUI 行为一致。

---

## 10. 测试

| 层 | 内容 |
|---|---|
| `engine` | 预检、分段、文件名、限速、端到端下载、无 Range、暂停/恢复、校验失败 |
| `limiter` | 零限速、速率约束、取消 |
| `store` | 默认值、原子写、损坏恢复、运行中状态修复 |
| `model` | 百分比、ETA、分类匹配、排序 |
| `app` | 视图文本、筛选/搜索、添加弹窗、选择/详情、分类与重命名 |
| `ui` | 无窗口渲染截图（`cmd/preview`） |

运行：

```sh
go test ./...
go vet ./...
go run ./cmd/preview
```

端到端测试使用 `httptest` 自建 Range 服务器，不依赖外网。

---

## 11. 构建与发布

### 11.1 开发

```sh
go run .
# 或
go tool mygo dev
```

### 11.2 打包

```sh
go tool mygo build
```

MyGo 读取 `mygo.json`：

- `name: downmygo`
- `identifier: app.downmygo.desktop`
- `version: 1.0.0`
- `icon: resources/icon.png`
- `out: build`
- `urlSchemes: ["downmygo"]`

产物：

| 平台 | 产物 |
|---|---|
| macOS | `.app`、`.dmg`（可签名/公证） |
| Windows | `.exe` 安装器、便携 zip |
| Linux | `.deb`、`.rpm`、AppImage、tar.gz |

### 11.3 交叉编译

MyGo 纯 Go、无 cgo，可在任意平台构建其他平台：

```sh
GOOS=windows GOARCH=amd64 go build ./...
GOOS=linux   GOARCH=arm64 go build ./...
GOOS=darwin  GOARCH=arm64 go build ./...
```

---

## 12. 扩展点

### 12.1 新协议

在 `engine` 增加 `Fetcher` 接口：

```go
type Fetcher interface {
    Probe(ctx, url) (*Info, error)
    Open(ctx, seg) (io.ReadCloser, error)
}
```

HTTP、FTP、种子、视频各自实现；`job` 只依赖接口。

### 12.2 新下载类型

`model.Kind` 增加枚举；`app.start` 根据 Kind 选择引擎；UI 用不同图标/详情页。

### 12.3 浏览器扩展

- 扩展 → `downmygo://add?url=...` 深链（v1.0 已支持）。
- v1.1 增加 native messaging host，直接调用本地 API。
- 捕获规则放在 `internal/ipc`（待建），与主程序共享。

### 12.4 插件

- 后续可定义 `Plugin` 接口：`Name()`、`Commands()`、`Download()`。
- 插件以独立进程运行，通过本地 API 通信，避免崩溃影响主程序。

---

## 13. 已知限制（v1.0）

- 仅 HTTP/HTTPS；FTP、种子、视频、图库未实现。
- 浏览器扩展未实现；目前用剪贴板 + 深链 + 本地 API。
- 无站点登录 UI；可通过添加框的 Referer/Cookie 手动传。
- 无加密 DNS、无网卡绑定 UI（设置字段已预留，绑定逻辑在引擎中）。
- 无解压、无 debrid、无 MCP。
- 仅英文界面；i18n 架构未接入。
- 主题仅浅色/深色 + 强调色；无主题文件。
- 远程页面无配对码；token 直接注入页面（仅本机/局域网）。
- 无自动更新、无崩溃上报。

这些都在路线图中，不影响 v1.0 作为“核心下载器”的完整性。

---

## 14. 关键代码索引

| 文件 | 作用 |
|---|---|
| `main.go` | 入口、单实例、窗口、菜单、托盘、深链 |
| `internal/engine/engine.go` | 下载 job、worker、进度、续传、完成 |
| `internal/engine/planner.go` | 分段规划 |
| `internal/engine/probe.go` | 预检与文件名 |
| `internal/limiter/limiter.go` | 令牌桶 |
| `internal/app/app.go` | 状态机、队列、调度、通知、剪贴板 |
| `internal/app/view.go` | 整个窗口 |
| `internal/app/api.go` | 本地 API + 远程页面 |
| `internal/store/store.go` | 数据目录与原子持久化 |
| `internal/ui/theme.go` | 品牌主题 |
| `internal/ui/icons.go` | 图标 |
| `internal/app/icon.go` | 应用/托盘图标生成 |
| `cmd/preview/main.go` | 无窗口截图 |
