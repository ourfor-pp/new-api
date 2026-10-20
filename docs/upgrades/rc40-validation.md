## 2026-10-20 正式候选准备

最终候选提交 `c8e1bdbf0`，镜像 `local/new-api:1.0.0-rc.45-sxh.1-c8e1bdbf0`。最终镜像已用当前生产一致性备份完成升级、重复启动、rc.22 应用回滚及最小真实聊天、TTS→ASR、余额和计费验收。原有业务表保持，新增旧凭据退役选项；Manager 当前实际旧凭据读取接口通过，升级后进入 30 天退役期，仍需后续迁移。

生产备份 478031872 字节，完整性 `ok`，SHA-256 `723c74be353b735d2fcfb5743060f855c74d6c98ed0842cc15ece467083423ba`，权限 0600。发布证据目录为服务器 `new-api/releases/rc45-sxh1-20261020/`；本地脱敏证据在 `.tmp/rc45-release/`。

用户已授权择机上线。首轮连续观察 15 分钟未出现 60 秒无业务请求且无连接的静默窗口，未执行切换。已启动独立后台发布任务，最多继续等待 24 小时；切换前重新备份并校验，若流量恢复则继续等待。仅切换 new-api，健康超时或核心验收失败时切回旧镜像，不自动恢复数据库。任务状态记录于远端 `job-state.json`；本记录不表示部署已经完成。未推送 Git 或创建远端标签。

候选版本 `1.0.0-rc.45-sxh.1`，包含已审查的混合图片渠道路由、重试退款、无 Redis 额度更新和订阅退款修订。生产保持 `BATCH_UPDATE_ENABLED` 未设置（关闭）。已知批量写库下追加预扣及无限额度 Token 退款缺陷尚未修复，该模式不得用于本候选。

根模块全量测试、定向 race 和 SQLite 3.50.4 / MySQL 8.0.46 / PostgreSQL 16.15 退款矩阵通过；生产副本升级、重复启动及 rc.22 应用回滚已有演练记录，最终镜像还须复验。生产切换状态以最终部署证据为准。

# rc.45 本地集成验证（2026-10-20）

## 版本与生产边界

生产仍为 `1.0.0-rc.22-sxh.9`，rc.40 / rc.44 均只是本地升级过程，没有部署。下一次生产升级路径应是 **rc.22-sxh.9 → rc.45 候选**，不能按生产已运行 rc.44 来制定迁移或回滚方案。

本轮在 `upgrade/rc40-validation` 的 `5a68778c2` 上合入官方 `v1.0.0-rc.45`（`6cf0d90b5`），版本标识为 `1.0.0-rc.45-sxh.dev`。保留全部 rc.22 `.9` 定制与现有升级适配；Claude 零 usage / 纯缓存计费修复此前已经以等价补丁合入。本轮没有加入 rc.45 之后的通知限流窗口、审计日志索引、Claude 准备/重试保活和 Resend 四个主线提交，没有推送、构建发布镜像、调用付费提供方或操作生产。

唯一文本冲突在 `controller/relay.go`：保留 SXH 的响应已输出后不再追加 JSON 保护，同时使用 rc.45 的上游地址脱敏参数。新增上游脱敏测试依赖 `.invalid` 域名无法解析，但当前网络把该域名解析到代理地址；改为测试级拨号替身模拟 DNS 失败，仍执行真实适配器、代理选择和错误脱敏，保留全部断言，未改变生产网络逻辑。

## 验证结果

环境为 Go 1.26.0、Bun 1.3.11；原始日志和合成数据验证工具位于忽略目录 `.tmp/rc45-validation-evidence/`。

