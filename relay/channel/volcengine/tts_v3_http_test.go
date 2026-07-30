package volcengine

import (
	"bytes"
	"encoding/binary"
	"io"
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

func buildVolcV3TestFrame(t *testing.T, msgType MsgType, event EventType, payload []byte) []byte {
	t.Helper()
	message, err := NewMessage(msgType, MsgTypeFlagWithEvent)
	require.NoError(t, err)
	message.EventType = event
	message.SessionID = "session-id"
	message.Payload = payload
	frame, err := message.Marshal()
	require.NoError(t, err)
	return frame
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

func TestVolcTTSV3ChunkedStreamsMultipleFramesAndUsesProviderUsage(t *testing.T) {
	audioOne := []byte("audio-one")
	audioTwo := []byte("audio-two")
	finishPayload, err := common.Marshal(volcTTSV3Result{
		Code:  20000000,
		Usage: &volcTTSV3Usage{TextWords: 17},
	})
	require.NoError(t, err)
	stream := bytes.Join([][]byte{
		buildVolcV3TestFrame(t, MsgTypeAudioOnlyServer, EventType_TTSResponse, audioOne),
		buildVolcV3TestFrame(t, MsgTypeAudioOnlyServer, EventType_TTSResponse, audioTwo),
		buildVolcV3TestFrame(t, MsgTypeFullServerResponse, EventType_SessionFinished, finishPayload),
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
	finishPayload, err := common.Marshal(volcTTSV3Result{Code: 20000000})
	require.NoError(t, err)
	stream := bytes.Join([][]byte{
		buildVolcV3TestFrame(t, MsgTypeAudioOnlyServer, EventType_TTSResponse, []byte("audio")),
		buildVolcV3TestFrame(t, MsgTypeFullServerResponse, EventType_SessionFinished, finishPayload),
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
	stream := buildVolcV3TestFrame(t, MsgTypeAudioOnlyServer, EventType_TTSResponse, []byte("partial-audio"))
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
	stream := buildVolcV3TestFrame(t, MsgTypeAudioOnlyServer, EventType_TTSResponse, []byte("partial-audio"))
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

func TestVolcTTSV3RejectsOversizedFrameBeforeReadingPayload(t *testing.T) {
	frame := []byte{
		0x11,
		byte(MsgTypeAudioOnlyServer << 4),
		byte(SerializationJSON << 4),
		0,
	}
	size := make([]byte, 4)
	binary.BigEndian.PutUint32(size, uint32(volcTTSMaxFrameBytes+1))
	frame = append(frame, size...)

	_, err := readVolcTTSV3Frame(bytes.NewReader(frame))
	require.ErrorContains(t, err, "frame exceeds")
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
