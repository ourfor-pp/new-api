# 火山 TTS 与 ASR 客户端接入指南

本文面向调用深效科技语音 API 的客户端开发者，介绍以下两个模型：

- 文本转语音（TTS）：`sxh-tts`
- 录音转文字（ASR）：`sxh-asr`

两个接口采用 OpenAI 兼容路径。`sxh-tts` 和 `sxh-asr` 是客户端唯一应使用的业务模型名，服务端负责映射到底层火山模型。客户端只使用 new-api Token，不接触底层模型名、火山 APP Key、资源 ID 或鉴权信息。

## 1. 接入信息

正式客户端入口：

```text
https://aiapi.shenxiaokeji.com
```

鉴权方式：

```http
Authorization: Bearer <NEW_API_TOKEN>
```

使用 OpenAI SDK 时，Base URL 需要包含 `/v1`：

```text
https://aiapi.shenxiaokeji.com/v1
```

使用 curl 或原生 HTTP 请求时，直接调用完整接口路径：

```text
POST https://aiapi.shenxiaokeji.com/v1/audio/speech
POST https://aiapi.shenxiaokeji.com/v1/audio/transcriptions
```

上线前请确认：

- Token 已允许调用对应模型。
- 对应火山语音渠道已经启用。
- 两个模型都已经配置价格。
- TTS 渠道已经配置默认 Seed-TTS 2.0 音色。

可以先查询当前 Token 可用的模型：

```bash
curl https://aiapi.shenxiaokeji.com/v1/models \
  -H "Authorization: Bearer ${NEW_API_TOKEN}"
```

返回结果中应包含 `sxh-tts` 或 `sxh-asr`。如果只看到底层火山模型名，不要在客户端直接使用，应先检查渠道模型映射和 Token 模型权限。

## 2. TTS：文本转语音

### 2.1 接口

```http
POST /v1/audio/speech
Content-Type: application/json
Authorization: Bearer <NEW_API_TOKEN>
```

### 2.2 请求参数

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | 是 | 固定为 `sxh-tts` |
| `input` | string | 是 | 要合成的非空文本 |
| `voice` | string | 是 | 建议传 `alloy`，使用渠道配置的默认火山音色；也可直接传火山 speaker ID |
| `response_format` | string | 否 | `mp3`、`opus` 或 `pcm`，默认 `mp3` |
| `speed` | number | 否 | 语速范围 `0.5`–`2.0`，默认使用火山服务端语速 |
| `instructions` | string | 否 | 语音指令，例如“请用平静、自然的语气说话” |
| `speech_options` | object | 否 | 厂商中立语音扩展；当前 TTS 支持 `sample_rate` 和 `context.texts` |
| `stream_format` | string | 否 | `audio` 或 `sse`；默认 `audio`，请求字幕时必须为 `sse` |
| `timestamp_granularities` | string[] | 否 | `segment`、`word`；指定后默认返回 JSON 时间轴 |
| `subtitle_formats` | string[] | 否 | `json`、`srt`、`vtt`；指定后默认启用 segment 和 word 时间轴 |

以下 OpenAI 标准音色名都会映射到渠道配置的默认火山音色：

```text
alloy
echo
fable
onyx
nova
shimmer
```

如果客户端传入其他非空字符串，网关会把它当作火山 speaker ID 原样传递。普通客户端建议始终使用 `alloy`，避免依赖服务端具体音色 ID。

#### 可指定的火山音色

客户端可以把火山 `seed-tts-2.0` 音色的 `voice_type` 直接作为 `voice` 传入。网关不会维护音色白名单，也不会改写这类 speaker ID；最终是否可用取决于火山账号、应用和资源授权。

以下是火山官方音色目录中的常用选择：

