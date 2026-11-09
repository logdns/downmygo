# 架构

downmygo 是 Go + MyGo 的单二进制应用：界面由 MyGo 在 GPU 上绘制，没有 WebView，没有前端构建产物。

## 1. 目录

```
main.go                 入口：CLI 分流、窗口、菜单、托盘、单实例、深链
locales/                三种语言的 ARB 文案 + 生成的访问器
resources/              图标（Icon Composer 文档 + PNG）
internal/
  model/                下载、分类、队列、设置；纯数据与规则
  store/                数据目录、原子 JSON、损坏修复
  engine/               HTTP 下载：预检、分段、worker、续传、校验
  torrent/              BitTorrent：DHT、tracker、peer、做种、上传限速
  limiter/              令牌桶
  netset/               加密 DNS（DoH/DoT）与绑定地址
  tools/                yt-dlp / gallery-dl：查找、自动安装、运行
  extract/              zip / 7z / rar / tar / gz / bz2 / xz
  grab/                 页面与列表里的链接
  rss/                  RSS 与 Atom
  debrid/               Real-Debrid、AllDebrid
  nativehost/           浏览器桥：native messaging、规则、登录
  themes/               主题文件
  mcp/                  MCP 服务（stdio）
  update/               版本检查、下载、替换自身
  app/                  状态机、视图、队列、调度、通知、API
  ui/                   品牌主题与图标
  cli/                  get / import / grab / info / mcp / native-host
cmd/preview             无窗口渲染截图
tools/makeicon          画图标并导出
browser/                浏览器扩展（MV3）
```

依赖方向：`main → app → {engine, torrent, tools, extract, grab, rss, debrid, nativehost, themes, mcp, update} → {model, store, netset, limiter}`；`cli` 与 `app` 复用同一批包；`ui` 只有主题和图标。

## 2. 线程

- **主线程**：输入、视图、菜单、托盘、所有应用状态。`win.Update(fn)` 把工作送回主线程。
- **引擎 goroutine**：每个下载连接一个；事件经 channel 合并后每 120ms 送回主线程。
- **torrent goroutine**：每个种子一个，每秒报一次进度。
- **工具 goroutine**：每个 yt-dlp / gallery-dl 进程一个。
- **定时器**：每 5 秒一跳（速度、调度、限速窗口、订阅、自动保存）。

跨线程只经 `win.Update`，所以界面数据没有锁。

## 3. HTTP 下载

```
预检（Range: bytes=0-0）→ 分段计划（默认 8 段）
  ├─ 支持 Range → 每段一个 worker，写 <文件>.dmpart
  └─ 不支持     → 单流回退，界面标注「单连接」
每 2 秒写 <文件>.dmpart.json（分段表，原子替换）
某段先完成 → 把剩余最大的段一分为二
完成 → 校验（可选）→ 改名到位 → 设置文件时间（可选）→ 通知
```

- 连接 90 秒没有数据 → 关闭 body，该段重试（stall 看门狗）
- 4xx 不重试，5xx / 429 / 断流重试，退避 0.7s×n，上限 8s
- 令牌桶两层：全局 + 单下载；另有「每 N 小时 M MB」的流量窗口

## 4. 种子

- `anacrolix/torrent` 之上包一层：一个 client，每个种子一个 job
- 入口：`magnet:`、`.torrent` 文件、`.torrent` 链接（先下载再解析）
- 元数据到达前显示 `dn` 或哈希；到达后显示真实名字与大小
- 下载完成 → 若开启做种，继续上传直到分享率或时长到达
- 上传限速：client 级令牌桶；暂停同时停上传与下载
- 坏 magnet（无 infohash、全零）在入口拒绝，不让底层库 panic

## 5. 持久化

`state.json`（0600，含 API 令牌）：

```json
{
  "version": 1,
  "settings": { … },
  "categories": [ … ],
  "queues": [ … ],
  "downloads": [ … ],
  "subscriptions": [ … ],
  "sites": [ … ],
  "rules": [ … ]
}
```

- 原子写：临时文件 + fsync + rename
- 损坏：保留 `state.json.broken`，以默认值启动
- 手改的 `null` 条目在加载时丢弃
- 分段表与 torrent 元数据在数据目录下各自的子目录

## 6. 界面

- 视图每帧重建，所以语言、主题、尺寸变化都是数据变化
- 主题：`ui.Theme` 从 MyGo 的浅色/深色出发，覆盖品牌色；主题文件再覆盖 19 个颜色角色
- 自适应：`< 820` 收起侧边栏（标题行变筛选菜单）、列按宽度取舍、按钮收成图标
- 图标：Icon Composer 文档 + `ictool` 导出，按 macOS 网格留边；托盘用同一几何的单色模板图

## 7. 本地 API 与远程页

- 只监听回环；打开「远程控制」后监听所有网卡
- Bearer token，常量时间比较；页面里的 token 按 JS 字面量转义
- 端点：`health`、`downloads`、`add`、`pause`、`resume`、`remove`、`queues`、`queue`、`options`
- 状态同时给机器值 `status` 与本地化 `statusText`
- 远程页在启动时按当前语言渲染一次，每个请求只替换 token

## 8. 外部程序

- `tools.Manager` 先找 PATH，再找应用目录，最后自己下载：
  - yt-dlp：官方原生二进制
  - gallery-dl：Windows 用官方 exe；其他平台从 PyPI 取 wheel 解包，用系统 python3 运行
- 进度从程序的输出解析（`--progress-template`），文件从 `--print after_move:filepath` 认领
- 每个程序一个互斥量，两个视频同时开始不会写同一个 `.part`

## 9. 更新

```
GET /repos/logdns/downmygo/releases/latest → 比较版本
下载资产 → 校验 assets[].digest（SHA-256）→ 写 <data>/updates/
助手脚本：等进程退出 → 替换 → 重新打开
  macOS：hdiutil 挂载 dmg，拷出 .app
  Windows：静默运行 NSIS 安装包
  Linux：解包 tar.gz，覆盖可执行文件
```

## 10. 构建与发布

```sh
go tool mygo build                 # 当前平台
go tool mygo build -platform windows/amd64,linux/amd64
go run ./tools/makeicon            # 重画图标
go run ./cmd/preview               # 重画截图（三种语言 + 窄窗口）
```

- `ci.yml`：gofmt、vet、`go test -race`、检查生成的 catalog 是否最新，然后三平台构建
- `build.yml`：打 `v*` 标签 → macOS（universal + dmg）、Windows（amd64/arm64 + NSIS）、Linux（amd64/arm64 + deb/tar.gz）→ 草稿 Release
