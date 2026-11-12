# 收藏追更检测

`internal/watch` 是独立于搜索和链接存活检测的目录读取模块。只使用公开分享接口、收藏中的提取码和临时分享令牌，不读取浏览器 Cookie，不登录账号，不保存或下载分享文件。临时令牌只存在于一次扫描的内存中，错误提示不包含令牌或请求 URL。

## 实现范围

| 网盘 | 目录读取方式 |
| --- | --- |
| 阿里 | 分享令牌、`adrive/v3/file/list`，按 marker 翻页 |
| 夸克 / UC | 分享令牌、分享目录；UC 使用 `transfer_share/detail` |
| 百度 | 提取码校验返回临时 Cookie，`share/list` 按页读取目录 |
| 115 | 分享码及提取码，`share/snap` 按 offset 翻页 |
| 123 | 公开分享目录，Web 请求签名、按页读取 |
| 天翼 | 分享信息、访问码校验、`listShareDir`；同时识别单文件分享 |
| 迅雷 / PikPak | 分享信息与临时访问码令牌，`share/detail` 按 page token 翻页 |
| 光鸭 | 分享访问令牌、`get_share_page_files_list`，按数值 cursor 翻页 |
| 移动 | 公开分享请求的 AES-CBC 编码、`getOutLinkInfoV6` 按范围读取 |

这些接口可能受登录、验证码、频率或地域限制；尤其迅雷 / PikPak 不自动获取或续期验证码令牌，服务端要求验证时会停止本轮检测。接口失效或无法辨认返回结构时，显示无法读取，而不宣称目录为空或没有更新。磁力和电驴是固定内容标识，没有可追更的分享目录；其他链接不做任意 URL 抓取。

## 一致性与通知

- 首次完整成功的快照只建立基线。后续比较文件 ID、大小和两次都有的内容哈希；忽略改名和目录时间，不将文件删除视为新增更新。
- 子目录或任何一页失败、循环分页、重复目录、重复文件、响应格式异常、超过数量/时间/体积限制，整次扫描失败。上次成功快照和未读更新保持原样。
- 每次完成后从当前时间安排下一轮，不逐轮补跑停机期间的任务。连续失败指数退避至最多 24 小时。
- 检测结果先写 SQLite，再发布系统通知。同一快照只通知一次；通知失败保留未读标记。没有通知重试队列，因此恰好在落盘后、通知前退出也可能只保留应用内标记。
- 配置更改、取消订阅或删除收藏会取消当前请求。随机代号阻止旧请求写回新配置，删除后重新收藏也不会接收旧请求结果。标为已读单独存储，不被进行中的检测覆盖。
- 本机系统通知由 MyGo 提供，首次发送可能触发系统授权；不使用远程推送服务。程序退出或电脑休眠时不执行任务，也没有自动登录系统后台服务。

## 协议参考与验证

此模块使用标准库独立实现，没有加入第三方网盘 SDK。协议字段和请求形式参考了以下项目公开的实现；未引入它们的驱动代码或依赖：

- [PanSou 分享链接检测](https://github.com/fish2018/pansou/blob/221665fb2c9279f22af4684ea2d8c0c331eac1cc/service/check_service.go)
- [OpenList 阿里分享目录](https://github.com/OpenListTeam/OpenList/tree/main/drivers/aliyundrive_share)、[123 分享目录](https://github.com/OpenListTeam/OpenList/tree/main/drivers/123_share)、[PikPak 分享目录](https://github.com/OpenListTeam/OpenList/tree/main/drivers/pikpak_share)
- [115driver 分享响应字段](https://github.com/SheltonZhu/115driver/blob/main/pkg/driver/response.go)
- [天翼分享目录请求字段](https://github.com/xxcheng123/cloudpan189-interface/blob/master/client/api_list_share_dir.go)
- [Mediary Scout 光鸭接口](https://github.com/fancydirty/mediary-scout/blob/main/packages/workflow/src/guangya-client.ts)
- [AList 移动分享字段](https://github.com/AlistGo/alist/blob/main/drivers/139/types.go)

自动测试通过模拟各网盘 HTTP 返回验证目录递归、访问码传递、长数字 ID、分页失败、重定向拒绝、基线比较、重启持久化、取消订阅及通知时序；界面测试覆盖中英文、明暗主题与最小窗口。模拟测试不等于每种网盘都已完成真实分享的持续追更实测，也不能保证第三方接口未来不变。真实分享是否可追更，以该收藏首次完整读取成功及后续检测状态为准。