| 场景 | 音色 | `voice` / `voice_type` | 语种 |
| --- | --- | --- | --- |
| 通用女声 | Vivi 2.0（推荐默认） | `zh_female_vv_uranus_bigtts` | 中文、日文、印尼语、墨西哥西班牙语；支持四川、陕西、东北方言 |
| 通用女声 | 小何 2.0 | `zh_female_xiaohe_uranus_bigtts` | 中文 |
| 通用女声 | 魅力苏菲 2.0 | `zh_female_sophie_uranus_bigtts` | 中文 |
| 通用女声 | 清新女声 2.0 | `zh_female_qingxinnvsheng_uranus_bigtts` | 中文 |
| 通用男声 | 云舟 2.0 | `zh_male_m191_uranus_bigtts` | 中文 |
| 通用男声 | 小天 2.0 | `zh_male_taocheng_uranus_bigtts` | 中文 |
| 通用男声 | 刘飞 2.0 | `zh_male_liufei_uranus_bigtts` | 中文 |
| 角色扮演 | 知性灿灿 2.0 | `zh_female_cancan_uranus_bigtts` | 中文 |
| 角色扮演 | 撒娇学妹 2.0 | `zh_female_sajiaoxuemei_uranus_bigtts` | 中文 |
| 视频配音 | 大壹 2.0 | `zh_male_dayi_uranus_bigtts` | 中文 |
| 视频配音 | 黑猫侦探社咪仔 2.0 | `zh_female_mizai_uranus_bigtts` | 中文 |
| 视频配音 | 鸡汤女 2.0 | `zh_female_jitangnv_uranus_bigtts` | 中文 |
| 视频配音 | 流畅女声 2.0 | `zh_female_liuchangnv_uranus_bigtts` | 中文 |
| 视频配音 | 儒雅逸辰 2.0 | `zh_male_ruyayichen_uranus_bigtts` | 中文 |
| 教育场景 | Tina 老师 2.0 | `zh_female_yingyujiaoxue_uranus_bigtts` | 中文、英式英语 |
| 客服场景 | 暖阳女声 2.0 | `zh_female_kefunvsheng_uranus_bigtts` | 中文 |
| 有声阅读 | 儿童绘本 2.0 | `zh_female_xiaoxue_uranus_bigtts` | 中文 |
| 美式英语 | Tim | `en_male_tim_uranus_bigtts` | 美式英语 |
| 美式英语 | Dacey | `en_female_dacey_uranus_bigtts` | 美式英语 |
| 美式英语 | Stokie | `en_female_stokie_uranus_bigtts` | 美式英语 |

