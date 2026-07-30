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
	requestBody, err := buildVolcASRFlashRequestBody(context, dto.AudioRequest{LocalAudioFormat: "ogg"})
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
	assert.True(t, request.Request.ShowUtterances)
}

func TestVolcASRFlashStreamingBodyUsesExactContentLength(t *testing.T) {
	context := newASRMultipartContext(t, "sample.wav", []byte("audio-data"))
	requestBody, err := buildVolcASRFlashRequestBody(context, dto.AudioRequest{LocalAudioFormat: "wav"})
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
	body, err := common.Marshal(volcASRFlashResponse{
		AudioInfo: &volcASRAudioInfo{Duration: 2499},
		Result: &volcASRResult{
			Text: "关闭透传。",
			Utterances: []volcASRUtterance{
				{StartTime: 450, EndTime: 1530, Text: "关闭透传。"},
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
			"X-Api-Message":     []string{"server busy"},
		},
		Body: ioNopCloser([]byte(`{}`)),
	}
	_, apiErr = handleVolcASRFlashResponse(context, failed, info, "json")
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
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
