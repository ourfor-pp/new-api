package volcengine

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	channelconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildVolcTTSJSONLine(t *testing.T, result volcTTSV3Result) []byte {
	t.Helper()
	payload, err := common.Marshal(result)
	require.NoError(t, err)
	return append(payload, '\n')
}

func parseVolcTTSSSEEvents(t *testing.T, body string) []map[string]interface{} {
	t.Helper()
	parts := strings.Split(body, "\n\n")
	events := make([]map[string]interface{}, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		require.True(t, strings.HasPrefix(part, "data: "))
		var event map[string]interface{}
		require.NoError(t, common.Unmarshal([]byte(strings.TrimPrefix(part, "data: ")), &event))
		events = append(events, event)
	}
	return events
}

func newVolcSpeechTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	return context, recorder
}

type partialWriteRecorder struct {
	*httptest.ResponseRecorder
	maxBytes int
}

func (r *partialWriteRecorder) Write(payload []byte) (int, error) {
	if len(payload) > r.maxBytes {
		payload = payload[:r.maxBytes]
	}
	written, _ := r.ResponseRecorder.Write(payload)
	return written, io.ErrClosedPipe
}

func TestBuildVolcTTSV3RequestMapsOnlyStandardVoices(t *testing.T) {
	speed := 1.25
	request, encoding, err := buildVolcTTSV3Request(dto.AudioRequest{
		Input:          "测试文本",
		Voice:          "alloy",
		ResponseFormat: "opus",
		Speed:          &speed,
		Metadata:       []byte(`{"speaker":"forbidden","resource_id":"forbidden"}`),
	}, &dto.VolcSpeechConfig{DefaultTTSSpeaker: "zh_female_seed_2"})
	require.NoError(t, err)
	assert.Equal(t, "zh_female_seed_2", request.ReqParams.Speaker)
	assert.Equal(t, "ogg_opus", encoding)
	require.NotNil(t, request.ReqParams.AudioParams.SpeechRate)
	assert.Equal(t, 25, *request.ReqParams.AudioParams.SpeechRate)
	assert.Nil(t, request.ReqParams.AudioParams.EnableSubtitle)

	direct, _, err := buildVolcTTSV3Request(dto.AudioRequest{
		Input:          "测试文本",
		Voice:          "S_seed_special",
		ResponseFormat: "mp3",
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, "S_seed_special", direct.ReqParams.Speaker)

	_, _, err = buildVolcTTSV3Request(dto.AudioRequest{
		Input: "测试文本",
		Voice: "alloy",
	}, nil)
	require.ErrorContains(t, err, "default_tts_speaker")
	channelConfigErr := types.NewError(err, types.ErrorCodeConvertRequestFailed)
	assert.True(t, types.IsChannelError(channelConfigErr))
}

func TestBuildVolcTTSV3RequestEnablesSubtitleOnlyWhenRequested(t *testing.T) {
	request, _, err := buildVolcTTSV3Request(dto.AudioRequest{
		Input:                  "测试文本",
		Voice:                  "speaker",
		ResponseFormat:         "mp3",
		StreamFormat:           "sse",
		TimestampGranularities: []string{"segment", "word"},
		SubtitleFormats:        []string{"json", "srt"},
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, request.ReqParams.AudioParams.EnableSubtitle)
	assert.True(t, *request.ReqParams.AudioParams.EnableSubtitle)
}

func TestVolcTTSV3ValidationRejectsUnsupportedFormatAndSpeed(t *testing.T) {
	_, _, err := buildVolcTTSV3Request(dto.AudioRequest{Input: "x", Voice: "speaker", ResponseFormat: "wav"}, nil)
	require.ErrorContains(t, err, "unsupported response_format")

	speed := 2.01
	_, _, err = buildVolcTTSV3Request(dto.AudioRequest{Input: "x", Voice: "speaker", ResponseFormat: "mp3", Speed: &speed}, nil)
	require.ErrorContains(t, err, "speed must be between")
}

func TestVolcTTSV3ChunkedStreamsMultipleJSONLinesAndUsesProviderUsage(t *testing.T) {
	audioOne := []byte("audio-one")
	audioTwo := []byte("audio-two")
	stream := bytes.Join([][]byte{
		buildVolcTTSJSONLine(t, volcTTSV3Result{Code: 0, Data: base64.StdEncoding.EncodeToString(audioOne)}),
		buildVolcTTSJSONLine(t, volcTTSV3Result{Code: 0, Data: base64.StdEncoding.EncodeToString(audioTwo)}),
		buildVolcTTSJSONLine(t, volcTTSV3Result{Code: 0}),
		buildVolcTTSJSONLine(t, volcTTSV3Result{Code: 20000000, Usage: &volcTTSV3Usage{TextWords: 17}}),
	}, nil)

	var receivedHeader http.Header
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedHeader = request.Header.Clone()
		writer.Header().Set("X-Tt-Logid", "tts-log-id")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(stream)
	}))
	defer server.Close()

	context, recorder := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{
		OriginModelName: channelconstant.ModelDoubaoSeedTTS20,
		RelayMode:       relayconstant.RelayModeAudioSpeech,
		Request:         &dto.AudioRequest{Input: "12345678901234567890"},
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey: "new-console-key",
		},
		VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{ResourceID: volcTTSResourceID, Protocol: volcTTSProtocol},
	}
	requestBody, err := common.Marshal(volcTTSV3Request{})
	require.NoError(t, err)
	response, err := doVolcSpeechRequestURL(&Adaptor{}, context, info, bytes.NewReader(requestBody), server.URL)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)

	usageValue, apiErr := handleVolcTTSV3Response(context, response, info, "mp3")
	require.Nil(t, apiErr)
	usage := usageValue.(*dto.Usage)
	assert.Equal(t, 17, usage.PromptTokens)
	assert.Equal(t, append(audioOne, audioTwo...), recorder.Body.Bytes())
	assert.Equal(t, "audio/mpeg", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "tts-log-id", recorder.Header().Get("X-Volc-Logid"))
	assert.Equal(t, "new-console-key", receivedHeader.Get("X-Api-Key"))
	assert.Equal(t, volcTTSResourceID, receivedHeader.Get("X-Api-Resource-Id"))
	assert.Equal(t, "*", receivedHeader.Get("X-Control-Require-Usage-Tokens-Return"))
}

