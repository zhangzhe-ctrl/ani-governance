# tx7do 源码备份

本目录保存应用当前依赖图选定的 **49 个 tx7do Go module**，以及开发工具
`github.com/tx7do/go-wind-toolkit/gowind@v1.0.3` 和它的 tx7do 依赖源码。
两份依赖图合并去重后共 **53 个 module@version**（应用 49 项，工具补充 4 项）。
同一仓库下的子 module 按各自版本保存，不使用仓库最新分支代替。

- `proxy/`：原始 `.zip` 源码、`.mod`、`.info` 和版本列表，可用作 file-GOPROXY。
- `manifest.json`：每个 module 的版本、来源、Go 校验值、文件 SHA-256 和许可文件位置。
- `locks/`：此次备份对应的 应用与 gow 的 `go.mod`、`go.sum`，以及 `buf.lock`。
- `buf/`：两个锁定 BSR 模块的原始 Proto 源码、commit/digest 和独立校验脚本。
- `verification.json`：本次目录迁移和备份验证摘要。
- `SHA256SUMS`：本目录交付文件校验表。

源码 ZIP 中的版权和许可证完整保留；仓库派生代码的声明见根目录
[THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md)。多数 module 很小，
主要体积来自 `go-utils/geoip` 的数据文件。未修改应用的 Go import、module path 或增加 replace。

## 校验和阅读

在执行验证的主机上：

```bash
cd third_party/tx7do
sha256sum -c SHA256SUMS
unzip -l proxy/github.com/tx7do/go-crud/entgo/@v/v0.0.55.zip
```

解压任一 ZIP 即可阅读对应源码；ZIP 的根目录是完整的 `module@version`。
`manifest.json` 中每项的 `license_files` 给出了原始声明在归档中的路径。

## 使用备份代理

从仓库根目录设置代理，例如：

```bash
export GOWORK=off
export GOPROXY="file://$PWD/third_party/tx7do/proxy,https://proxy.golang.org"
go mod download github.com/tx7do/go-crud/entgo@v0.0.55
```

这是 tx7do 源码备份，不是全项目离线包。其他作者的依赖、Go 工具链、
其余生成工具和固定 Model/Network API 交付仍需单独提供；有私有 file-GOPROXY
交付时，将它加入代理链。应用自身的 `go.mod`、`go.sum` 保持原样。

备份时已在 Fedora 使用空 `GOMODCACHE`、只有本目录的 `file://` 代理且无网络回退，
恢复全部 53 个 module@version 并逐项比较 Go `Sum` / `GoModSum`。恢复测试关闭外部 SumDB，
比较对象是采集时通过原项目 `go.sum` 核对的值；gow 及其独立依赖图是本次明确固定的工具快照。
这证明这些归档可恢复，不代表整个项目已经实现离线构建或全部代码再生成。

## 更新快照

在编译机使用任务私有缓存，先审阅依赖变化，再写入一个新目录：

```bash
export GOWORK=off
export GOMODCACHE="$RUN/cache/mod"
export GOCACHE="$RUN/cache/build"
python3 scripts/backup-tx7do.py \
  --source-commit "$(git rev-parse HEAD)" \
  --gow-version v1.0.3 \
  --output third_party/tx7do-next
```

脚本拒绝覆盖现有快照，自动核对应用锁定值及空缓存恢复；它只处理 Go modules。
依赖升级后需同步快照、工具版本和校验表。不要把备份目录直接改成 vendor 或
对全部模块添加本地 replace；要自行维护某个依赖时，再为该模块建立独立 fork。

Buf 两个原始模块的 B5 摘要均已与 `api/buf.lock` 一致性核验。BSR 导出未附许可证，
已另存相关固定 Go 模块的声明及来源；这两个 BSR commit 的确切许可归属仍为
`not_verified`，详见 [Buf 备份说明](buf/README.md)。
