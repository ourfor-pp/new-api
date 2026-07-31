package helper

import (
	"bytes"
	"encoding/binary"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMappedVolcTTSUsesSpecialValidationWithoutChangingBusinessModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("model_mapping", `{"customer-tts-alias":"`+constant.ModelDoubaoSeedTTS20+`"}`)
	context.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/audio/speech",
		bytes.NewBufferString(`{"model":"customer-tts-alias","input":"测试","voice":"speaker"}`),
	)
	context.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidAudioRequest(context, relayconstant.RelayModeAudioSpeech)
	require.NoError(t, err)
	assert.Equal(t, "customer-tts-alias", request.Model)
	assert.Equal(t, "mp3", request.ResponseFormat)
}

func TestMappedVolcTTSRejectsUnsupportedFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("model_mapping", `{"another-tts-alias":"`+constant.ModelDoubaoSeedTTS20+`"}`)
	context.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/audio/speech",
		bytes.NewBufferString(`{"model":"another-tts-alias","input":"测试","voice":"speaker","response_format":"wav"}`),
	)
	context.Request.Header.Set("Content-Type", "application/json")

	_, err := GetAndValidAudioRequest(context, relayconstant.RelayModeAudioSpeech)
	require.ErrorContains(t, err, "only supports mp3, opus, or pcm")
}

func TestMappedVolcTTSSpeechOptionsPreserveSampleRateAndRejectUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("model_mapping", `{"customer-tts-alias":"`+constant.ModelDoubaoSeedTTS20+`"}`)
	context.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/audio/speech",
		bytes.NewBufferString(`{
			"model":"customer-tts-alias",
			"input":"测试",
			"voice":"speaker",
			"instructions":"请自然地说",
			"speech_options":{
				"sample_rate":16000,
				"context":{"texts":["上一轮对话"]}
			}
		}`),
	)
	context.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidAudioRequest(context, relayconstant.RelayModeAudioSpeech)
	require.NoError(t, err)
	require.NotNil(t, request.SpeechOptions)
	require.NotNil(t, request.SpeechOptions.SampleRate)
	assert.Equal(t, 16000, *request.SpeechOptions.SampleRate)
	require.NotNil(t, request.SpeechOptions.Context)
	assert.Equal(t, []string{"上一轮对话"}, request.SpeechOptions.Context.Texts)

	context, _ = gin.CreateTestContext(httptest.NewRecorder())
	context.Set("model_mapping", `{"customer-tts-alias":"`+constant.ModelDoubaoSeedTTS20+`"}`)
	context.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/audio/speech",
		bytes.NewBufferString(`{
			"model":"customer-tts-alias",
			"input":"测试",
			"voice":"speaker",
			"speech_options":{"provider_payload":{"resource_id":"forbidden"}}
		}`),
	)
	context.Request.Header.Set("Content-Type", "application/json")

	_, err = GetAndValidAudioRequest(context, relayconstant.RelayModeAudioSpeech)
	require.ErrorContains(t, err, "unknown speech_options field: provider_payload")
}

func TestMappedVolcTTSRejectsASROnlyAndUnverifiedContextOptions(t *testing.T) {
	tests := []struct {
		name    string
		options string
		message string
	}{
		{
			name:    "ASR option",
			options: `{"punctuation":false}`,
			message: "speech_options.punctuation is not supported",
		},
		{
			name:    "image context",
			options: `{"context":{"images":[{"image_url":"https://example.com/image.png"}]}}`,
			message: "context.images is not supported",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Set("model_mapping", `{"customer-tts-alias":"`+constant.ModelDoubaoSeedTTS20+`"}`)
			context.Request = httptest.NewRequest(
				http.MethodPost,
				"/v1/audio/speech",
				bytes.NewBufferString(`{
					"model":"customer-tts-alias",
					"input":"测试",
					"voice":"speaker",
					"speech_options":`+test.options+`
				}`),
			)
			context.Request.Header.Set("Content-Type", "application/json")

			_, err := GetAndValidAudioRequest(context, relayconstant.RelayModeAudioSpeech)
			require.ErrorContains(t, err, test.message)
		})
	}
}

func TestNonVolcAudioModelRejectsSpeechOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/audio/speech",
		bytes.NewBufferString(`{
			"model":"tts-1",
			"input":"test",
			"voice":"alloy",
			"speech_options":{"sample_rate":16000}
		}`),
	)
	context.Request.Header.Set("Content-Type", "application/json")

	_, err := GetAndValidAudioRequest(context, relayconstant.RelayModeAudioSpeech)
	require.ErrorContains(t, err, "speech_options is not supported for model tts-1")
}

