# downmygo · 油猴脚本

一个用户脚本（Tampermonkey / Violentmonkey / Greasemonkey），把当前页面、单个链接或整页的下载链接交给本机的 downmygo。

脚本不依赖浏览器扩展，也不需要注册 Native Messaging：它直接调用 downmygo 的本地 API（`http://127.0.0.1:8463`），通过 `GM_xmlhttpRequest` 绕过页面的跨域限制。

## 安装

1. 安装 [Tampermonkey](https://www.tampermonkey.net/) 或 Violentmonkey。
2. 打开 [`downmygo.user.js`](downmygo.user.js)，油猴会提示安装；或把文件拖进浏览器。
3. 启动 downmygo，打开 设置 → 高级 → **API token**，复制那串令牌。
4. 在任意网页上，从油猴菜单选 **downmygo → Set the API token**，粘贴令牌。只需一次。

## 用法

油猴菜单（或快捷键）：

| 命令 | 快捷键 | 做什么 |
|---|---|---|
| Send this page to downmygo | `Alt+D` | 把当前页面地址发过去 |
| Grab the links of this page | `Alt+Shift+D` | 收集页面上可下载的链接，确认后一起发过去 |
| Set the API token | — | 重新输入令牌 |
| Set the API port | — | 默认 8463，改了端口时用 |

- 抓取按扩展名过滤（zip/rar/7z/视频/音频/文档/安装包/种子…），列表在脚本顶部的 `EXTENSIONS` 里，可以直接改。
- 结果在右下角弹一条提示；失败会说明原因（令牌不对、应用没开…）。
- 应用没开时，脚本会退回 `downmygo://add?url=…` 深链，由系统唤起应用。

## 发布

- **Greasy Fork**：直接粘贴 `downmygo.user.js` 的内容，或填这个仓库的 raw 地址。
- **GitHub**：文件本身就是可安装的，raw 链接即可。
- 图标：`icon-128.png`，Greasy Fork 的脚本图标用得上。

## 与扩展的区别

| | 油猴脚本 | 浏览器扩展 |
|---|---|---|
| 安装 | 装一个用户脚本管理器 | 装扩展 + 注册本机程序 |
| 通信 | 本地 API（需要令牌） | Native Messaging（不需要令牌） |
| Cookie | 拿不到浏览器的 Cookie | 可以带上 Cookie |
| 右键菜单 | 没有，用油猴菜单 / 快捷键 | 有 |
| 接管浏览器下载 | 不能 | 能 |

需要登录才能下载的文件，用扩展；只是想把链接丢给 downmygo，脚本更轻。