func TestVolcTTSV3SuccessfulFinishWithoutUsageFallsBackToUnicodeCharacters(t *testing.T) {
	stream := bytes.Join([][]byte{
		buildVolcTTSJSONLine(t, volcTTSV3Result{Code: 0, Data: base64.StdEncoding.EncodeToString([]byte("audio"))}),
		buildVolcTTSJSONLine(t, volcTTSV3Result{Code: 20000000}),
	}, nil)
	context, _ := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{
		Request:         &dto.AudioRequest{Input: "你好A"},
		OriginModelName: channelconstant.ModelDoubaoSeedTTS20,
		VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{ResourceID: volcTTSResourceID, Protocol: volcTTSProtocol},
	}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(stream))}

	usageValue, apiErr := handleVolcTTSV3Response(context, response, info, "mp3")
	require.Nil(t, apiErr)
	assert.Equal(t, 3, usageValue.(*dto.Usage).PromptTokens)
	assert.Equal(t, "fallback_unicode", info.VolcSpeechAudit.UsageSource)
}

func TestVolcTTSV3SSEStreamsAudioAndFinalSubtitleEvents(t *testing.T) {
	confidenceOne := 0.92
	confidenceTwo := 0.95
	audio := []byte("audio-one")
	stream := bytes.Join([][]byte{
		buildVolcTTSJSONLine(t, volcTTSV3Result{Code: 0, Data: base64.StdEncoding.EncodeToString(audio)}),
		buildVolcTTSJSONLine(t, volcTTSV3Result{
			Code:     0,
			Sentence: &volcTTSV3Sentence{Text: "第一句。"},
		}),
		buildVolcTTSJSONLine(t, volcTTSV3Result{
			Code: 0,
			Sentence: &volcTTSV3Sentence{
				Text: "第一句。",
				Words: []volcTTSV3Word{
					{Word: "第", StartTime: 0.235, EndTime: 0.415, Confidence: &confidenceOne},
					{Word: "一句。", StartTime: 0.415, EndTime: 0.915, Confidence: &confidenceTwo},
				},
			},
		}),
		buildVolcTTSJSONLine(t, volcTTSV3Result{
			Code: 0,
			Sentence: &volcTTSV3Sentence{
				Text: "第二句。",
				Words: []volcTTSV3Word{
					{Word: "第二句。", StartTime: 1.2, EndTime: 2.0},
				},
			},
		}),
		buildVolcTTSJSONLine(t, volcTTSV3Result{Code: 20000000}),
	}, nil)
	context, recorder := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{
		Request: &dto.AudioRequest{
			Input:                  "第一句。第二句。",
			StreamFormat:           "sse",
			TimestampGranularities: []string{"segment", "word"},
			SubtitleFormats:        []string{"json", "srt", "vtt"},
		},
		OriginModelName: channelconstant.ModelDoubaoSeedTTS20,
		VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{
			ResourceID:             volcTTSResourceID,
			Protocol:               volcTTSProtocol,
			TimestampGranularities: []string{"segment", "word"},
			SubtitleFormats:        []string{"json", "srt", "vtt"},
		},
	}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(stream))}

	usageValue, apiErr := handleVolcTTSV3Response(context, response, info, "mp3")
	require.Nil(t, apiErr)
	assert.Equal(t, 8, usageValue.(*dto.Usage).PromptTokens)
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	assert.Equal(t, 2, info.VolcSpeechAudit.SubtitleSentenceCount)
	assert.Equal(t, 3, info.VolcSpeechAudit.SubtitleWordCount)
	assert.Equal(t, "fallback_unicode", info.VolcSpeechAudit.UsageSource)

	events := parseVolcTTSSSEEvents(t, recorder.Body.String())
	require.Len(t, events, 5)
	assert.Equal(t, "speech.audio.delta", events[0]["type"])
	assert.Equal(t, base64.StdEncoding.EncodeToString(audio), events[0]["audio"])
	assert.Equal(t, "sxh.speech.subtitle.delta", events[1]["type"])
	assert.Equal(t, "第一句。", events[1]["text"])
	assert.InDelta(t, 0.235, events[1]["startTime"], 0.0001)
	assert.InDelta(t, 0.915, events[1]["endTime"], 0.0001)
	words, ok := events[1]["words"].([]interface{})
	require.True(t, ok)
	require.Len(t, words, 2)
	assert.Equal(t, "sxh.speech.subtitle.delta", events[2]["type"])
	assert.Equal(t, "第二句。", events[2]["text"])
	assert.Equal(t, "sxh.speech.subtitle.done", events[3]["type"])
	assert.Equal(t, true, events[3]["available"])
	formats, ok := events[3]["formats"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "1\n00:00:00,235 --> 00:00:00,915\n第一句。\n\n2\n00:00:01,200 --> 00:00:02,000\n第二句。\n\n", formats["srt"])
	assert.Equal(t, "WEBVTT\n\n00:00:00.235 --> 00:00:00.915\n第一句。\n\n00:00:01.200 --> 00:00:02.000\n第二句。\n\n", formats["vtt"])
	assert.Equal(t, "speech.audio.done", events[4]["type"])
}

