package volcengine

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
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

func newASRMultipartContext(t *testing.T, filename string, audio []byte) *gin.Context {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = part.Write(audio)
	require.NoError(t, err)
	require.NoError(t, writer.WriteField("model", channelconstant.ModelDoubaoSeedASRFlash))
	require.NoError(t, writer.Close())

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body.Bytes()))
	context.Request.Header.Set("Content-Type", writer.FormDataContentType())
	return context
}

func TestBuildVolcASRFlashRequestUsesValidatedFormatAndFixedModel(t *testing.T) {
	context := newASRMultipartContext(t, "sample.opus", []byte("audio-data"))
	requestBody, err := buildVolcASRFlashRequestBody(context, dto.AudioRequest{LocalAudioFormat: "ogg"}, nil)
	require.NoError(t, err)
	payload, err := io.ReadAll(requestBody)
	require.NoError(t, err)
	require.NoError(t, requestBody.Close())
	assert.Equal(t, int64(len(payload)), requestBody.ContentLength())

	var request volcASRFlashRequest
	require.NoError(t, common.Unmarshal(payload, &request))
	assert.Equal(t, "ogg", request.Audio.Format)
	assert.Equal(t, "bigmodel", request.Request.ModelName)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("audio-data")), request.Audio.Data)
	require.NotNil(t, request.Request.ShowUtterances)
	assert.True(t, *request.Request.ShowUtterances)
}

func TestBuildVolcASRFlashRequestMapsSpeechOptionsAndChannelHotwordConfig(t *testing.T) {
	disabled := false
	enabled := true
	forceSegmentAfterMS := 800
	context := newASRMultipartContext(t, "sample.mp3", []byte("audio-data"))
	requestBody, err := buildVolcASRFlashRequestBody(context, dto.AudioRequest{
		LocalAudioFormat: "mp3",
		SpeechOptions: &dto.SpeechOptions{
			TextNormalization:   &disabled,
			Punctuation:         &disabled,
			SemanticSmoothing:   &disabled,
			SensitiveWordFilter: &disabled,
			VADSegmentation:     &enabled,
			SpeakerDiarization:  &enabled,
			ChannelMode:         "separate",
			ForceSegmentAfterMS: &forceSegmentAfterMS,
			Hotwords:            []string{"专有名词"},
			Replacements:        map[string]string{"旧词": "新词"},
			Context:             &dto.SpeechOptionsContext{Texts: []string{"业务背景"}},
		},
	}, &dto.VolcSpeechConfig{ASRHotwordTableID: "platform-table-id"})
	require.NoError(t, err)
	payload, err := io.ReadAll(requestBody)
	require.NoError(t, err)
	require.NoError(t, requestBody.Close())

	var request volcASRFlashRequest
	require.NoError(t, common.Unmarshal(payload, &request))
	require.NotNil(t, request.Request.EnableITN)
	assert.False(t, *request.Request.EnableITN)
	require.NotNil(t, request.Request.EnablePunc)
	assert.False(t, *request.Request.EnablePunc)
	require.NotNil(t, request.Request.EnableDDC)
	assert.False(t, *request.Request.EnableDDC)
	require.NotNil(t, request.Request.EnableSpeakerInfo)
	assert.True(t, *request.Request.EnableSpeakerInfo)
	require.NotNil(t, request.Request.EnableChannelSplit)
	assert.True(t, *request.Request.EnableChannelSplit)
	require.NotNil(t, request.Audio.Channel)
	assert.Equal(t, 2, *request.Audio.Channel)
	assert.Equal(t, 800, *request.Request.EndWindowSize)
	assert.Equal(t, 3000, *request.Request.VADSegmentDuration)
	assert.JSONEq(t, `{"system_reserved_filter":false}`, request.Request.SensitiveWordsFilter)
	require.NotNil(t, request.Request.Corpus)
	assert.Equal(t, "platform-table-id", request.Request.Corpus.BoostingTableID)

	var contextPayload struct {
		Hotwords     []map[string]string `json:"hotwords"`
		CorrectWords map[string]string   `json:"correct_words"`
		ContextType  string              `json:"context_type"`
		ContextData  []map[string]string `json:"context_data"`
	}
	require.NoError(t, common.Unmarshal([]byte(request.Request.Corpus.Context), &contextPayload))
	assert.Equal(t, []map[string]string{{"word": "专有名词"}}, contextPayload.Hotwords)
	assert.Equal(t, map[string]string{"旧词": "新词"}, contextPayload.CorrectWords)
	assert.Equal(t, "dialog_ctx", contextPayload.ContextType)
	assert.Equal(t, []map[string]string{{"text": "业务背景"}}, contextPayload.ContextData)
}

