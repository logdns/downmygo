# 路线图状态：每一项都检查过了

这份文档逐项回答「做了没有、做到什么程度、没做的为什么」。代码在 `internal/` 下，测试与实现同目录。

图例：**✅ 已实现** · **◐ 部分** · **○ 未实现（有计划）** · **— 明确不做**

---

## 1. 功能梳理与逻辑检查

| 项 | 状态 | 说明 |
|---|---|---|
| 下载引擎（多连接、续传、校验） | ✅ | 分段动态二分、分段表原子保存、单流回退、SHA-256、90 秒 stall 看门狗 |
| 状态机 | ✅ | Queued → Connecting → Downloading → Complete / Error / Waiting / Paused，条件等待与用户暂停分开 |
| 队列与调度 | ✅ | 并行数、开始/停止时间、一次性日期、开机启动、完成后睡眠/休眠/关机/退出 |
| 限速与流量上限 | ✅ | 单下载 + 全局令牌桶；每 N 小时 M MB 的窗口 |
| 列表、分类、搜索、排序 | ✅ | 六列可排序，分类按扩展名自动归类 |
| 本地 API 与远程页 | ✅ | token 保护、常量时间比较、页面 token 转义、本地化状态 |
| 持久化 | ✅ | 原子写、0600（含 token）、损坏备份、空条目丢弃 |
| 三种语言 | ✅ | 界面、菜单、托盘、远程页、通知、日期与数字 |
| 自适应布局 | ✅ | 侧边栏收起为筛选菜单、列按宽度取舍、按钮收成图标 |
| 审计修复 | ✅ | 见提交记录：权限、令牌比较、XSS、数据竞争、空指针、stall、API 关闭 |

## 2. 浏览器与站点

| 项 | 状态 | 位置 | 说明 |
|---|---|---|---|
| 浏览器扩展 | ✅ | `browser/` | MV3，右键菜单、弹窗、可选接管浏览器下载 |
| Native Messaging | ✅ | `internal/nativehost`、`downmygo native-host` | 4 字节长度 + JSON，Chrome/Edge/Brave/Firefox 清单由 `downmygo install-host` 写入 |
| 站点登录 | ✅ | `state.json` 的 `sites` | 按域名后缀匹配，Cookie 与 Referer 自动带上；扩展也会传浏览器 Cookie |
| 捕获规则 | ✅ | `state.json` 的 `rules` | 按域名、扩展名、大小、排除词过滤；无规则时全收 |
| 导入 | ✅ | `downmygo import FILE`、文件菜单 | 纯文本每行一个链接，或 JSON 数组（`url` / `link` 字段） |

## 3. 媒体与抓取

| 项 | 状态 | 位置 | 说明 |
|---|---|---|---|
| RSS / Atom 订阅 | ✅ | `internal/rss`、订阅对话框 | 定时检查、按扩展名或 enclosure 取件、记住已取过的条目 |
| yt-dlp 视频 | ✅ | `internal/tools` | 外部程序，进度模板解析，作为一条下载显示 |
| gallery-dl 图库 | ✅ | `internal/tools` | 外部程序，按目录差集认领文件 |
| 解压 | ✅ | `internal/extract` | zip / tar / tar.gz / tar.bz2；拒绝路径穿越与符号链接；xz 需外部工具 |
| 链接抓取 | ✅ | `internal/grab`、抓取对话框 | 解析页面 href/src，按扩展名过滤，勾选后加入 |
| 种子 / magnet | ◐ | `addLink` | 交给系统种子客户端（`magnet:` 与 `.torrent`）；**不含内置 BitTorrent 引擎** |
| 视频站内嵌字幕/播放列表 | ○ | — | 交给 yt-dlp 的参数，未做界面 |

## 4. 网络、工具与生态

| 项 | 状态 | 位置 | 说明 |
|---|---|---|---|
| 加密 DNS | ✅ | `internal/netset` | DoH（RFC 8484，Cloudflare/Google/Quad9/AdGuard/Mullvad 或任意端点）与 DoT |
| VPN 网卡绑定 | ✅ | `internal/netset`、设置 | 本地地址绑定，断隧道即断流量 |
| debrid | ✅ | `internal/debrid` | Real-Debrid 与 AllDebrid，自动换直链，失败回退原链接 |
| MCP | ✅ | `internal/mcp`、`downmygo mcp` | stdio JSON-RPC，7 个工具：列表、添加、暂停、继续、删除、队列、选项 |
| 主题文件 | ✅ | `internal/themes` | JSON，覆盖 19 个颜色角色与圆角/间距，可从内置浅色/深色继承 |
| 完整 i18n | ✅ | `locales/` | 界面、菜单、托盘、通知、远程页、API 状态、日期数字复数；CLI 与技术错误保留英文 |
| 自动更新 | ✅ | `internal/update` | 查 GitHub 最新 Release，比较版本，打开发布页；不做静默替换 |

---

## 5. 仍未做的，以及为什么

| 项 | 原因 |
|---|---|
| 内置 BitTorrent 引擎 | 需要完整的 DHT / BEP 协议栈与磁盘调度，是一个独立项目；现在交给系统客户端 |
| 解压 RAR / 7z / xz | 需要外部程序或非标准库依赖；zip/tar/gz/bz2 已覆盖大多数下载 |
| 静默自动更新（下载并替换自身） | 需要签名与回滚；现在只提示并打开发布页 |
| 扩展商店上架 | 需要开发者账号与审核；现在加载已解压的扩展 |

## 6. 怎么验证

```sh
go test ./...                 # 每个新包都有测试
go run ./cmd/preview          # 界面截图，含新增的设置页
go run ./tools/makeicon       # 图标
downmygo grab https://example.com/page
downmygo import links.txt
downmygo mcp                  # 接 MCP 客户端
downmygo native-host          # 由浏览器启动
downmygo install-host <扩展ID>
```