| 检查 | 命令与结果 |
| --- | --- |
| 根模块全量回归 | `GOMAXPROCS=2 go test -p 2 -timeout 10m ./...` 通过 |
| 根模块构建 | `GOMAXPROCS=2 go build -p 2 -o .tmp/rc45-validation-evidence/new-api` 通过 |
| SXH 并发回归 | `GOMAXPROCS=2 go test -p 2 -race ./relay/channel/volcengine ./relay/helper ./plugins -run 'Volc\|Mapped\|SpeechOptions\|Alibaba.*Image\|Alibaba.*Qwen'` 通过 |
| 脱敏测试修订 | `go test -race ./relay/channel/advancedcustom -run TestAdaptorMasksDialedHostsInDialFailure -count=1` 通过 |
| 独立模块 | 分别在 `relaykit/`、`tokenkit/` 内运行 `GOWORK=off GOMAXPROCS=2 go build -p 2 ./...` 与 `go test -p 2 ./...`，全部通过 |
| 前端依赖 | `bun install --frozen-lockfile --force` 通过，锁文件未变；修复本地旧 node_modules 的 jsdom/LRUCache 不匹配 |
| 前端类型与构建 | `bun run typecheck`、`bun run build` 通过 |
| 前端全量测试 | `bun run test --maxWorkers=2`：200 个文件、2370 个用例全部通过（395.09 秒） |
| 前端 lint | 158 errors、66 warnings；与原始 rc.45 使用同一工具比较，文件、规则、消息和级别完全一致，合并新增项为 0；存量错误未修复 |
| 数据库专项 | 配置下述测试 DSN 后运行 `GOMAXPROCS=2 go test -p 1 ./model ./controller -run 'Migration\|Migrate\|DatabaseMatrix' -timeout 10m`，通过，包含渠道优先级数据库矩阵 |

### 真实数据库矩阵

- SQLite **3.50.4**（项目实际驱动）、MySQL **8.0.46**、PostgreSQL **16.15**。
- MySQL / PostgreSQL 使用本机临时容器 `sxh-rc45-mysql` / `sxh-rc45-postgres`，仅绑定 `127.0.0.1:23306` / `127.0.0.1:25432`。数据库专项 DSN 分别为 `root:<本地合成密码>@tcp(127.0.0.1:23306)/rc45_matrix?charset=utf8mb4&parseTime=True&loc=Local` 和 `postgres://postgres:<本地合成密码>@127.0.0.1:25432/rc45_matrix?sslmode=disable`，通过 `TEST_MYSQL_DSN` / `TEST_POSTGRES_DSN` 提供。
- 执行 `python3 .tmp/rc45-validation-evidence/run-migrations.py`，每个引擎覆盖新库、从 rc.22-sxh.9（`a4611c1bc`）创建的代表库升级，以及从官方 rc.44 创建的代表库升级；旧版本工具分别从对应 Git 源码归档构建。rc.44 场景只是本地辅助检查，不是生产状态。
- **九组场景全部通过**；每组初始化或升级后再重复启动新版本，结构指纹一致。检查用户/令牌额度、语音渠道配置与映射、定价和消费日志保持，用户名及令牌 Key 唯一约束有效。旧令牌的新增 `AllowUserQuotaQuery` 权限保持关闭。
- MySQL / PostgreSQL 使用独立日志数据库；SQLite `integrity_check` 为 `ok`。验证完成后已删除本轮临时容器及匿名卷。原始日志保留。
- 这是合成存量数据矩阵，未使用真实生产快照，不代表生产数据迁移验收，也未验证 ClickHouse。

### 认证与前端检查边界