func TestVolcASRAdditionChannelPreservesZero(t *testing.T) {
	channel := volcASRAdditionChannel([]byte(`0`))
	require.NotNil(t, channel)
	assert.Equal(t, 0, *channel)
}

func TestVolcASRMappedAliasUsesUpstreamModelForAdapterRouting(t *testing.T) {
	context := newASRMultipartContext(t, "sample.opus", []byte("audio-data"))
	info := &relaycommon.RelayInfo{
		OriginModelName: "customer-defined-asr-alias",
		RelayMode:       relayconstant.RelayModeAudioTranscription,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "new-console-key",
			UpstreamModelName: channelconstant.ModelDoubaoSeedASRFlash,
		},
	}

	body, err := (&Adaptor{}).ConvertAudioRequest(context, info, dto.AudioRequest{
		Model:                channelconstant.ModelDoubaoSeedASRFlash,
		ResponseFormat:       "json",
		LocalAudioFormat:     "ogg",
		LocalAudioDurationMS: 1000,
	})
	require.NoError(t, err)
	require.NotNil(t, body)
	require.NotNil(t, info.VolcSpeechAudit)
	assert.Equal(t, volcASRFlashResourceID, info.VolcSpeechAudit.ResourceID)

	requestURL, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, volcASRFlashURL, requestURL)

	headers := http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(context, &headers, info))
	assert.Equal(t, "new-console-key", headers.Get("X-Api-Key"))
	assert.Equal(t, volcASRFlashResourceID, headers.Get("X-Api-Resource-Id"))
}

