# 火山语音稳定契约

## 模型与接口

| 业务模型 | 上游模型 | 接口 |
| --- | --- | --- |
| `sxh-tts` | `doubao-seed-tts-2.0` | `POST /v1/audio/speech` |
| `sxh-asr` | `doubao-seed-asr-flash` | `POST /v1/audio/transcriptions` |

- TTS 资源 ID：`seed-tts-2.0`，V3 HTTP Chunked。
- ASR 资源 ID：`volc.bigasr.auc_turbo`，录音文件极速版。
- 业务模型名只属于渠道映射和定价层。

## TTS

- 支持 MP3、Opus、PCM；语速 0.5–2.0。
- OpenAI 标准音色映射到渠道 `volc_speech.default_tts_speaker`；火山 speaker ID 原样传递。
- 顶层 `instructions` 表达语音指令；`speech_options.sample_rate` 支持 8k、16k、24k，未传默认 24k。
- `speech_options.context.texts` 表达有序文本上文；因当前 V3 只使用 `context_texts` 第一项，网关按客户端顺序合并文本，并把 `instructions` 放在最后。
- 当前单向资源不开放图片上下文、参考音频、音调、Markdown/SSML、双向和声音复刻；显式传入未支持字段必须返回 `400`。
- 裸音频保持原有二进制流。
- 字幕请求必须使用 `stream_format=sse`，并向火山设置 `enable_subtitle=true`。
- SSE 顺序：一个或多个 `speech.audio.delta`，可选 `sxh.speech.subtitle.delta`，可选 `sxh.speech.subtitle.done`，最后 `speech.audio.done`。
- 音频成功但无字幕时返回 `available:false`。
- 客户端必须在收到 `speech.audio.done` 前把结果视为未完成。

## ASR

- 支持 WAV、MP3、OGG/Opus，最大 100MB、最长 2 小时。
- 支持 `json`、`text`、`verbose_json`、`srt`、`vtt`。
- multipart `speech_options` 是且只能是一个 JSON 字符串；支持 ITN、标点、DDC、系统敏感词、VAD、强制分段、说话人、双声道、请求级热词、替换词和文本上下文。
- ITN、标点和 DDC 未传时默认开启；所有可选布尔和数值保留显式 `false`、`0`，无效显式值返回 `400`。
- `channel_mode=separate` 使用 `enable_channel_split=true` 和 `audio.channel=2`；上游 `speaker`、`channel_id` 映射到 `verbose_json` 的 segment/word。
- 平台热词表 ID 只允许配置在渠道 `volc_speech.asr_hotword_table_id`，业务客户端不能指定。
- 图片上下文和音乐/POI 结构化标注在当前资源没有得到可验证结果，因此显式传入返回 `400`；LID、情绪、性别、音量和语速检测不属于当前极速接口。
- `timestamp_granularities[]=segment|word` 只和 `verbose_json` 组合；兼容不带 `[]` 的重复字段。
- 未指定粒度时默认只返回 segment。
- SRT/VTT 从 utterances 生成，字词时间戳从毫秒转换为秒。

## 计费

- TTS：优先使用合法 `usage.text_words`；成功结束但缺少 usage 时回退 Unicode 字符数。
- ASR：按实际时长结算，每分钟换算 1000 个内部计费单位；上游缺少时长时使用本地解析值。
- 字幕和时间戳不重复计费。
- `sxh-tts`、`sxh-asr` 必须显式配置输入价格；用户分组倍率负责销售加价。

## 可靠性与隐私

- multipart 必须从可回放 `BodyStorage` 流式解析，禁止把磁盘缓存的 100MB 请求整体读回内存。
- TTS 已输出部分字节后不得切换渠道，避免拼接两次合成结果。
- 日志只记录模型、资源 ID、协议、LogID、用量、时长、字幕计数、选项名称、布尔状态、上下文/热词/替换词数量和 usage 来源。
- 不记录 Key、音频、完整合成文本、指令、上下文、热词、替换词、图片 URL 或字幕正文。
