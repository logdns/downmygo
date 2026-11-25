# 开发与发布

## 从源码运行

安装 Git 和 Go（以 `go.mod` 顶部的 `go` 版本为准；v1.6.0 需要 1.27.4）。macOS 的本地开发与竞态测试需要 Xcode Command Line Tools。Windows 安装包需要 NSIS；CI 在 Ubuntu 安装 `nsis` 交叉构建。扩展打包需要 `sh`、`make` 和 `zip`。

```sh
git clone https://github.com/logdns/downmygo.git
cd downmygo
go mod download
go mod verify
go tool mygo generate
go run . version
go run .
```

MyGo 与打包 CLI 使用同一模块版本。yt-dlp 和 gallery-dl 是按需安装的运行时工具，不是 Go 构建依赖；已有 PATH 安装请通过原包管理器更新。macOS/Linux 的图库自动安装需要 `python3 -m pip` 可用；Windows 官方 gallery-dl 可执行文件来自 Codeberg。

## 升级依赖

- 查 [Go 稳定版](https://go.dev/dl/)，升级工具链后运行 `go get -u ./...`；也检查 `go tool mygo` 的构建依赖，最后 `go mod tidy`。提交 `go.mod`、`go.sum`，不要提交工具缓存。
- **v1.6.0 的兼容约束：** `github.com/anacrolix/stm` 保持 v0.5.0。v0.6.0 移除了 `Settish.Iter`，而 DHT v2.24.0 仍调用它；若全量升级遇到该编译错误，执行 `go get github.com/anacrolix/stm@v0.5.0`。等 DHT 适配后再移除约束。
- Actions 使用稳定主版本；浏览器扩展按各自 manifest 独立编号，不随桌面软件空涨版本。

## 验证、构建与发布

```sh
go mod verify && go mod tidy -diff
go vet ./...
go test -race ./... -timeout 300s
go run golang.org/x/vuln/cmd/govulncheck@v1.10.0 ./...
go tool mygo generate
git diff --exit-code -- locales/catalog_gen.go
make extensions
go tool mygo build
```

1. 同步 `internal/version/version.go` 与 `mygo.json`，确认 `go run . version` 与包版本相同。需要更新 UI 素材时，依次运行 `make screens store site`。
2. 推送 `main` 后检查 CI；从同一提交打 `vX.Y.Z` 标签。`build.yml` 为 macOS universal、Windows/Linux amd64 和 arm64 构建安装包，并把两个扩展 ZIP 和油猴脚本一并放入草稿。
3. 核对草稿版本、平台、资产数量和 SHA-256；安装包冒烟通过再发布。自动更新选 macOS DMG、Windows NSIS、Linux tar.gz，不选裸 exe 或 deb；缺少摘要时请走手动安装。
