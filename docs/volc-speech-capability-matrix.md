# 火山语音能力矩阵

本文只描述 SXH 当前接入的两个资源：

- TTS：`seed-tts-2.0`，V3 HTTP Chunked 单向接口。
- ASR：`volc.bigasr.auc_turbo`，录音文件极速接口。

“上游接受”只表示当前账号的最小真实请求成功；只有协议语义和响应也可验证的能力才会对客户端开放。所有未开放字段都返回 `400`，不会静默忽略。

## 统一协议

客户端继续使用 OpenAI Audio API，并通过厂商中立的 `speech_options` 扩展。TTS 在 JSON 请求中传对象；ASR 在 multipart 中传 JSON 字符串。客户端不能传资源 ID、鉴权、火山原始请求体或任意透传参数。

## TTS

| 能力 | `.6` 状态 | 说明 |
| --- | --- | --- |
| MP3、Opus、PCM | 支持 | 保持现有裸音频和 SSE 行为 |
| 语速 | 支持 | 顶层 `speed`，范围 `0.5`–`2.0` |
| 采样率 | 支持 | `speech_options.sample_rate`：8k、16k、24k；默认 24k |
| 语音指令 | 支持 | 顶层 `instructions` |
| 引用上文 | 支持 | `speech_options.context.texts`，最多 20 条且合计不超过 8000 UTF-8 字节 |
| 指令与上文顺序 | 支持 | 上文按客户端顺序合并，`instructions` 最后加入；火山当前只使用 `context_texts` 的第一项，因此网关发送一个合并后的有效上下文 |
| SSE、时间轴、SRT/VTT | 支持 | 计费和完成事件语义不变 |
| 图片上下文 | 不支持 | 当前单向 V3 API 未给出可验证字段，传入返回 `400` |
| 参考音频 | 不支持 | 当前资源和协议未验证 |
| 音调 | 不支持 | 未纳入公共协议，也未做当前资源真实探测 |
| Markdown/SSML 控制 | 不支持 | 不开放厂商原始 additions 或任意透传 |
| 双向、长文本、声音复刻 | 不支持 | 不属于当前资源 |

真实探测使用生产数据库只读副本中的当前账号配置完成。8k、16k、24k、生产默认 Vivi 2.0 音色、云舟 2.0 音色和 `context_texts` 均成功返回音频；上下文不参与网关计费字符数。

## ASR

| 能力 | `.6` 状态 | 公共字段 / 说明 |
| --- | --- | --- |
| ITN | 支持 | `text_normalization`，未传默认开启，显式 `false` 保留 |
| 标点 | 支持 | `punctuation`，未传默认开启，显式 `false` 保留 |
| 语义顺滑 DDC | 支持 | `semantic_smoothing`，为兼容现有行为未传默认开启 |
| 系统敏感词过滤 | 支持 | `sensitive_word_filter`；不允许客户端上传自定义敏感词 |
| VAD 分句 | 部分支持 | `vad_segmentation=true` 使用当前资源默认语义；当前资源没有可验证的关闭参数，显式 `false` 返回 `400` |
| 强制分段 | 支持 | `force_segment_after_ms`，范围 200–60000ms |
| 说话人分离 | 支持 | `speaker_diarization`；结果映射到 segment/word 的 `speaker` |
| 双声道分离 | 支持 | `channel_mode=mixed|separate`；`separate` 使用 `enable_channel_split` 和双声道输入，结果映射到 segment/word 的 `channel` |
| 请求级热词 | 支持 | `hotwords`，最多 5000 项且合计不超过 20000 UTF-8 字节 |
| 平台级热词表 | 支持 | 渠道管理端配置 `asr_hotword_table_id`；客户端不能指定 |
| 正则/普通替换 | 支持 | `replacements`，最多 5000 项，键和值合计不超过 40000 UTF-8 字节 |
| 文本上下文 | 支持 | `context.texts`，最多 20 条且合计不超过 8000 UTF-8 字节 |
| 图片上下文 | 不支持 | 当前 `volc.bigasr.auc_turbo` 对探测请求成功但没有可证明的模型效果，可能是忽略未知能力，因此传入返回 `400` |
| 音乐/POI 优化 | 不开放 | 上游接受开关，但当前资源没有返回可映射的结构化结果；`detect` 返回 `400` |
| `annotations[]` | 协议预留 | 固定结构为 `type/start/end/label/confidence`；当前资源不会返回 |
| LID、情绪、性别、音量、语速 | 不支持 | 极速接口明确移除这些字段 |
| 实时、异步、超过 2 小时 | 不支持 | 不属于当前极速资源 |

真实探测中，ITN/标点/DDC 的显式 `false`、说话人、系统敏感词过滤行为、VAD/强制分段字段、热词、替换词、文本上下文均成功；替换词实际改变转写结果。双声道使用两种音色生成独立左右声道，只有 `enable_channel_split=true` 配合 `audio.channel=2` 返回 `channel_id`，旧式 `channel_split` 字段未生效，因此实现使用前者。

## 计费和隐私

- TTS 仍按火山 `usage.text_words` 结算；上下文和指令不会写入日志。
- ASR 仍按火山返回的实际音频时长结算；语音选项不改变网关计费公式。
- 请求日志只记录选项名称、布尔状态和上下文/热词/替换词数量，不记录正文、指令、图片 URL、热词、替换词或音频。
- 网关计费公式不变不代表火山侧一定没有能力附加费。`.6` 新候选进入生产前，必须用隔离请求对照火山控制台用量/账单增量，或取得明确的官方计费确认并留存结论；若任一能力存在独立收费，在补齐定价和结算规则前不得上线该能力。
