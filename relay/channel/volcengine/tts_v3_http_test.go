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
