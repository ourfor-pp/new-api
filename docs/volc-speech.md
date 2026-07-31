# 火山语音渠道配置

火山 Seed-TTS 2.0 与录音文件极速 ASR 使用独立的火山语音渠道。服务端底层模型分别为：

- `doubao-seed-tts-2.0`
- `doubao-seed-asr-flash`

生产环境对客户端只公开以下映射模型：

- `sxh-tts` → `doubao-seed-tts-2.0`
- `sxh-asr` → `doubao-seed-asr-flash`

渠道类型选择火山方舟（VolcEngine），但不要与已有文本或图像渠道共用。渠道 Key 支持两种格式：

- 新版控制台：直接填写单段 APP Key，网关使用 `X-Api-Key`。
- 旧版控制台：填写 `appid|access_token`，网关自动使用旧版鉴权头。

调用方继续使用 OpenAI 兼容接口：

- `POST /v1/audio/speech`
- `POST /v1/audio/transcriptions`

## TTS 音色

在渠道高级设置中配置 `Seed-TTS 2.0 默认音色 ID`。客户端传 `alloy`、`echo`、`fable`、`onyx`、`nova` 或 `shimmer` 时，网关统一映射到该默认音色；客户端直接传火山 speaker ID 时保持原值。

未配置默认音色时，使用 OpenAI 标准音色的请求会被明确拒绝。协议、资源 ID 和鉴权方式由服务端固定，客户端请求中的 `metadata` 不会覆盖这些字段。

## 模型定价

两个对外映射模型 `sxh-tts` 和 `sxh-asr` 都必须在模型倍率设置中显式配置价格，未配置时不得开放调用。计费按客户端请求的映射模型名匹配，不要只给底层模型名配置价格。不要在源码中写入火山易变的刊例价格，应按火山控制台当日后付费价格换算：

- TTS“输入价格” = `人民币/万字符 × 100`
- ASR“输入价格” = `人民币/小时 × 16.6667`

定价界面虽然显示美元符号和“$/1M”，本项目实际按人民币 1:1 记账，不做汇率换算。两个模型都选择“按 Token”，只填写“输入价格”，补全、缓存、图像、音频输入和音频输出价格均保持关闭。这里的 Token 是兼容 new-api 计费框架的内部计费单位，不代表模型实际使用文本 Token。

用户分组倍率继续作为销售加价系数。

TTS 按火山返回的 `usage.text_words` 结算；仅在成功收到 `SessionFinished` 但缺少 usage 时，回退为 Unicode 字符数。ASR 按每分钟 1000 个内部计费单位预扣，并在完成后使用火山 `audio_info.duration` 校正。

## 故障切换

如需主渠道失败后自动切换到备用渠道，必须同时满足：

- 主、备渠道都配置对应的火山语音模型，并使用不同优先级。
- 系统重试次数 `RetryTimes` 至少设置为 `1`；设置为 `0` 时不会选择备用渠道。
- TTS 只有在尚未向客户端输出音频时允许切换；已经输出部分音频后不会重放请求。

渠道 Key 格式错误、缺少默认 TTS 音色、火山限流、服务端错误以及输出前的协议响应异常均可尝试备用渠道。是否自动禁用故障渠道仍由系统自动禁用设置和渠道 `AutoBan` 共同控制。

## 支持边界

- TTS 输出：MP3、Opus、PCM；语速范围 0.5–2.0。
- ASR 输入：WAV、MP3、OGG/Opus；最大 100MB、最长 2 小时。
- ASR 输出：`json`、`text`、`verbose_json`。
- 不支持实时 WebSocket ASR、异步长文件 ASR、双向 TTS、SSE、声音复刻和阿里语音。
