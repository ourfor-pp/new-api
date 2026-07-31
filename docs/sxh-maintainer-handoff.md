# SXH new-api 维护交接

## 文档定位

本文记录 SXH Fork 在上游 `v1.0.0-rc.22` 基础上的定制能力、维护边界和当前状态。长期编码规则放在根目录 `AGENTS.md`，可执行流程放在 `.agents/skills/`，客户端调用说明放在 `docs/volc-speech-client.md`。

本文不保存主机地址、密钥、Token、数据库内容或渠道凭据。线上状态会变化，生产事实必须在操作前重新核实。

## 仓库关系

- 官方上游：`QuantumNous/new-api`，本地 remote 为 `upstream`，只读。
- SXH Fork：`ourfor-pp/new-api`，本地 remote 为 `origin`。
- 上游基线：标签 `v1.0.0-rc.22`，提交 `bc14c18f`。
- 当前定制稳定线：`sxh/rc22`。
- 上游同步、分支和 PR 流程：使用 `$manage-new-api-fork`。
- 生产诊断、发布和回滚：使用 `$operate-new-api-production`。

## 已完成能力

### 1. 火山语音接入

- 接入 Seed-TTS 2.0 V3 HTTP Chunked，公开业务模型 `sxh-tts`。
- 接入录音文件极速 ASR，公开业务模型 `sxh-asr`。
- 支持新版单段 APP Key 与旧版 `appid|access_token`。
- 增加渠道默认 TTS 音色；标准 OpenAI 音色映射到默认音色，火山 speaker ID 原样传递。
- TTS 支持 MP3、Opus、PCM；ASR 支持 WAV、MP3、OGG/Opus。

### 2. 模型映射与定价

- 业务模型通过渠道映射到 `doubao-seed-tts-2.0`、`doubao-seed-asr-flash`。
- 适配器读取映射后的上游模型，不写死 `sxh-*`。
- TTS 按火山 `usage.text_words` 结算，缺失时回退 Unicode 字符数。
- ASR 按实际音频时长结算，每分钟换算 1000 个内部计费单位。
- 定价配置在业务模型上，未配置价格不得免费调用。

### 3. 可靠性修订

- ASR 大文件使用可回放 `BodyStorage` 与流式 Base64 请求体，避免把磁盘缓存整体读回内存。
- 火山语音在首字节前可按限定错误切换备用渠道。
- TTS 输出部分音频后禁止重试，避免拼接不同渠道结果，并退回预扣额度。
- 渠道测试使用真实合法音频，日志不输出音频二进制。
- TTS 兼容火山 V3 JSON 分块响应。

### 4. 字幕和时间轴

- ASR `verbose_json` 支持 segment、word；支持 SRT、VTT。
- TTS SSE 支持音频 delta/done、JSON 字幕时间轴、SRT、VTT。
- 保留火山字词、标点、数字、空格和时间区间，不做 forced alignment 或纠偏。
- 增加字幕计数、粒度、格式和 usage 来源审计，不记录正文或音频。

## 提交与 PR 线

| 变更 | 结果 |
| --- | --- |
| PR #1 Seed-TTS 2.0 与极速 ASR | 已合并 |
| PR #2 语音可靠性修订 | 已合并，合并提交 `581025a1` |
| 故障切换、JSON 分块、业务映射 | 已进入 `sxh/rc22` |
| PR #3 字幕时间轴 | 已合并，提交 `a1e0e626` |

精确提交以 `git log v1.0.0-rc.22..sxh/rc22` 为准。

## 当前版本状态

- 源码 `VERSION`：`1.0.0-rc.22-sxh.5`。
- `.4` 是已被后续 code review 修订取代的字幕时间轴候选，不得复用或部署。
- `.5` 是包含 multipart、SSE 客户端和 OpenAPI 修订的新候选线，不代表已经生成镜像或部署。
- 2026-07-31 的只读生产快照仍为 `1.0.0-rc.22-sxh.2-c20a7794`；操作前必须重新核实。
- 2026-07-31 首次生产只读快照中的渠道 #17 未配置 `volc_speech.default_tts_speaker`；提交前维护者已确认生产默认音色完成配置，上线前仍需只读核对具体值和生效状态。
- 当前工作区包含最近 code review 的修订：multipart 单次解析、SSE 客户端完成事件校验和完整结果缓冲、OpenAPI 文本/SRT/VTT 响应类型，以及候选版本推进。提交状态以 `git status` 为准。

## 尚未完成

- 主动渠道健康探测、自动摘除、恢复探测和防抖闭环尚未实现；现有 priority 不是健康管理器。
- SQLite 到 PostgreSQL、Redis 和蓝绿部署只完成评估，未迁移。
- 不支持实时 WebSocket ASR、异步长文件 ASR、双向或长文本 TTS、声音复刻和阿里语音。
- `.5` 在修订合并后需要生成唯一标签的候选镜像并验证；旧 `.4` 候选镜像不得直接视为最终产物。

## 验证基线

```bash
go test -timeout 10m ./...
go test -race ./relay/channel/volcengine ./relay/helper -run 'Volc|Mapped'
git diff --check
```

涉及前端时执行当前仓库的 Bun 类型检查和生产构建；涉及发布时继续构建唯一标签 Docker 镜像。真实语音联调使用生产数据库副本、隔离端口和最小样本，不修改生产渠道。

生产数据库副本可以作为仓库外的不可变验证基线复用，每次联调从基线复制一次性工作库。复用前比较不暴露敏感值的结构与相关配置指纹；数据库迁移、渠道凭据、模型映射、定价、权限、生产版本或关键环境变量变化时刷新。

当前单实例 SQLite 发布采用“全部预准备、监控在途请求、等待连续空窗、最后检查后快速切换”的最小停机策略。访问日志空闲不能替代长连接、流式请求和上游在途检查；没有安全 drain 时这是数秒级短暂停机优化，不是严格零停机，也不得临时让两个写实例共用同一 SQLite。

2026-07-31 已使用生产一致性只读基线和一次性工作库验证当前 `.5` 工作区镜像：TTS SSE 最终事件、字幕、ASR JSON/text/SRT/VTT、混合 multipart 时间戳字段、业务模型映射、实际结算和日志隐私均通过。该结果用于工作区预验证；已合并提交构建的唯一标签镜像仍需重复相同验收。