func TestVolcTTSV3SSEWithoutSubtitleKeepsStandardAudioEvents(t *testing.T) {
	audioOne := []byte("audio-one")
	audioTwo := []byte("audio-two")
	stream := bytes.Join([][]byte{
		buildVolcTTSJSONLine(t, volcTTSV3Result{Code: 0, Data: base64.StdEncoding.EncodeToString(audioOne)}),
		buildVolcTTSJSONLine(t, volcTTSV3Result{Code: 0, Data: base64.StdEncoding.EncodeToString(audioTwo)}),
		buildVolcTTSJSONLine(t, volcTTSV3Result{Code: 20000000, Usage: &volcTTSV3Usage{TextWords: 2}}),
	}, nil)
	context, recorder := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{
		Request:         &dto.AudioRequest{Input: "测试", StreamFormat: "sse"},
		VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{},
	}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(stream))}

	usageValue, apiErr := handleVolcTTSV3Response(context, response, info, "mp3")
	require.Nil(t, apiErr)
	assert.Equal(t, 2, usageValue.(*dto.Usage).PromptTokens)
	events := parseVolcTTSSSEEvents(t, recorder.Body.String())
	require.Len(t, events, 3)
	assert.Equal(t, "speech.audio.delta", events[0]["type"])
	assert.Equal(t, "speech.audio.delta", events[1]["type"])
	assert.Equal(t, "speech.audio.done", events[2]["type"])
	var restoredAudio []byte
	for _, event := range events[:2] {
		chunk, decodeErr := base64.StdEncoding.DecodeString(event["audio"].(string))
		require.NoError(t, decodeErr)
		restoredAudio = append(restoredAudio, chunk...)
	}
	assert.Equal(t, append(audioOne, audioTwo...), restoredAudio)
}