func TestMappedVolcTTSSubtitleOptionsRequireSSEAndApplyDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("model_mapping", `{"customer-tts-alias":"`+constant.ModelDoubaoSeedTTS20+`"}`)
	context.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/audio/speech",
		bytes.NewBufferString(`{
			"model":"customer-tts-alias",
			"input":"测试",
			"voice":"speaker",
			"stream_format":"SSE",
			"timestamp_granularities":["WORD","segment","word"]
		}`),
	)
	context.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidAudioRequest(context, relayconstant.RelayModeAudioSpeech)
	require.NoError(t, err)
	assert.Equal(t, "customer-tts-alias", request.Model)
	assert.Equal(t, "sse", request.StreamFormat)
	assert.Equal(t, []string{"word", "segment"}, request.TimestampGranularities)
	assert.Equal(t, []string{"json"}, request.SubtitleFormats)

	context, _ = gin.CreateTestContext(httptest.NewRecorder())
	context.Set("model_mapping", `{"customer-tts-alias":"`+constant.ModelDoubaoSeedTTS20+`"}`)
	context.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/audio/speech",
		bytes.NewBufferString(`{
			"model":"customer-tts-alias",
			"input":"测试",
			"voice":"speaker",
			"timestamp_granularities":["word"]
		}`),
	)
	context.Request.Header.Set("Content-Type", "application/json")

	_, err = GetAndValidAudioRequest(context, relayconstant.RelayModeAudioSpeech)
	require.ErrorContains(t, err, "require stream_format=sse")
}

func TestMappedVolcASRValidatesAndKeepsBusinessModel(t *testing.T) {
	const (
		sampleRate    = 16000
		bitsPerSample = 16
		channels      = 1
		durationMS    = 200
	)
	audioDataSize := sampleRate * durationMS / 1000 * channels * bitsPerSample / 8
	wav := make([]byte, 44+audioDataSize)
	copy(wav[0:4], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:8], uint32(len(wav)-8))
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint16(wav[20:22], 1)
	binary.LittleEndian.PutUint16(wav[22:24], channels)
	binary.LittleEndian.PutUint32(wav[24:28], sampleRate)
	binary.LittleEndian.PutUint32(wav[28:32], sampleRate*channels*bitsPerSample/8)
	binary.LittleEndian.PutUint16(wav[32:34], channels*bitsPerSample/8)
	binary.LittleEndian.PutUint16(wav[34:36], bitsPerSample)
	copy(wav[36:40], "data")
	binary.LittleEndian.PutUint32(wav[40:44], uint32(audioDataSize))

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "sample.wav")
	require.NoError(t, err)
	_, err = part.Write(wav)
	require.NoError(t, err)
	require.NoError(t, writer.WriteField("model", "customer-asr-alias"))
	require.NoError(t, writer.WriteField("response_format", "verbose_json"))
	require.NoError(t, writer.WriteField("timestamp_granularities[]", "word"))
	require.NoError(t, writer.WriteField("timestamp_granularities[]", "segment"))
	require.NoError(t, writer.WriteField("timestamp_granularities", "word"))
	require.NoError(t, writer.Close())

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("model_mapping", `{"customer-asr-alias":"`+constant.ModelDoubaoSeedASRFlash+`"}`)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body.Bytes()))
	context.Request.Header.Set("Content-Type", writer.FormDataContentType())

	request, err := GetAndValidAudioRequest(context, relayconstant.RelayModeAudioTranscription)
	require.NoError(t, err)
	assert.Equal(t, "customer-asr-alias", request.Model)
	assert.Equal(t, "wav", request.LocalAudioFormat)
	assert.Equal(t, int64(durationMS), request.LocalAudioDurationMS)
	assert.Equal(t, []string{"word", "segment"}, request.TimestampGranularities)
}

func TestMappedVolcASRRejectsUnsupportedResponseFormatBeforeFileParsing(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "another-asr-alias"))
	require.NoError(t, writer.WriteField("response_format", "diarized_json"))
	require.NoError(t, writer.Close())

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("model_mapping", `{"another-asr-alias":"`+constant.ModelDoubaoSeedASRFlash+`"}`)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body.Bytes()))
	context.Request.Header.Set("Content-Type", writer.FormDataContentType())

	_, err := GetAndValidAudioRequest(context, relayconstant.RelayModeAudioTranscription)
	require.ErrorContains(t, err, "unsupported response_format")
}

func TestMappedVolcASRRejectsTimestampGranularityOutsideVerboseJSON(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "customer-asr-alias"))
	require.NoError(t, writer.WriteField("response_format", "srt"))
	require.NoError(t, writer.WriteField("timestamp_granularities[]", "word"))
	require.NoError(t, writer.Close())

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("model_mapping", `{"customer-asr-alias":"`+constant.ModelDoubaoSeedASRFlash+`"}`)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body.Bytes()))
	context.Request.Header.Set("Content-Type", writer.FormDataContentType())

	_, err := GetAndValidAudioRequest(context, relayconstant.RelayModeAudioTranscription)
	require.ErrorContains(t, err, "require response_format=verbose_json")
}