func TestVolcASRFlashStreamingBodyUsesExactContentLength(t *testing.T) {
	context := newASRMultipartContext(t, "sample.wav", []byte("audio-data"))
	requestBody, err := buildVolcASRFlashRequestBody(context, dto.AudioRequest{LocalAudioFormat: "wav"}, nil)
	require.NoError(t, err)

	var receivedPayload []byte
	var receivedContentLength int64
	var receivedTransferEncoding []string
	var readErr error
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedContentLength = request.ContentLength
		receivedTransferEncoding = append([]string(nil), request.TransferEncoding...)
		receivedPayload, readErr = io.ReadAll(request.Body)
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{}`))
	}))
	defer server.Close()

	info := &relaycommon.RelayInfo{
		OriginModelName: channelconstant.ModelDoubaoSeedASRFlash,
		ChannelMeta:     &relaycommon.ChannelMeta{ApiKey: "new-console-key"},
	}
	response, err := doVolcSpeechRequestURL(&Adaptor{}, context, info, requestBody, server.URL)
	require.NoError(t, err)
	defer response.Body.Close()
	require.NoError(t, readErr)

	assert.Equal(t, int64(len(receivedPayload)), receivedContentLength)
	assert.Equal(t, requestBody.ContentLength(), receivedContentLength)
	assert.Empty(t, receivedTransferEncoding)
	var request volcASRFlashRequest
	require.NoError(t, common.Unmarshal(receivedPayload, &request))
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("audio-data")), request.Audio.Data)
}

func TestVolcASRFlashResponseUsesActualDurationAndVerboseSegments(t *testing.T) {
	confidence := 0.93
	body, err := common.Marshal(volcASRFlashResponse{
		AudioInfo: &volcASRAudioInfo{Duration: 2499},
		Result: &volcASRResult{
			Text: "关闭透传。",
			Utterances: []volcASRUtterance{
				{
					StartTime: 450,
					EndTime:   1530,
					Text:      "关闭透传。",
					Additions: &volcASRResultAdditions{
						Speaker:   []byte(`"speaker-1"`),
						ChannelID: []byte(`2`),
					},
					Words: []volcASRWord{
						{StartTime: 450, EndTime: 770, Text: "关", Confidence: &confidence},
					},
				},
			},
		},
	})
	require.NoError(t, err)
	context, recorder := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{
		OriginModelName: channelconstant.ModelDoubaoSeedASRFlash,
		Request:         &dto.AudioRequest{LocalAudioDurationMS: 60000},
		VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{ResourceID: volcASRFlashResourceID, Protocol: volcASRFlashProtocol},
	}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"X-Api-Status-Code": []string{"20000000"},
			"X-Tt-Logid":        []string{"asr-log-id"},
		},
		Body: ioNopCloser(body),
	}

	usageValue, apiErr := handleVolcASRFlashResponse(context, response, info, "verbose_json")
	require.Nil(t, apiErr)
	usage := usageValue.(*dto.Usage)
	assert.Equal(t, 42, usage.PromptTokens)
	assert.Equal(t, int64(2499), info.VolcSpeechAudit.AudioDurationMS)
	assert.Equal(t, "asr-log-id", recorder.Header().Get("X-Volc-Logid"))

	var verbose dto.WhisperVerboseJSONResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &verbose))
	assert.Equal(t, "关闭透传。", verbose.Text)
	assert.InDelta(t, 2.499, verbose.Duration, 0.0001)
	require.Len(t, verbose.Segments, 1)
	assert.InDelta(t, 0.45, verbose.Segments[0].Start, 0.0001)
	assert.InDelta(t, 1.53, verbose.Segments[0].End, 0.0001)
	assert.Empty(t, verbose.Words)
}

func TestVolcASRFlashVerboseJSONReturnsRequestedWordAndSegmentTimestamps(t *testing.T) {
	confidence := 0.91
	body, err := common.Marshal(volcASRFlashResponse{
		AudioInfo: &volcASRAudioInfo{Duration: 2499},
		Result: &volcASRResult{
			Text: "关闭透传。",
			Utterances: []volcASRUtterance{
				{
					StartTime: 450,
					EndTime:   1530,
					Text:      "关闭透传。",
					Additions: &volcASRResultAdditions{
						Speaker:   []byte(`"speaker-1"`),
						ChannelID: []byte(`2`),
					},
					Words: []volcASRWord{
						{StartTime: 450, EndTime: 770, Text: "关", Confidence: &confidence},
						{StartTime: 770, EndTime: 970, Text: "闭"},
					},
				},
			},
		},
	})
	require.NoError(t, err)
	context, recorder := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{
		OriginModelName: channelconstant.ModelDoubaoSeedASRFlash,
		Request: &dto.AudioRequest{
			LocalAudioDurationMS: 60000,
			TimestampGranularities: []string{
				"segment",
				"word",
			},
		},
		VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{
			ResourceID:             volcASRFlashResourceID,
			Protocol:               volcASRFlashProtocol,
			TimestampGranularities: []string{"segment", "word"},
		},
	}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Api-Status-Code": []string{"20000000"}},
		Body:       ioNopCloser(body),
	}

	usageValue, apiErr := handleVolcASRFlashResponse(context, response, info, "verbose_json")
	require.Nil(t, apiErr)
	assert.Equal(t, 42, usageValue.(*dto.Usage).PromptTokens)

	var verbose dto.WhisperVerboseJSONResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &verbose))
	require.Len(t, verbose.Segments, 1)
	require.Len(t, verbose.Words, 2)
	assert.Equal(t, "speaker-1", verbose.Segments[0].Speaker)
	require.NotNil(t, verbose.Segments[0].Channel)
	assert.Equal(t, 2, *verbose.Segments[0].Channel)
	assert.Equal(t, "关", verbose.Words[0].Word)
	assert.InDelta(t, 0.45, verbose.Words[0].Start, 0.0001)
	assert.InDelta(t, 0.77, verbose.Words[0].End, 0.0001)
	require.NotNil(t, verbose.Words[0].Confidence)
	assert.InDelta(t, confidence, *verbose.Words[0].Confidence, 0.0001)
	assert.Equal(t, "speaker-1", verbose.Words[0].Speaker)
	require.NotNil(t, verbose.Words[0].Channel)
	assert.Equal(t, 2, *verbose.Words[0].Channel)
	assert.Nil(t, verbose.Words[1].Confidence)
	assert.Equal(t, "speaker-1", verbose.Words[1].Speaker)
	assert.Equal(t, 1, info.VolcSpeechAudit.SubtitleSentenceCount)
	assert.Equal(t, 2, info.VolcSpeechAudit.SubtitleWordCount)
}

func TestVolcASRFlashVerboseJSONCanReturnOnlyWords(t *testing.T) {
	body, err := common.Marshal(volcASRFlashResponse{
		AudioInfo: &volcASRAudioInfo{Duration: 1000},
		Result: &volcASRResult{
			Text: "测试",
			Utterances: []volcASRUtterance{{
				StartTime: 0,
				EndTime:   1000,
				Text:      "测试",
				Words:     []volcASRWord{{StartTime: 0, EndTime: 500, Text: "测"}},
			}},
		},
	})
	require.NoError(t, err)
	context, recorder := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{
		Request: &dto.AudioRequest{
			TimestampGranularities: []string{"word"},
		},
		VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{},
	}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Api-Status-Code": []string{"20000000"}},
		Body:       ioNopCloser(body),
	}

	_, apiErr := handleVolcASRFlashResponse(context, response, info, "verbose_json")
	require.Nil(t, apiErr)
	var verbose dto.WhisperVerboseJSONResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &verbose))
	assert.Empty(t, verbose.Segments)
	require.Len(t, verbose.Words, 1)
}

func TestVolcASRFlashVerboseJSONPreservesProviderWordUnits(t *testing.T) {
	body, err := common.Marshal(volcASRFlashResponse{
		AudioInfo: &volcASRAudioInfo{Duration: 1800},
		Result: &volcASRResult{
			Text: "2026年，好。",
			Utterances: []volcASRUtterance{{
				StartTime: 100,
				EndTime:   1800,
				Text:      "2026年，好。",
				Words: []volcASRWord{
					{StartTime: 100, EndTime: 900, Text: "2026年"},
					{StartTime: 900, EndTime: 1100, Text: "，"},
					{StartTime: -1, EndTime: -1, Text: " "},
					{StartTime: 1100, EndTime: 1800, Text: "好。"},
				},
			}},
		},
	})
	require.NoError(t, err)
	context, recorder := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{
		Request: &dto.AudioRequest{
			TimestampGranularities: []string{"word"},
		},
		VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{},
	}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Api-Status-Code": []string{"20000000"}},
		Body:       ioNopCloser(body),
	}

	_, apiErr := handleVolcASRFlashResponse(context, response, info, "verbose_json")
	require.Nil(t, apiErr)
	var verbose dto.WhisperVerboseJSONResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &verbose))
	require.Len(t, verbose.Words, 4)
	assert.Equal(t, "2026年", verbose.Words[0].Word)
	assert.Equal(t, "，", verbose.Words[1].Word)
	assert.Equal(t, " ", verbose.Words[2].Word)
	assert.Equal(t, "好。", verbose.Words[3].Word)
	assert.InDelta(t, 0.1, verbose.Words[0].Start, 0.0001)
	assert.InDelta(t, -0.001, verbose.Words[2].Start, 0.0001)
	assert.InDelta(t, 1.8, verbose.Words[3].End, 0.0001)
}

func TestVolcASRFlashVerboseJSONAllowsMissingProviderWords(t *testing.T) {
	body, err := common.Marshal(volcASRFlashResponse{
		AudioInfo: &volcASRAudioInfo{Duration: 1000},
		Result: &volcASRResult{
			Text: "测试",
			Utterances: []volcASRUtterance{{
				StartTime: 0,
				EndTime:   1000,
				Text:      "测试",
			}},
		},
	})
	require.NoError(t, err)
	context, recorder := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{
		Request: &dto.AudioRequest{
			TimestampGranularities: []string{"segment", "word"},
		},
		VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{},
	}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Api-Status-Code": []string{"20000000"}},
		Body:       ioNopCloser(body),
	}

	_, apiErr := handleVolcASRFlashResponse(context, response, info, "verbose_json")
	require.Nil(t, apiErr)
	var verbose dto.WhisperVerboseJSONResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &verbose))
	require.Len(t, verbose.Segments, 1)
	assert.Empty(t, verbose.Words)
}

func TestVolcASRFlashGeneratesSRTAndVTTFromUtterances(t *testing.T) {
	body, err := common.Marshal(volcASRFlashResponse{
		AudioInfo: &volcASRAudioInfo{Duration: 3500},
		Result: &volcASRResult{
			Text: "第一句。第二句。",
			Utterances: []volcASRUtterance{
				{StartTime: 450, EndTime: 1530, Text: "第一句。"},
				{StartTime: 2000, EndTime: 3500, Text: "第二句。"},
			},
		},
	})
	require.NoError(t, err)

	tests := []struct {
		format      string
		contentType string
		expected    string
	}{
		{
			format:      "srt",
			contentType: "application/x-subrip; charset=utf-8",
			expected: "1\n00:00:00,450 --> 00:00:01,530\n第一句。\n\n" +
				"2\n00:00:02,000 --> 00:00:03,500\n第二句。\n\n",
		},
		{
			format:      "vtt",
			contentType: "text/vtt; charset=utf-8",
			expected: "WEBVTT\n\n00:00:00.450 --> 00:00:01.530\n第一句。\n\n" +
				"00:00:02.000 --> 00:00:03.500\n第二句。\n\n",
		},
	}
	for _, test := range tests {
		t.Run(test.format, func(t *testing.T) {
			context, recorder := newVolcSpeechTestContext()
			info := &relaycommon.RelayInfo{
				Request:         &dto.AudioRequest{},
				VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{},
			}
			response := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"X-Api-Status-Code": []string{"20000000"}},
				Body:       ioNopCloser(body),
			}

			_, apiErr := handleVolcASRFlashResponse(context, response, info, test.format)
			require.Nil(t, apiErr)
			assert.Equal(t, test.contentType, recorder.Header().Get("Content-Type"))
			assert.Equal(t, test.expected, recorder.Body.String())
		})
	}
}

func TestVolcASRFlashEmptyTranscriptStillBillsLocalDurationFallback(t *testing.T) {
	body, err := common.Marshal(volcASRFlashResponse{Result: &volcASRResult{Text: ""}})
	require.NoError(t, err)
	context, recorder := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{
		OriginModelName: channelconstant.ModelDoubaoSeedASRFlash,
		Request:         &dto.AudioRequest{LocalAudioDurationMS: 60000},
		VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{ResourceID: volcASRFlashResourceID, Protocol: volcASRFlashProtocol},
	}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Api-Status-Code": []string{"20000003"}},
		Body:       ioNopCloser(body),
	}

	usageValue, apiErr := handleVolcASRFlashResponse(context, response, info, "json")
	require.Nil(t, apiErr)
	assert.Equal(t, 1000, usageValue.(*dto.Usage).PromptTokens)
	assert.JSONEq(t, `{"text":""}`, recorder.Body.String())
}

func TestVolcASRFlashTextResponseAndProviderFailure(t *testing.T) {
	body, err := common.Marshal(volcASRFlashResponse{
		AudioInfo: &volcASRAudioInfo{Duration: 60000},
		Result:    &volcASRResult{Text: "转写文本"},
	})
	require.NoError(t, err)
	context, recorder := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{
		Request:         &dto.AudioRequest{},
		VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{ResourceID: volcASRFlashResourceID, Protocol: volcASRFlashProtocol},
	}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Api-Status-Code": []string{"20000000"}},
		Body:       ioNopCloser(body),
	}
	_, apiErr := handleVolcASRFlashResponse(context, response, info, "text")
	require.Nil(t, apiErr)
	assert.Equal(t, "转写文本", recorder.Body.String())

	failed := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"X-Api-Status-Code": []string{"55000031"},
			"X-Api-Message":     []string{"server busy: private transcript text"},
			"X-Tt-Logid":        []string{"asr-log-id"},
		},
		Body: ioNopCloser([]byte(`{}`)),
	}
	_, apiErr = handleVolcASRFlashResponse(context, failed, info, "json")
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.Contains(t, apiErr.Error(), "55000031")
	assert.Contains(t, apiErr.Error(), "asr-log-id")
	assert.NotContains(t, apiErr.Error(), "private transcript text")

	malformedStatus := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"X-Api-Status-Code": []string{"55000031 private transcript text"},
			"X-Api-Message":     []string{"private transcript text"},
		},
		Body: ioNopCloser([]byte(`{}`)),
	}
	_, apiErr = handleVolcASRFlashResponse(context, malformedStatus, info, "json")
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.Contains(t, apiErr.Error(), "invalid X-Api-Status-Code")
	assert.NotContains(t, apiErr.Error(), "private transcript text")
}

func TestVolcASRFlashMalformedSuccessResponseRemainsRetryable(t *testing.T) {
	tests := []struct {
		name   string
		header http.Header
		body   []byte
	}{
		{
			name:   "缺少状态响应头",
			header: http.Header{},
			body:   []byte(`{"audio_info":{"duration":1000}}`),
		},
		{
			name:   "响应 JSON 损坏",
			header: http.Header{"X-Api-Status-Code": []string{"20000000"}},
			body:   []byte(`{"audio_info":`),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context, _ := newVolcSpeechTestContext()
			info := &relaycommon.RelayInfo{
				Request:         &dto.AudioRequest{LocalAudioDurationMS: 1000},
				VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{ResourceID: volcASRFlashResourceID, Protocol: volcASRFlashProtocol},
			}
			response := &http.Response{
				StatusCode: http.StatusOK,
				Header:     test.header,
				Body:       ioNopCloser(test.body),
			}

			_, apiErr := handleVolcASRFlashResponse(context, response, info, "json")
			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeBadResponse, apiErr.GetErrorCode())
			assert.False(t, types.IsSkipRetryError(apiErr))
			assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
		})
	}
}

func ioNopCloser(data []byte) *readCloser {
	return &readCloser{Reader: bytes.NewReader(data)}
}

type readCloser struct {
	*bytes.Reader
}

func (r *readCloser) Close() error {
	return nil
}