火山会持续新增、调整音色，完整清单以[火山官方音色列表](https://www.volcengine.com/docs/6561/1257544)为准。服务端或管理工具需要结构化查询时，可使用火山官方 [`ListSpeakers`](https://api.volcengine.com/api-docs/view?action=ListSpeakers&serviceCode=speech_saas_prod&version=2025-05-20) API，并将 `ResourceIDs` 设为 `seed-tts-2.0`。官方目录中出现某个音色不代表当前火山应用必然已开通；客户端上线前应使用目标渠道做一次最小合成测试。

直接指定音色的请求示例：

```json
{
  "model": "sxh-tts",
  "input": "你好，这是一段指定音色的语音合成测试。",
  "voice": "zh_female_xiaohe_uranus_bigtts",
  "response_format": "mp3"
}
```

语音指令、引用上文和采样率示例：

```json
{
  "model": "sxh-tts",
  "input": "你头发长了，十年了，你还好吗？",
  "voice": "alloy",
  "response_format": "mp3",
  "instructions": "请用重逢时激动、克制的语气说话",
  "speech_options": {
    "sample_rate": 16000,
    "context": {
      "texts": [
        "是你吗？怎么看着好像没怎么变啊？",
        "挺好的，去年整理旧书时还翻到你写的毕业留言。"
      ]
    }
  }
}
```

`context.texts` 按数组顺序处理，最多 20 条；`instructions` 始终作为最后一条控制信息。火山当前只使用 `context_texts` 列表的第一项，因此网关会把上文和指令按顺序合并为一个有效上下文。未传 `sample_rate` 时保持 24k，当前可选 8k、16k、24k。

当前不支持：

- `wav`、`aac`、`flac`
- 双向流式输入
- 声音复刻
- 图片上下文、参考音频、音调和 Markdown/SSML 控制
- 通过 `metadata` 覆盖火山协议、资源 ID 或鉴权信息

请求字幕或时间轴时必须同时传入 `"stream_format":"sse"`。普通裸音频请求的响应协议保持不变；在裸音频模式中传入字幕参数会返回 `400`，不会静默忽略。

### 2.3 curl 示例

```bash
curl https://aiapi.shenxiaokeji.com/v1/audio/speech \
  -H "Authorization: Bearer ${NEW_API_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sxh-tts",
    "input": "你好，这是一段语音合成测试。",
    "voice": "alloy",
    "response_format": "mp3",
    "speed": 1.0
  }' \
  --output speech.mp3
```

生成 Opus：

```bash
curl https://aiapi.shenxiaokeji.com/v1/audio/speech \
  -H "Authorization: Bearer ${NEW_API_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sxh-tts",
    "input": "这是一段 Opus 音频。",
    "voice": "alloy",
    "response_format": "opus"
  }' \
  --output speech.ogg
```

### 2.4 SSE 音频与字幕示例

以下请求同时返回 Base64 音频块、句级与字词级 JSON 时间轴，以及最终 SRT/VTT：

```bash
curl -N https://aiapi.shenxiaokeji.com/v1/audio/speech \
  -H "Authorization: Bearer ${NEW_API_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sxh-tts",
    "input": "你好，2026年。这里是第二句。",
    "voice": "alloy",
    "response_format": "mp3",
    "stream_format": "sse",
    "timestamp_granularities": ["segment", "word"],
    "subtitle_formats": ["json", "srt", "vtt"]
  }'
```

响应采用 `text/event-stream`。每个 `data:` 块都是一个 JSON 对象，事件顺序如下：

```text
speech.audio.delta
...可以有多个音频块...
sxh.speech.subtitle.delta
...可以有多句字幕...
sxh.speech.subtitle.done
speech.audio.done
```

主要事件结构：

```json
{
  "type": "speech.audio.delta",
  "audio": "<BASE64_AUDIO_CHUNK>"
}
```

```json
{
  "type": "sxh.speech.subtitle.delta",
  "index": 0,
  "text": "你好，2026年。",
  "startTime": 0.12,
  "endTime": 1.86,
  "words": [
    {
      "word": "你好",
      "startTime": 0.12,
      "endTime": 0.68,
      "confidence": 0.98
    }
  ]
}
```

```json
{
  "type": "sxh.speech.subtitle.done",
  "available": true,
  "sentence_count": 2,
  "word_count": 8,
  "formats": {
    "srt": "1\n00:00:00,120 --> 00:00:01,860\n你好，2026年。\n\n",
    "vtt": "WEBVTT\n\n00:00:00.120 --> 00:00:01.860\n你好，2026年。\n\n"
  }
}
```

`startTime` 和 `endTime` 统一为秒。字词内容、顺序、标点、数字组合和时间区间完全保留火山返回，不保证每个 Unicode 字符都是一个独立字词。若音频成功但火山没有给出有效字幕，合成仍成功，`subtitle.done.available` 为 `false`。

TypeScript 原生 `fetch` 客户端示例：

```typescript
import { writeFile } from 'node:fs/promises'

type SSEEvent = {
  type: string
  audio?: string
  formats?: Record<string, string>
  [key: string]: unknown
}

const response = await fetch(
  'https://aiapi.shenxiaokeji.com/v1/audio/speech',
  {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${process.env.NEW_API_TOKEN}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      model: 'sxh-tts',
      input: '你好，2026年。这里是第二句。',
      voice: 'alloy',
      response_format: 'mp3',
      stream_format: 'sse',
      timestamp_granularities: ['segment', 'word'],
      subtitle_formats: ['json', 'srt', 'vtt'],
    }),
  },
)

if (!response.ok || !response.body) {
  throw new Error(`TTS 请求失败：${response.status} ${await response.text()}`)
}

const audioChunks: Uint8Array[] = []
const subtitleEvents: SSEEvent[] = []
const decoder = new TextDecoder()
let pending = ''
let audioDone = false

const consumeEvent = (block: string) => {
  const data = block
    .split('\n')
    .filter((line) => line.startsWith('data: '))
    .map((line) => line.slice(6))
    .join('\n')
  if (!data) return

  const event = JSON.parse(data) as SSEEvent
  if (event.type === 'speech.audio.delta' && event.audio) {
    // 每个 Base64 音频块要分别解码，再按事件顺序拼接二进制。
    audioChunks.push(Uint8Array.from(Buffer.from(event.audio, 'base64')))
  } else if (
    event.type === 'sxh.speech.subtitle.delta' ||
    event.type === 'sxh.speech.subtitle.done'
  ) {
    // 在 speech.audio.done 前只缓存字幕，不展示或持久化未完成结果。
    subtitleEvents.push(event)
  } else if (event.type === 'speech.audio.done') {
    audioDone = true
  } else if (event.type === 'error') {
    throw new Error(`TTS 流中断：${JSON.stringify(event)}`)
  }
}

for await (const chunk of response.body) {
  pending += decoder.decode(chunk, { stream: true })
  // 拼接后再规范化，兼容 CRLF 恰好跨越两个网络分块的情况。
  pending = pending.replace(/\r\n/g, '\n')
  let boundary = pending.indexOf('\n\n')
  while (boundary >= 0) {
    consumeEvent(pending.slice(0, boundary))
    pending = pending.slice(boundary + 2)
    boundary = pending.indexOf('\n\n')
  }
}
pending += decoder.decode()
pending = pending.replace(/\r\n/g, '\n')
if (pending.trim()) consumeEvent(pending)

if (!audioDone) {
  audioChunks.length = 0
  subtitleEvents.length = 0
  throw new Error('TTS 流在 speech.audio.done 前结束，已丢弃不完整音频和字幕')
}

for (const event of subtitleEvents) {
  if (event.type === 'sxh.speech.subtitle.delta') {
    console.log('字幕时间轴：', event)
  } else {
    console.log('最终字幕：', event.formats)
  }
}

await writeFile(
  'speech.mp3',
  Buffer.concat(audioChunks.map((chunk) => Buffer.from(chunk))),
)
```

未修改的 OpenAI SDK 不解析 `sxh.speech.subtitle.*` 扩展事件；需要字幕时应使用原生 HTTP/SSE 客户端。只调用普通裸音频的现有 SDK 代码不受影响。

### 2.5 JavaScript 裸音频示例

使用官方 OpenAI JavaScript SDK：

```javascript
import fs from 'node:fs/promises'
import OpenAI from 'openai'

const client = new OpenAI({
  apiKey: process.env.NEW_API_TOKEN,
  baseURL: 'https://aiapi.shenxiaokeji.com/v1',
})

const response = await client.audio.speech.create({
  model: 'sxh-tts',
  input: '你好，这是一段语音合成测试。',
  voice: 'alloy',
  response_format: 'mp3',
  speed: 1.0,
  instructions: '请用平静、自然的语气说话',
  extra_body: {
    speech_options: {
      sample_rate: 16000,
      context: { texts: ['上一轮正在讨论发布计划。'] },
    },
  },
})

const audio = Buffer.from(await response.arrayBuffer())
await fs.writeFile('speech.mp3', audio)
```

### 2.6 Python 裸音频示例

使用官方 OpenAI Python SDK：

```python
import os
from pathlib import Path

from openai import OpenAI

client = OpenAI(
    api_key=os.environ["NEW_API_TOKEN"],
    base_url="https://aiapi.shenxiaokeji.com/v1",
)

output = Path("speech.mp3")
with client.audio.speech.with_streaming_response.create(
    model="sxh-tts",
    input="你好，这是一段语音合成测试。",
    voice="alloy",
    response_format="mp3",
    speed=1.0,
    instructions="请用平静、自然的语气说话",
    extra_body={
        "speech_options": {
            "sample_rate": 16000,
            "context": {"texts": ["上一轮正在讨论发布计划。"]},
        }
    },
) as response:
    response.stream_to_file(output)
```

### 2.7 TTS 裸音频响应

接口直接返回音频二进制，不返回 JSON：

| `response_format` | Content-Type | 建议文件扩展名 |
| --- | --- | --- |
| `mp3` | `audio/mpeg` | `.mp3` |
| `opus` | `audio/ogg` | `.ogg` |
| `pcm` | `audio/pcm` | `.pcm` |

音频使用 HTTP Chunked 方式边生成边返回。响应头可能包含：

```http
X-Volc-Logid: <火山请求日志 ID>
```

建议客户端先写入临时文件，例如 `speech.mp3.part`，完整读取响应后再重命名。若连接在输出过程中中断，应删除临时文件并提示重试，不能把部分音频当作完整结果。

## 3. ASR：录音转文字

### 3.1 接口

```http
POST /v1/audio/transcriptions
Content-Type: multipart/form-data
Authorization: Bearer <NEW_API_TOKEN>
```

### 3.2 请求参数

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | 是 | 固定为 `sxh-asr` |
| `file` | file | 是 | 要识别的音频文件 |
| `response_format` | string | 否 | `json`、`text`、`verbose_json`、`srt` 或 `vtt`，默认 `json` |
| `timestamp_granularities[]` | string | 否 | `verbose_json` 专用，可重复传 `segment`、`word` |
| `speech_options` | string | 否 | JSON 字符串形式的厂商中立 ASR 选项，必须只出现一次 |

`speech_options` 当前支持：

| 字段 | 类型 | 默认/范围 | 说明 |
| --- | --- | --- | --- |
| `text_normalization` | boolean | 默认 `true` | 数字规整 ITN；显式 `false` 会发送给上游 |
| `punctuation` | boolean | 默认 `true` | 标点预测 |
| `semantic_smoothing` | boolean | 默认 `true` | 语义顺滑 DDC |
| `sensitive_word_filter` | boolean | 未传不启用 | 使用火山系统敏感词过滤；不允许客户端上传自定义词 |
| `vad_segmentation` | boolean | 仅支持 `true` | 当前资源没有可验证的关闭参数，传 `false` 返回 `400` |
| `speaker_diarization` | boolean | 未传不启用 | 自动说话人分离 |
| `channel_mode` | string | `mixed` / `separate` | `separate` 用于双声道独立识别 |
| `force_segment_after_ms` | integer | 200–60000 | 强制判停/分段阈值，单位毫秒 |
| `hotwords` | string[] | 最多 5000 项 | 请求级热词 |
| `replacements` | object | 最多 5000 项 | 键为原词、值为替换词 |
| `context.texts` | string[] | 最多 20 条 | 文本上下文 |

平台级热词表由服务端渠道配置，客户端不能传热词表 ID。未知字段、图片上下文和 `detect` 都返回 `400`，不会静默忽略。

音频限制：

- 支持 WAV、MP3、OGG 和 Opus。
- 文件最大 100MB。
- 音频最长 2 小时。
- 文件名必须带有正确扩展名，网关会根据扩展名校验格式。

当前不支持：

- 实时 WebSocket ASR
- 流式返回转写文本
- 异步长文件识别
- 客户端自定义火山资源 ID、鉴权或协议

未传 `speech_options` 时，服务端继续启用数字规整、标点、语义顺滑和分段信息，保持旧客户端行为。客户端传入 `language`、`prompt` 或 `temperature` 不会改变火山请求，不应依赖这些参数。

时间戳参数只允许与 `response_format=verbose_json` 组合。未传 `timestamp_granularities` 时默认只返回句段级 `segments`，保持旧客户端兼容。字段名也兼容不带 `[]` 的重复表单字段：

```text
timestamp_granularities[]=segment
timestamp_granularities[]=word
```

或：

```text
timestamp_granularities=segment
timestamp_granularities=word
```

### 3.3 curl 示例

返回 JSON：

```bash
curl https://aiapi.shenxiaokeji.com/v1/audio/transcriptions \
  -H "Authorization: Bearer ${NEW_API_TOKEN}" \
  -F "model=sxh-asr" \
  -F "response_format=json" \
  -F "file=@./meeting.mp3"
```

返回纯文本：

```bash
curl https://aiapi.shenxiaokeji.com/v1/audio/transcriptions \
  -H "Authorization: Bearer ${NEW_API_TOKEN}" \
  -F "model=sxh-asr" \
  -F "response_format=text" \
  -F "file=@./meeting.mp3"
```

返回带分段时间戳的 JSON：

```bash
curl https://aiapi.shenxiaokeji.com/v1/audio/transcriptions \
  -H "Authorization: Bearer ${NEW_API_TOKEN}" \
  -F "model=sxh-asr" \
  -F "response_format=verbose_json" \
  -F "file=@./meeting.mp3"
```

同时返回句段和字词时间戳：

```bash
curl https://aiapi.shenxiaokeji.com/v1/audio/transcriptions \
  -H "Authorization: Bearer ${NEW_API_TOKEN}" \
  -F "model=sxh-asr" \
  -F "response_format=verbose_json" \
  -F "timestamp_granularities[]=segment" \
  -F "timestamp_granularities[]=word" \
  -F "file=@./meeting.mp3"
```

启用说话人、双声道、热词、替换词和上下文：

```bash
curl https://aiapi.shenxiaokeji.com/v1/audio/transcriptions \
  -H "Authorization: Bearer ${NEW_API_TOKEN}" \
  -F "model=sxh-asr" \
  -F "response_format=verbose_json" \
  -F "timestamp_granularities[]=segment" \
  -F "timestamp_granularities[]=word" \
  -F 'speech_options={"speaker_diarization":true,"channel_mode":"separate","hotwords":["深效科技"],"replacements":{"深校科技":"深效科技"},"context":{"texts":["这是一场产品会议。"]}}' \
  -F "file=@./stereo-meeting.mp3"
```

直接生成 SRT：

```bash
curl https://aiapi.shenxiaokeji.com/v1/audio/transcriptions \
  -H "Authorization: Bearer ${NEW_API_TOKEN}" \
  -F "model=sxh-asr" \
  -F "response_format=srt" \
  -F "file=@./meeting.mp3" \
  --output meeting.srt
```

生成 VTT 时把 `response_format` 改为 `vtt`，建议输出文件扩展名使用 `.vtt`。SRT/VTT 直接使用火山 `utterances` 的句子文本和时间区间，不需要额外传 timestamp granularities。

### 3.4 JavaScript 示例

使用官方 OpenAI JavaScript SDK：

```javascript
import fs from 'node:fs'
import OpenAI from 'openai'

const client = new OpenAI({
  apiKey: process.env.NEW_API_TOKEN,
  baseURL: 'https://aiapi.shenxiaokeji.com/v1',
})

const result = await client.audio.transcriptions.create({
  model: 'sxh-asr',
  file: fs.createReadStream('./meeting.mp3'),
  response_format: 'verbose_json',
  timestamp_granularities: ['segment', 'word'],
  extra_body: {
    speech_options: JSON.stringify({
      speaker_diarization: true,
      hotwords: ['深效科技'],
      context: { texts: ['这是一场产品会议。'] },
    }),
  },
})

console.log(result.text)
console.log(result.duration)
console.log(result.segments)
console.log(result.words)
```

### 3.5 Python 示例

使用官方 OpenAI Python SDK：

```python
import json
import os

from openai import OpenAI

client = OpenAI(
    api_key=os.environ["NEW_API_TOKEN"],
    base_url="https://aiapi.shenxiaokeji.com/v1",
)

with open("meeting.mp3", "rb") as audio:
    result = client.audio.transcriptions.create(
        model="sxh-asr",
        file=audio,
        response_format="verbose_json",
        timestamp_granularities=["segment", "word"],
        extra_body={
            "speech_options": json.dumps(
                {
                    "speaker_diarization": True,
                    "hotwords": ["深效科技"],
                    "context": {"texts": ["这是一场产品会议。"]},
                },
                ensure_ascii=False,
            )
        },
    )

print(result.text)
print(result.duration)
print(result.segments)
print(result.words)
```

### 3.6 ASR 响应

`response_format=json`：

```json
{
  "text": "你好，这是一段录音转写结果。"
}
```

`response_format=text`：

```text
你好，这是一段录音转写结果。
```

`response_format=verbose_json`：

```json
{
  "task": "transcribe",
  "duration": 3.2,
  "text": "你好，这是一段录音转写结果。",
  "segments": [
    {
      "id": 0,
      "seek": 0,
      "start": 0.2,
      "end": 3.0,
      "text": "你好，这是一段录音转写结果。",
      "speaker": "1",
      "channel": 1,
      "tokens": null,
      "temperature": 0,
      "avg_logprob": 0,
      "compression_ratio": 0,
      "no_speech_prob": 0
    }
  ],
  "words": [
    {
      "word": "你好",
      "start": 0.2,
      "end": 0.74,
      "confidence": 0.98,
      "speaker": "1",
      "channel": 1
    },
    {
      "word": "2026年",
      "start": 0.74,
      "end": 1.52,
      "confidence": 0.96,
      "speaker": "1",
      "channel": 1
    }
  ]
}
```

`segments` 和顶层 `words` 的时间单位都是秒。`speaker` 和 `channel` 只在对应能力启用且火山返回时出现；word 继承所属 segment 的标识。`confidence` 只在火山返回该字段时出现。字词内容、顺序、标点、数字组合和时间区间保持火山原样，不保证每个 Unicode 字符独立成词；火山未返回 words 时，响应中也不会伪造或均分时间轴。

SRT 示例：

```srt
1
00:00:00,200 --> 00:00:03,000
你好，这是一段录音转写结果。
```

VTT 示例：

```vtt
WEBVTT

00:00:00.200 --> 00:00:03.000
你好，这是一段录音转写结果。
```

响应头可能包含：

```http
X-Volc-Logid: <火山请求日志 ID>
```

纯静音音频可能成功返回空文本，这不代表接口故障。

## 4. 计费口径

客户端不需要上传计费参数，服务端会按照实际成功用量结算：

- TTS：优先使用火山返回的实际计费字符数；成功但缺少用量时回退到 Unicode 字符数。
- ASR：按音频时长结算，每分钟换算为 1000 个内部计费单位，并使用火山返回的实际音频时长校正。
- 用户分组倍率仍会参与最终扣费。

调用失败通常会退回预扣额度。TTS 已经向客户端输出部分音频后如果上游连接中断，不会自动切换渠道重放，以免音频重复；该请求会记录为部分流失败并退回预扣额度。

时间戳和 SRT/VTT 是同一次语音请求的附加输出，不重复计费。

## 5. 错误处理

非音频成功响应采用 new-api 标准 JSON 错误结构：

```json
{
  "error": {
    "message": "错误说明",
    "type": "new_api_error",
    "code": ""
  }
}
```

常见 HTTP 状态：

| 状态码 | 常见原因 | 客户端建议 |
| --- | --- | --- |
| `400` | 缺少参数、格式不支持、语速越界、文件超限 | 修正请求，不要直接重试 |
| `401` | Token 缺失或无效 | 重新获取或检查 new-api Token |
| `403` | Token 未授权该模型或用户额度不足 | 检查模型权限和余额 |
| `429` | 请求过快或火山限流 | 指数退避后重试 |
| `5xx` | 上游服务、网络或协议异常 | 短暂退避后重试，并记录请求 ID、`X-Volc-Logid` |

建议客户端：

1. 为每次请求设置合理超时；ASR 长音频需要比普通文本请求更长的读取超时。
2. 记录 HTTP 状态、new-api 请求 ID 以及 `X-Volc-Logid`，但不要记录 Token、完整合成文本或音频内容。
3. 仅对网络错误、`429` 和 `5xx` 自动重试。
4. TTS 重试时重新创建输出文件，避免把两次响应拼接到同一个文件。
5. ASR 请求体包含文件流，重试时必须重新打开文件，不能复用已经读取完的流。
6. TTS SSE 收到 `error` 事件或连接在 `speech.audio.done` 前结束时，丢弃已拼接的音频和字幕；服务端不会在已经输出首字节后切换渠道。

## 6. 与 OpenAI 标准接口的差异

接口路径和基础请求结构兼容 OpenAI Audio API，但模型能力以本文为准：

| 项目 | 当前实现 |
| --- | --- |
| TTS 模型 | 客户端使用 `sxh-tts`，服务端映射到底层火山模型 |
| TTS 音色 | OpenAI 六个标准名称映射到渠道默认音色，或直传火山 speaker ID |
| TTS 格式 | MP3、Opus、PCM |
| TTS 扩展 | 8k/16k/24k、语音指令、文本引用上文 |
| TTS SSE | 支持 `speech.audio.delta`、`speech.audio.done` 和 `sxh.speech.subtitle.*` 扩展事件 |
| TTS 字幕 | 句级、字词级 JSON 时间轴，以及 SRT、VTT |
| ASR 模型 | 客户端使用 `sxh-asr`，服务端映射到底层火山模型 |
| ASR 输入 | WAV、MP3、OGG、Opus，最大 100MB、最长 2 小时 |
| ASR 输出 | JSON、纯文本、Verbose JSON、SRT、VTT |
| ASR 时间戳 | 句级和火山原始字词单元，统一为秒 |
| ASR 扩展 | ITN、标点、DDC、敏感词、VAD、强制分段、说话人、双声道、热词、替换词、文本上下文 |
| ASR 实时流 | 不支持 |

OpenAI SDK 只负责构造兼容请求，实际请求会发送到深效科技服务，不会发送到 OpenAI。客户端必须显式配置本文给出的 Base URL 和模型名称。

## 7. 参考资料

- [OpenAI Audio API 参考](https://platform.openai.com/docs/api-reference/audio)
- [火山 Seed-TTS 2.0 V3 HTTP Chunked 接口](https://www.volcengine.com/docs/6561/1598757?lang=zh)
- [火山大模型录音文件极速版 ASR 接口](https://www.volcengine.com/docs/6561/1631584?lang=zh)
- [SXH 当前资源能力矩阵](./volc-speech-capability-matrix.md)
