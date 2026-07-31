package common

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type multipartStreamingStorage struct {
	*bytes.Reader
}

func (s *multipartStreamingStorage) Close() error {
	return nil
}

func (s *multipartStreamingStorage) Bytes() ([]byte, error) {
	return nil, errors.New("multipart parser must not materialize body storage")
}

func (s *multipartStreamingStorage) Size() int64 {
	return s.Reader.Size()
}

func (s *multipartStreamingStorage) IsDisk() bool {
	return true
}

func TestParseMultipartFormReusableStreamsBodyStorage(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "doubao-seed-asr-flash"))
	require.NoError(t, writer.WriteField("timestamp_granularities", "word"))
	require.NoError(t, writer.WriteField("timestamp_granularities[]", "segment"))
	part, err := writer.CreateFormFile("file", "sample.wav")
	require.NoError(t, err)
	_, err = part.Write([]byte("audio-data"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", nil)
	context.Request.Header.Set("Content-Type", writer.FormDataContentType())
	storage := &multipartStreamingStorage{Reader: bytes.NewReader(body.Bytes())}
	context.Set(KeyBodyStorage, storage)

	var request struct {
		Model                  string   `json:"model"`
		TimestampGranularities []string `json:"timestamp_granularities"`
	}
	require.NoError(t, UnmarshalBodyReusable(context, &request))
	assert.Equal(t, "doubao-seed-asr-flash", request.Model)
	assert.Equal(t, []string{"word"}, request.TimestampGranularities)

	form, err := ParseMultipartFormReusable(context)
	require.NoError(t, err)
	defer form.RemoveAll()
	assert.Equal(t, "doubao-seed-asr-flash", form.Value["model"][0])
	require.Len(t, form.File["file"], 1)
	file, err := form.File["file"][0].Open()
	require.NoError(t, err)
	defer file.Close()
	audio, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.Equal(t, []byte("audio-data"), audio)

	replayed, err := io.ReadAll(context.Request.Body)
	require.NoError(t, err)
	assert.Equal(t, body.Bytes(), replayed)
}

func TestLargeTranscriptionMultipartUsesDiskWithoutGlobalDiskCache(t *testing.T) {
	originalConfig := GetDiskCacheConfig()
	SetDiskCacheConfig(DiskCacheConfig{
		Enabled:     false,
		ThresholdMB: 1,
		MaxSizeMB:   1024,
		Path:        t.TempDir(),
	})
	defer SetDiskCacheConfig(originalConfig)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "doubao-seed-asr-flash"))
	part, err := writer.CreateFormFile("file", "sample.wav")
	require.NoError(t, err)
	_, err = part.Write(bytes.Repeat([]byte{0}, 1<<20))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body.Bytes()))
	context.Request.Header.Set("Content-Type", writer.FormDataContentType())

	storage, err := GetBodyStorage(context)
	require.NoError(t, err)
	defer CleanupBodyStorage(context)
	assert.True(t, storage.IsDisk())
	assert.Equal(t, int64(body.Len()), storage.Size())
}