func TestVolcTTSV3SSECompletesWhenSubtitleIsUnavailable(t *testing.T) {
	stream := bytes.Join([][]byte{
		buildVolcTTSJSONLine(t, volcTTSV3Result{Code: 0, Data: base64.StdEncoding.EncodeToString([]byte("audio"))}),
		buildVolcTTSJSONLine(t, volcTTSV3Result{
			Code:     0,
			Sentence: &volcTTSV3Sentence{Text: "测试", Words: nil},
		}),
		buildVolcTTSJSONLine(t, volcTTSV3Result{Code: 20000000}),
	}, nil)
	context, recorder := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{
		Request: &dto.AudioRequest{
			Input:                  "测试",
			StreamFormat:           "sse",
			TimestampGranularities: []string{"word"},
			SubtitleFormats:        []string{"json", "srt"},
		},
		VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{},
	}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(stream))}

	usageValue, apiErr := handleVolcTTSV3Response(context, response, info, "mp3")
	require.Nil(t, apiErr)
	assert.Equal(t, 2, usageValue.(*dto.Usage).PromptTokens)
	events := parseVolcTTSSSEEvents(t, recorder.Body.String())
	require.Len(t, events, 3)
	assert.Equal(t, "speech.audio.delta", events[0]["type"])
	assert.Equal(t, "sxh.speech.subtitle.done", events[1]["type"])
	assert.Equal(t, false, events[1]["available"])
	assert.Equal(t, "speech.audio.done", events[2]["type"])
}

func TestVolcTTSV3SSEInterruptedAfterAudioEmitsErrorAndIsNotRetryable(t *testing.T) {
	stream := buildVolcTTSJSONLine(t, volcTTSV3Result{
		Code: 0,
		Data: base64.StdEncoding.EncodeToString([]byte("partial-audio")),
	})
	context, recorder := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{
		Request:         &dto.AudioRequest{Input: "测试", StreamFormat: "sse"},
		VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{},
	}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(stream))}

	usage, apiErr := handleVolcTTSV3Response(context, response, info, "mp3")
	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.True(t, info.VolcSpeechAudit.PartialFailure)
	events := parseVolcTTSSSEEvents(t, recorder.Body.String())
	require.Len(t, events, 2)
	assert.Equal(t, "speech.audio.delta", events[0]["type"])
	assert.Equal(t, "error", events[1]["type"])
}

func TestVolcTTSV3InterruptedAfterAudioIsPartialAndNotRetryable(t *testing.T) {
	stream := buildVolcTTSJSONLine(t, volcTTSV3Result{
		Code: 0,
		Data: base64.StdEncoding.EncodeToString([]byte("partial-audio")),
	})
	context, recorder := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{
		Request:         &dto.AudioRequest{Input: "测试"},
		OriginModelName: channelconstant.ModelDoubaoSeedTTS20,
		VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{ResourceID: volcTTSResourceID, Protocol: volcTTSProtocol},
	}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(stream))}

	usage, apiErr := handleVolcTTSV3Response(context, response, info, "mp3")
	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.True(t, info.VolcSpeechAudit.PartialFailure)
	assert.Equal(t, 0, info.VolcSpeechAudit.BillingUnits)
	assert.Equal(t, "partial-audio", recorder.Body.String())
}

func TestVolcTTSV3ClientWriteFailureMarksPartialFailure(t *testing.T) {
	context, _ := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{
		ResourceID: volcTTSResourceID,
		Protocol:   volcTTSProtocol,
	}}
	apiErr := volcTTSStreamError(context, info, true, io.ErrClosedPipe, 499)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.True(t, info.VolcSpeechAudit.PartialFailure)
}