参考 [OWASP ASVS 5.0.0](https://owasp.org/projects/asvs)、[Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html) 和 [Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)。本轮重点核对新增用户额度查询的服务端授权开关、旧令牌默认权限、IP 白名单、令牌创建/更新语义，以及账户安全通知的语言调整；全量回归涵盖相关接口和现有证明过期、重放、用途绑定测试。未宣称完成完整 ASVS 合规审计。

前端直接集成上游实现与现有共享组件，未自行新增替代 UI；本轮没有真实浏览器交互验收。类型、构建和测试结果不替代页面验收，lint 存量错误也仍需单独处置。

## 生产升级前仍需完成

1. 以当前 **rc.22-sxh.9** 的一致性生产备份演练直接升级到最终 rc.45 候选，检查数据库、账务、渠道映射、插件价格及回滚；本轮的合成库通过不替代这一项。
2. 验证 Manager 的管理 API 和凭据迁移。rc.44 已引入的旧面板令牌 30 天退役逻辑会在生产首次升级到新版本时生效，不能把该期限算作线上已经开始。
3. 核对敏感词检查开关（rc.45 默认关闭）、通知开关、新令牌用户额度查询默认值与实际业务需求。rc.45 后主线已修复通知限流窗口，正式候选前评估是否需单独补入；Claude 新保活暂不随本次标签合并。
4. 对最终镜像进行真实火山 TTS/ASR、字幕、失败退款、图生图参数删除与重试、Qwen 编辑、任务插件和 Manager 验收。生产切换及付费请求需另行授权；回滚目标是 rc.22-sxh.9。

---

# rc.44 集成验证与上线准备（2026-10-15）

## 当前集成范围

在现有 `upgrade/rc40-validation` 分支合入官方标签 `v1.0.0-rc.44`（`633b4c5b1`），合并前为 `a1b95f52b`。保留 SXH `.7` ASR 资源映射、`.8` 图生图 multipart 参数删除和重试隔离，以及此前的语音字幕、流式输出、预扣和审计定制。没有引入 rc.44 之后 main 上的 14 个提交。

版本标识改为 `1.0.0-rc.44-sxh.dev`。这仍是开发验证线，不是生产候选或新稳定线；没有推送、发布镜像或修改生产。

两个文本冲突按语义处理：`controller/channel-test.go` 保留音频响应只记字节数，并使用 `common.LogText`；`controller/relay.go` 合并上游 i18n 导入并去掉重复 task DTO 导入。tokenkit 替换后，火山 TTS 字符数和 ASR 时长的特殊预扣分支仍优先执行；没有把语音改为普通文本 Token 计费。

## 本轮验证

环境：Go 1.26.0、Bun 1.3.11；根模块及 tokenkit 声明 Go 1.26，relaykit 声明 Go 1.25.1。原始日志、合成数据工具及上游对照位于未跟踪目录 `.tmp/rc44-validation-evidence/`。

| 检查 | 命令 / 结果 |
| --- | --- |
| 后端构建 | `GOMAXPROCS=2 go build -p 2 -o .tmp/rc44-validation-evidence/new-api` 通过 |
| 后端全量回归 | `GOMAXPROCS=2 go test -p 2 -timeout 10m ./...` 通过 |
| 语音并发回归 | `GOMAXPROCS=2 go test -p 2 -race ./relay/channel/volcengine ./relay/helper -run 'Volc\|Mapped\|SpeechOptions'` 通过 |
| SXH 定向复验 | `controller`、`relay`、`service` 的映射语音、参数和图片重试用例通过 |
| RelayKit 独立模块 | 模块内 `GOWORK=off GOMAXPROCS=2 go build -p 2 ./...` 和 `go test -p 2 ./...` 通过 |
| TokenKit 独立模块 | 模块内 `GOWORK=off GOMAXPROCS=2 go build -p 2 ./...` 和 `go test -p 2 ./...` 通过 |
| 前端依赖、类型和构建 | `bun install --frozen-lockfile`、`bun run typecheck`、`bun run build` 通过 |
| 前端全量回归 | `bun run test --maxWorkers=2`：188 个文件、2252 个用例全部通过 |
| 前端 lint | 未通过：164 errors、65 warnings；对原始 rc.44 使用同版本 oxlint 比较，文件、规则和消息完全一致，新增错误 0 个；没有顺带修改上游存量问题 |
| 数据库定向回归 | 配置 `TEST_MYSQL_DSN` / `TEST_POSTGRES_DSN` 后，`GOMAXPROCS=2 go test -p 1 ./model ./controller -run 'Migration\|Migrate\|DatabaseMatrix' -timeout 10m` 通过 |

### 真实数据库升级矩阵

执行 `python3 .tmp/rc44-validation-evidence/run-migrations.py`。旧库由 `sxh/rc22` 的 `6474cae52` 源码初始化；新库由本次合并源码初始化。MySQL 8.0.46、PostgreSQL 16.15 运行于仅绑定本机的临时容器，SQLite 3.50.4 使用项目实际驱动。每个引擎均验证新库和旧库升级，再重复启动新版本两次。

六组场景全部通过：用户/令牌额度、渠道语音配置和资源映射、价格、日志数据保持，用户名和 Token Key 唯一约束有效，重复迁移结构指纹一致。MySQL/PostgreSQL 配置独立日志库；SQLite 完整性检查为 `ok`。这是合成存量数据验证，不能替代生产快照迁移演练，也没有验证 ClickHouse。

### 认证验证边界

参考 [OWASP ASVS 5.0.0](https://owasp.org/projects/asvs)、[Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html) 和 [Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)。本次集成重点是服务端权限范围、凭据用途隔离、过期/撤销、敏感操作重新验证与单次证明，以及审计不泄露凭据。

后端全量回归包含既有访问令牌生命周期、scope 路由、过期令牌、证明重放/绑定、权限提升和审计脱敏用例。没有自行改写上游认证机制；本轮不是完整 ASVS 合规审计，生产 TLS、浏览器会话和 Manager 的真实凭据联调仍待验收。

## 上线方案与放行条件

1. **审定集成结果**：复核 SXH 差异、前端回归及存量 lint 处置，确定候选提交。构建唯一版本与提交号镜像，建议候选版本 `1.0.0-rc.44-sxh.1`，使用前核对未占用。当前 `.dev` 不能作为已验收候选。
2. **生产副本演练**：从当前线上一致性备份建立只读基线，在副本上完成 `.8 → rc.44` 迁移、重复启动与应用回滚试验，核对用户/额度/渠道/定价/任务/日志。不把演练数据库写回生产。
3. **Manager 兼容性**：Manager 客户端使用管理 API Bearer 凭据；上线前确认其当前类型。rc.44 首次启动写入旧面板访问令牌的 30 天退役时间，重启不续期。若仍使用旧令牌，应规划迁移到新 scoped PAT；目前调用涉及 `api_key:read`、`api_key:write`、`api_key:reveal`、`log:read`，仍受所属用户角色和权限限制。用隔离 Manager 验证授权码开通、充值/续期、额度、消费明细和趋势；不要直接更改线上凭据或重启 Manager。
4. **真实候选验收**：对最终镜像完成 TTS→ASR、字幕/SSE、失败退款、图生图参数删除与备用渠道重试、插件提交/轮询/下载及实际计费。涉及真实提供方请求时先明确请求范围和费用授权。本轮未发起付费请求。
5. **配置与用户通知**：明确 `DEFAULT_LANGUAGE=zh-CN` 是否保留中文默认消息；确认普通用户默认 365 天日志范围；告知游乐场历史聊天记录将清空一次。检查升级后的插件版本和已有定价，不能凭插件加载成功宣称业务兼容。
6. **切换与回滚**：备份数据库并校验字节数、SHA-256 和完整性，预先上传/校验镜像并保留 `.8` 镜像与 Compose。建议连续 30 秒业务静默、最多等 10 分钟，未达条件则延期；停旧实例前最后检查在途请求，单实例 SQLite 不并行共享写库。只切换 new-api，启动后 60 秒仍不健康则执行已经演练的回滚。没有安全 drain 时明确仍有长请求竞争窗口。涉及不兼容数据库恢复须单独授权，不能默认覆盖切换后新增账务数据。
7. **上线后验收**：内部健康、公开接口、Manager 业务、最小真实调用、计费/退款和日志隐私；观察 5xx、鉴权失败和任务积压，记录实际停机秒数。生产切换、自动迁移和必要的配置变更需在最终方案中明确授权范围。

当前结论：可以继续准备上线，但不能因代码合并或上述本地验证通过就直接判定生产可上线。

---

# rc.40 本地合并验证（2026-09-23）

## 结论和版本边界

初次合并验证在独立工作树完成；后续在主工作目录 `/home/joptear/new-api` 维护分支 `upgrade/rc40-validation`。该分支集成上游 `v1.0.0-rc.40`（`0aec08fee`）与 SXH `sxh/rc22`（`c480f4571`）。版本标识为 `1.0.0-rc.40-sxh.dev`，不是发布候选，也未建立新稳定线。

后端、RelayKit、语音并发回归、前端渠道测试及构建、三种真实数据库迁移验证通过。全局前端 lint 未通过，但与未修改的 rc.40 相比，182 个错误完全一致，没有新增错误。这次验证支持继续隔离联调，不等同于生产验收或插件系统已稳定。

## 合并处理

- SXH 音频 DTO 和错误重试标记迁入 RelayKit；保留其独立模块构建边界。
- 保留上游新的统一重试服务、渠道约束、会话策略；语音输出前超时的强制重试接入该服务，仍服从固定渠道、禁止重试和重试次数限制。
- 保留请求体可重放接口，同时保留 SXH multipart ASR、磁盘音频缓存、SSE/字幕及语音隐私审计。
- 保留上游模型映射路径，语音验证和 token 估算的共享映射增加相同的模型修饰符回退规则。
- 语音日志适配新的日志对象；保留默认音色、热词表配置，并遵守新渠道表单敏感字段锁定。
- 更新与上述接口变化相关的既有测试；新增渠道配置往返与切换渠道类型清理语音设置的两个前端回归用例。

## 验证记录

在升级工作树执行，Go 并发限制为 `GOMAXPROCS=2`、`-p 2`。

| 检查 | 结果 |
| --- | --- |
| 根模块 `go test -p 2 -timeout 10m ./...` | 通过 |
| 根模块 `go build -p 2` | 通过 |
| `relaykit/` 内 `GOWORK=off go build ./...` | 通过 |
| `relaykit/` 内 `GOWORK=off go test -p 2 ./...` | 通过 |
| `go test -p 2 -race ./relay/channel/volcengine ./relay/helper -run 'Volc\|Mapped\|SpeechOptions'` | 通过 |
| 前端 `bun install --frozen-lockfile`、`bun run typecheck`、`bun run build` | 通过 |
| 前端渠道既有测试 | 21 个文件、273 个用例通过 |
| 前端新增语音配置测试 | 1 个文件、2 个用例通过 |
| 前端全局 `bun run lint` | 182 个错误；用相同工具检查原始 rc.40，错误文件、规则和内容完全一致（忽略行号），新增 0 个 |
| Go 数据库 `Migration\|Migrate\|DatabaseMatrix` 定向测试 | SQLite、MySQL、PostgreSQL 通过 |

数据库使用独立合成数据，MySQL 8.0.46、PostgreSQL 16.15 和项目 SQLite 驱动。三个引擎均验证新库初始化、从当前 SXH rc.22 代码创建的库升级、随后连续两次启动迁移；检查用户/令牌额度、渠道模型映射及语音配置、价格配置、消费日志，以及用户名和令牌键唯一性。MySQL/PostgreSQL 使用单独的日志数据库，SQLite 额外检查 `integrity_check`。

首次空库验证脚本在 SQLite/MySQL 下因 GORM 向插入 map 加入 `@id` 而误报。脚本改为插入 map 副本后，用新建空库完整重跑并通过，没有为该误报修改业务代码。

本地原始日志和合成数据验证脚本保存在主仓库的 `.tmp/rc40-validation-evidence/`，不纳入 Git。它们仅含本地测试数据；临时数据库容器在验证完成后删除。

## 本地 review 修订

- multipart 语音选项校验在所有返回路径释放解析时产生的临时文件，保留请求体重放能力。回归覆盖未传选项、有效选项、未知字段、重复字段和 `null`。
- 渠道测试先按最终映射模型选择语音协议、ASR 上传体和请求 DTO，保留业务模型用于路由、价格及日志。回归通过拦截传输验证 TTS/ASR 链式别名的实际出站请求，不调用真实提供方。
- 新增回归用例在修订前代码上均失败、修订后通过；检查命令为 `go test ./controller ./relay/helper -run 'TestMappedVolcChannelTestSendsAudioProtocolWithBusinessModel|TestSpeechOptionsValidationCleansMultipartFilesAndPreservesReplay'`。
- 修订后根模块全量 `go test -p 2 -timeout 10m ./...`、语音定向 race 和 `git diff --check` 均通过。

## 上线前尚需完成

2026-10-08 已合入 Fork `origin/sxh/rc22` 的 `.7` 修订：`7f26f3ab6`（ASR 资源映射）及 `bcc2b0ffe`（生产验收记录）。rc.40 分支继续使用 `1.0.0-rc.40-sxh.dev`，保留此前的 multipart 清理、模型别名和 RelayKit 兼容修复。两代 ASR 资源均纳入渠道测试和预扣回归；ASR 渠道测试日志仅记录响应字节数。

旧的 `doubao-seed-asr-flash` 映射不再支持，升级实例的渠道应直接映射至 `volc.seedasr.auc` 或 `volc.bigasr.auc_turbo`。随源分支合入的 2026-10-05 生产验收记录属于 rc.22 稳定线，不代表本次 rc.40 合并已在生产验收。

此次合并后的定向回归、根模块全量 `go test -p 2 -timeout 10m ./...`、语音定向 race 和差异检查均通过；没有修改数据库结构、前端或 RelayKit API，也没有发起真实提供方请求。

- 使用生产数据库副本演练实际渠道配置、权限、账务和迁移；合成数据矩阵不能覆盖全部存量状态。
- 用真实提供方验收火山 TTS/ASR、字幕、流式输出、故障切换和实际账单；本轮没有发起付费提供方请求。
- MiniMax-H3 多模态视频输入及插件调用需要独立真实验收，不能由后端单测推出协议和生产可用性。
- 插件安装/升级/回退、管理界面真实操作、生产流量性能与长时间运行尚未验证。
- 发布前明确处理上游现有 lint 问题，并从审定的干净提交构建唯一版本镜像。

推送目标仅为 SXH Fork `ourfor-pp/new-api`（`origin`），分支为 `upgrade/rc40-validation`；不向官方 `upstream` 推送。未创建 PR、未发布镜像、未修改生产服务或稳定分支。后续生产部署仍需单独授权。
