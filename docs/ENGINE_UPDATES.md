# 独立 PanSou 引擎更新

PanReel 与 PanSou 使用两个独立更新通道。应用更新仍使用 MyGo；引擎更新不修改 `.app`、Windows 安装目录或应用版本，只在用户数据目录的 `engines/` 保存已验证的引擎。

## 首次启用（仓库维护者）

1. 将本次代码及 `.github/workflows/engine.yml` 推送到仓库默认分支，并启用 GitHub Actions。计划任务只在默认分支运行。
2. 在仓库 **Settings → Secrets and variables → Actions** 中设置 `MYGO_UPDATER_PRIVATE_KEY`，使用现有的 PanReel Ed25519 私钥。若应用发布已配置这个 Secret，可直接复用。它必须匹配 `mygo.json` 及 `internal/engineupdate/update.go` 中的公钥，不要生成一个不匹配的新密钥，也不要提交私钥文件。
3. 手动运行 **Independent PanSou engine updates** 工作流一次。确认 build、publish 两个作业成功。
4. 仓库会出现 `pansou-engine` Release，包括三种平台的归档及最后发布的 `engine-update.json`。该 Release 标为非 latest，不影响 MyGo 的应用更新通道。
5. 在应用“偏好设置 → 更新”手动检查，或等待下次自动检查。引擎修订与当前版本相同不会重复提示。

当前仓库中的工作流和客户端代码本身不会替你配置 GitHub Secrets 或发布远程 Release。首个包尚未发布时，应用会显示明确的未发布提示。

## 发布流程

- 每小时第 13、43 分钟检查 PanSou 默认分支的完整提交 SHA。GitHub 的实际调度可能延迟；仓库活动策略也可能暂停计划任务。
- 无新提交时不构建。手动触发可重新构建同一提交；客户端以源码修订比较更新，桌面协议改变应同时升级协议/客户端。
- 构建任务不持有签名私钥；固定 Go 工具链为 macOS arm64、macOS amd64、Windows amd64 编译，应用可重复桌面补丁，生成实际注册插件目录并收集许可证。
- 启动真实引擎检查桌面协议、临时令牌、空频道行为和退出回收。补丁不适用或测试失败会停止发布，保留现有通道。
- 发布前执行 `go run ./cmd/licenses -check`。新依赖、许可证或署名文件变化会停止自动发布；维护者核对上游条款后，在本地运行 `go run ./cmd/licenses` 并提交已审核的变更，再重跑工作流。相同依赖和许可声明下的引擎更新仍可自动发布。
- 引擎使用 `buildtools/pansou.mod` / `pansou.sum` 固定已审核的依赖和 Go 1.27.4 工具链。上游新增依赖时需更新清单并重新审核；发布前的 `govulncheck` 检测到可达的已知漏洞也会停止发布，见[构建说明](../buildtools/README.md)。
- 独立 publish 作业只校验归档并签名，不执行上游程序；先上传内容寻址的归档，再替换签名清单。

本地构建命令（已准备全部平台引擎时）：

```sh
go run ./cmd/licenses -check
go run ./cmd/engine-release
# 通过安全方式注入现有 MYGO_UPDATER_PRIVATE_KEY 后：
go run ./cmd/engine-release -sign
```

测试：

```sh
go test ./internal/engineupdate ./internal/model
PANREEL_ENGINE_TEST=1 go test -count=1 ./internal/engine ./internal/app
```

## 客户端过程与恢复

应用启动及运行期间每 30 分钟检查签名清单；不会自动下载或安装。点击“更新 PanSou”后下载受限大小的归档，验证清单签名、归档 SHA-256、目标平台和二进制 SHA-256，拒绝路径越界、符号链接及重复归档成员。

新引擎先在临时数据目录试启动，必须返回对应修订及桌面协议、声明 `search_updates` 能力，并正确载入用户选择的频道。通过后关闭旧进程、启动新进程、保存兼容的设置，最后切换 `engines/active` 指针。任一步失败会恢复旧进程、目录和设置。收藏与历史数据库不会因引擎更新被替换。

各版本放在 `engines/<归档 SHA-256>/`，包含引擎、签名清单及许可证，旧版文件继续保留。每次应用启动重新检查签名和文件哈希，损坏或不能启动的版本回退到内置引擎。默认 TG 频道仍为空，不会因为更新自动加入上游示例频道。全部启用插件的配置会包含新增插件；手动选择的插件、频道和仍受支持的参数保留。

## 搜索结果自动更新

桌面补丁按搜索缓存键跟踪插件异步任务、插件补搜及 TG 补搜。`/api/panreel/search/snapshot` 与其他桌面接口使用同一临时令牌，只读取缓存和本次搜索范围的任务状态，不触发新的抓取；完成状态在读取缓存前采样，确保最后一批写入不会遗漏。插件全部选择、请求扩展参数和频道列表沿用 PanSou 的缓存键规则。

应用发起一次搜索后每秒读取快照，按链接 ID 合并并追加结果；整轮等待受用户设置的搜索超时时间约束。任务完成、手动停止、超时、切换关键词或退出应用后停止读取。旧请求的回调不会覆盖新的搜索。此前不支持进度接口的已安装引擎会回退到随应用内置的兼容版本。

构建时对异步入口做结构校验，上游修改相关机制后补丁不匹配会直接停止构建。`PANREEL_ENGINE_TEST=1` 同时验证受控快慢插件、最终缓存写入、失败任务退出和搜索范围隔离。
