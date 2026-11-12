# PanReel

[English](README.md) · **简体中文**

使用 Go 和 [MyGo](https://github.com/egoist/mygo) 开发的原生网盘搜索应用，支持 macOS 和 Windows。安装包内置 [PanSou](https://github.com/fish2018/pansou) 搜索引擎，安装后即可启动，无需 Docker、独立服务或开发环境。

## 功能

- 内置 75 个搜索插件，默认全部开启；TG 公开频道由用户自行配置。
- 支持百度、阿里、夸克、光鸭、天翼、UC、移动、115、PikPak、迅雷、123 云盘，以及磁力、电驴和其他链接，共 14 类，可勾选搜索范围。
- 搜索结果自动补充，支持去重、筛选、提取码和链接检测。默认展示「仅有效结果」；不支持检测或状态未确认的链接可切换「全部结果」查看。
- 收藏、搜索历史，以及可选的收藏追更提醒。
- 中英文、明暗主题、设置自动保存、自动读取系统代理。
- PanSou 可独立更新：应用运行时每 30 分钟检查，用户确认后安装；需维护者先发布[引擎更新通道](docs/ENGINE_UPDATES.md)。

应用聚合公开分享，不提供网盘账号登录、转存、下载或播放。部分搜索来源需要自行配置凭据，第三方服务的可用性不作保证。

## 安装

从 [Releases](https://github.com/cuikaipeng/panreel/releases) 下载安装包。

- **macOS 13+**，支持 Apple Silicon / Intel：打开 `panreel-0.1.0.dmg`，将 **PanReel.app** 拖入 **Applications（应用程序）**。
- **Windows 10/11 x64**：运行 `panreel-setup-0.1.0-amd64.exe`，保留随应用安装的引擎文件。

### macOS 首次启动

当前没有 Apple Developer ID 证书，安装包尚未公证。如果 macOS 因「无法验证开发者」等提示阻止打开，请先确认下载来自本仓库，再将应用复制到「应用程序」，打开「终端」执行：

```sh
xattr -dr com.apple.quarantine "/Applications/PanReel.app"
```

随后重新打开 PanReel。若命令提示权限不足，在 `xattr` 前加 `sudo` 重试；安装在其他位置时请修改路径。命令仅移除该应用的隔离属性。也可按 [Apple 官方说明](https://support.apple.com/zh-cn/102445)，在「系统设置 → 隐私与安全性」选择「仍要打开」。若提示文件损坏或检测到恶意软件，应先检查下载文件，不能一概当作证书问题。

Windows 安装包也暂未配置代码签名证书，系统可能显示「未知发布者」。

## 首次使用：配置 TG 频道

**软件默认不附带 TG 频道。必须先添加并启用至少一个公开频道，才能搜索，包括「仅搜索插件」模式。** 搜索插件仍全部默认开启。

1. 进入 **「搜索来源 → TG 频道」**。
2. 输入公开频道用户名或链接，支持一次添加多个，用英文或中文逗号、空格、换行或分号分隔：

   ```text
   @channel_one, @channel_two, https://t.me/channel_three
   https://t.me/s/channel_four，channel_five
   ```

   请把示例换成真实频道。用户名不区分大小写，重复项自动合并；不支持私密频道、邀请链接或具体消息链接。
3. 点击 **「添加频道」**，频道会自动勾选并保存。等待 **「引擎已就绪」** 后即可搜索。

之后可在同一页面取消勾选或移除频道；全部停用后会再次禁止搜索。公开频道搜索无需登录 Telegram。如果 Telegram 不可达，请检查网络。PanReel 默认跟随系统代理，可在代理软件中开启「系统代理」，也可到「偏好设置 → 搜索引擎 → 网络代理」调整。

## 收藏追更

在收藏夹选中资源，勾选右侧的 **「更新提醒」**，设置每 30 分钟至每天检测一次。首次成功检测建立记录，之后新增文件或已有文件内容变化时，会发送系统通知并显示「有更新」。

检测仅在 **PanReel 运行时**进行，请允许系统通知。已适配 11 类网盘目录，但登录要求、验证码或接口变化可能导致检测失败。具体行为和支持限制见[追更说明](docs/update-watches.md)。

## 开发

需要 Git、Go **1.27.4+**；以下命令使用 pnpm。首次运行会下载固定版本 PanSou 源码及其依赖。

```sh
pnpm dev            # 一个终端：准备引擎并启动应用
pnpm test
pnpm check
pnpm release:check
pnpm build          # 为当前平台生成安装包
```

pnpm 只是可选的命令别名，也可依次运行 `go run ./cmd/prepare` 和 `go tool mygo dev`。测试与架构见[开发说明](CONTRIBUTING.md)，跨平台打包与更新签名见[发布说明](docs/RELEASING.md)。

## 开源协议

PanReel 自有代码采用 [MIT](LICENSE) 协议，PanSou 及其他依赖保留各自的许可与版权声明，详见[第三方声明](THIRD_PARTY_NOTICES.md)。再分发时必须保留随附的 `resources/licenses/` 许可文件。

软件开源许可不包含影视作品或分享文件的授权。请只访问有权使用的内容，并遵守相关服务条款。本仓库不附带影视文件或预设 TG 频道。
