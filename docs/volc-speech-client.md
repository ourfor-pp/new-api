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

当前不支持：

- `wav`、`aac`、`flac`
- SSE 格式的 TTS
- 双向流式输入
- 声音复刻
- 通过 `metadata` 覆盖火山协议、资源 ID 或鉴权信息

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

### 2.4 JavaScript 示例

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
})

const audio = Buffer.from(await response.arrayBuffer())
await fs.writeFile('speech.mp3', audio)
```

### 2.5 Python 示例

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
) as response:
    response.stream_to_file(output)
```

### 2.6 TTS 响应

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
| `response_format` | string | 否 | `json`、`text` 或 `verbose_json`，默认 `json` |

音频限制：

- 支持 WAV、MP3、OGG 和 Opus。
- 文件最大 100MB。
- 音频最长 2 小时。
- 文件名必须带有正确扩展名，网关会根据扩展名校验格式。

当前不支持：

- `srt`、`vtt`
- 实时 WebSocket ASR
- 流式返回转写文本
- 异步长文件识别
- 客户端自定义火山资源 ID、鉴权或协议

当前服务端固定启用数字规整、标点、语义顺滑和分段信息。客户端传入 `language`、`prompt`、`temperature` 或 `timestamp_granularities` 不会改变火山请求，不应依赖这些参数。

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
})

console.log(result.text)
console.log(result.duration)
console.log(result.segments)
```

### 3.5 Python 示例

使用官方 OpenAI Python SDK：

```python
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
    )

print(result.text)
print(result.duration)
print(result.segments)
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
      "tokens": null,
      "temperature": 0,
      "avg_logprob": 0,
      "compression_ratio": 0,
      "no_speech_prob": 0
    }
  ]
}
```

`verbose_json` 当前提供句段级开始和结束时间，不提供可靠的 Token、置信度或逐词时间戳。响应头可能包含：

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

## 6. 与 OpenAI 标准接口的差异

接口路径和基础请求结构兼容 OpenAI Audio API，但模型能力以本文为准：

| 项目 | 当前实现 |
| --- | --- |
| TTS 模型 | 客户端使用 `sxh-tts`，服务端映射到底层火山模型 |
| TTS 音色 | OpenAI 六个标准名称映射到渠道默认音色，或直传火山 speaker ID |
| TTS 格式 | MP3、Opus、PCM |
| TTS SSE | 不支持 |
| ASR 模型 | 客户端使用 `sxh-asr`，服务端映射到底层火山模型 |
| ASR 输入 | WAV、MP3、OGG、Opus，最大 100MB、最长 2 小时 |
| ASR 输出 | JSON、纯文本、Verbose JSON |
| 字幕格式 | 不支持 SRT、VTT |
| ASR 实时流 | 不支持 |

OpenAI SDK 只负责构造兼容请求，实际请求会发送到深效科技服务，不会发送到 OpenAI。客户端必须显式配置本文给出的 Base URL 和模型名称。

## 7. 参考资料

- [OpenAI Audio API 参考](https://platform.openai.com/docs/api-reference/audio)
- [火山 Seed-TTS 2.0 V3 HTTP Chunked 接口](https://www.volcengine.com/docs/6561/2228192?lang=zh)
- [火山大模型录音文件极速版 ASR 接口](https://www.volcengine.com/docs/6561/1631584?lang=zh)
