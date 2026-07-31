# 仓库关系与分支约定

## 远端

- `origin`：`https://github.com/ourfor-pp/new-api.git`，SXH Fork，可写。
- `upstream`：`https://github.com/QuantumNous/new-api.git`，官方上游，只读。
- 不修改或移除上游项目名称、作者归属、许可证和品牌信息。
- 本地应为 `upstream` 配置不可用的 push URL，降低误推风险；fetch URL 保持官方地址。

## 当前稳定线

- 上游基线标签：`v1.0.0-rc.22`，提交 `bc14c18f`。
- SXH 稳定分支：`sxh/rc22`。
- 客户端业务模型由渠道映射到上游模型；适配器不得依赖 `sxh-*` 业务名称。

## 已合并的 SXH 主线

- PR #1：接入 Seed-TTS 2.0 与极速 ASR。
- PR #2：修正语音渠道可靠性、multipart 大文件和部分流重试。
- 后续稳定线提交：故障切换与资源保护、TTS V3 JSON 分块、业务模型映射。
- PR #3：补齐 ASR/TTS 字幕时间轴，合并提交 `a1e0e626`。

精确状态始终以 `git log v1.0.0-rc.22..sxh/rc22` 和 GitHub PR 为准，不把本文当作 Git 的替代品。

## 上游升级策略

- 小型独立修复：评估后 cherry-pick 或按当前代码重写。
- 社区未合并 PR：只提取协议和测试思路，默认重写。
- 新 RC 或正式版：从新上游标签建立新的升级线，再重放 SXH 能力；不要强制改写已发布稳定线。
- 合并冲突优先保护计费、数据库兼容、请求重放和部分流语义，不以“能编译”为完成标准。