func TestVolcTTSV3FirstPartialWriteIsNotRetryable(t *testing.T) {
	stream := buildVolcTTSJSONLine(t, volcTTSV3Result{
		Code: 0,
		Data: base64.StdEncoding.EncodeToString([]byte("partial-audio")),
	})
	recorder := &partialWriteRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		maxBytes:         4,
	}
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	info := &relaycommon.RelayInfo{
		Request:         &dto.AudioRequest{Input: "测试"},
		OriginModelName: channelconstant.ModelDoubaoSeedTTS20,
		VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{ResourceID: volcTTSResourceID, Protocol: volcTTSProtocol},
	}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(stream))}

	usage, apiErr := handleVolcTTSV3Response(context, response, info, "mp3")

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.True(t, info.VolcSpeechAudit.PartialFailure)
	assert.Equal(t, "part", recorder.Body.String())
}

func TestVolcTTSV3RejectsOversizedJSONLineBeforeWritingAudio(t *testing.T) {
	context, recorder := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{
		Request:         &dto.AudioRequest{Input: "测试"},
		OriginModelName: channelconstant.ModelDoubaoSeedTTS20,
		VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{ResourceID: volcTTSResourceID, Protocol: volcTTSProtocol},
	}
	stream := strings.Repeat("A", volcTTSMaxJSONLineBytes+1) + "\n"
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(stream))}

	usage, apiErr := handleVolcTTSV3Response(context, response, info, "mp3")
	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Contains(t, apiErr.Error(), "token too long")
	assert.False(t, types.IsSkipRetryError(apiErr))
	assert.Empty(t, recorder.Body.Bytes())
}

func TestVolcTTSV3ProviderErrorBeforeAudioIsRetryable(t *testing.T) {
	context, recorder := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{
		Request:         &dto.AudioRequest{Input: "测试"},
		OriginModelName: channelconstant.ModelDoubaoSeedTTS20,
		VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{ResourceID: volcTTSResourceID, Protocol: volcTTSProtocol},
	}
	stream := buildVolcTTSJSONLine(t, volcTTSV3Result{Code: 45000030, Message: "requested resource not granted"})
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(stream))}

	usage, apiErr := handleVolcTTSV3Response(context, response, info, "mp3")
	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Contains(t, apiErr.Error(), "45000030")
	assert.False(t, types.IsSkipRetryError(apiErr))
	assert.Empty(t, recorder.Body.Bytes())
}

func TestVolcSpeechModelRoutesDoNotChangeLegacyTTSURL(t *testing.T) {
	adaptor := &Adaptor{}
	ttsURL, err := adaptor.GetRequestURL(&relaycommon.RelayInfo{
		OriginModelName: channelconstant.ModelDoubaoSeedTTS20,
		ChannelMeta:     &relaycommon.ChannelMeta{},
	})
	require.NoError(t, err)
	assert.Equal(t, volcTTSV3URL, ttsURL)

	legacyURL, err := adaptor.GetRequestURL(&relaycommon.RelayInfo{
		OriginModelName: "legacy-tts-model",
		RelayMode:       relayconstant.RelayModeAudioSpeech,
		ChannelMeta:     &relaycommon.ChannelMeta{},
	})
	require.NoError(t, err)
	assert.Equal(t, "wss://openspeech.bytedance.com/api/v1/tts/ws_binary", legacyURL)
}

func TestVolcTTSMappedAliasUsesUpstreamModelForAdapterRouting(t *testing.T) {
	context, _ := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{
		OriginModelName: "customer-defined-tts-alias",
		RelayMode:       relayconstant.RelayModeAudioSpeech,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "new-console-key",
			UpstreamModelName: channelconstant.ModelDoubaoSeedTTS20,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				VolcSpeech: &dto.VolcSpeechConfig{DefaultTTSSpeaker: "zh_female_seed_2"},
			},
		},
	}

	body, err := (&Adaptor{}).ConvertAudioRequest(context, info, dto.AudioRequest{
		Model:          channelconstant.ModelDoubaoSeedTTS20,
		Input:          "测试",
		Voice:          "alloy",
		ResponseFormat: "mp3",
	})
	require.NoError(t, err)
	require.NotNil(t, body)
	require.NotNil(t, info.VolcSpeechAudit)
	assert.Equal(t, volcTTSResourceID, info.VolcSpeechAudit.ResourceID)

	requestURL, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, volcTTSV3URL, requestURL)

	headers := http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(context, &headers, info))
	assert.Equal(t, "new-console-key", headers.Get("X-Api-Key"))
	assert.Equal(t, volcTTSResourceID, headers.Get("X-Api-Resource-Id"))
}
