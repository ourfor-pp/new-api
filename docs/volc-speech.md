# 火山语音渠道配置

火山 Seed-TTS 2.0 与录音文件极速 ASR 使用独立的火山语音渠道，公开模型分别为：

- `doubao-seed-tts-2.0`
- `doubao-seed-asr-flash`

渠道类型选择火山方舟（VolcEngine），但不要与已有文本或图像渠道共用。渠道 Key 支持两种格式：

- 新版控制台：直接填写单段 APP Key，网关使用 `X-Api-Key`。
- 旧版控制台：填写 `appid|access_token`，网关自动使用旧版鉴权头。

调用方继续使用 OpenAI 兼容接口：

- `POST /v1/audio/speech`
- `POST /v1/audio/transcriptions`

## TTS 音色

在渠道高级设置中配置 `Seed-TTS 2.0 默认音色 ID`。客户端传 `alloy`、`echo`、`fable`、`onyx`、`nova` 或 `shimmer` 时，网关统一映射到该默认音色；客户端直接传火山 speaker ID 时保持原值。

未配置默认音色时，使用 OpenAI 标准音色的请求会被明确拒绝。协议、资源 ID 和鉴权方式由服务端固定，客户端请求中的 `metadata` 不会覆盖这些字段。

## 计费倍率

两个模型都必须在模型倍率设置中显式配置价格，未配置时不得开放调用。不要在源码中写入火山易变的刊例价格，应按火山控制台当日后付费价格换算：

- TTS 倍率 = `人民币/万字符 ÷ USDExchangeRate ÷ 0.02`
- ASR 倍率 = `人民币/小时 ÷ USDExchangeRate ÷ 0.12`

用户分组倍率继续作为销售加价系数。

TTS 按火山返回的 `usage.text_words` 结算；仅在成功收到 `SessionFinished` 但缺少 usage 时，回退为 Unicode 字符数。ASR 按每分钟 1000 个内部计费单位预扣，并在完成后使用火山 `audio_info.duration` 校正。

## 支持边界

- TTS 输出：MP3、Opus、PCM；语速范围 0.5–2.0。
- ASR 输入：WAV、MP3、OGG/Opus；最大 100MB、最长 2 小时。
- ASR 输出：`json`、`text`、`verbose_json`。
- 不支持实时 WebSocket ASR、异步长文件 ASR、双向 TTS、SSE、声音复刻和阿里语音。
