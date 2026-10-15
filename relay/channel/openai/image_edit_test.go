package openai

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConvertImageEditRequestMultipart verifies that ConvertImageRequest
// re-serializes multipart image edit requests with all fields (including
// stream) and the file intact, both when the form was already parsed and when
// it must be re-parsed from the reusable body.
func TestConvertImageEditRequestMultipart(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newMultipartContext := func(t *testing.T, prompt string) *gin.Context {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "gpt-image-1"))
		require.NoError(t, writer.WriteField("prompt", prompt))
		require.NoError(t, writer.WriteField("stream", "true"))
		require.NoError(t, writer.WriteField("partial_images", "3"))
		part, err := writer.CreateFormFile("image", "input.png")
		require.NoError(t, err)
		_, err = part.Write([]byte("fake image"))
		require.NoError(t, err)
		require.NoError(t, writer.Close())

		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		return c
	}

	convertAndReplay := func(t *testing.T, c *gin.Context, prompt string) {
		info := &relaycommon.RelayInfo{
			RelayMode: relayconstant.RelayModeImagesEdits,
		}
		request := dto.ImageRequest{
			Model:  "gpt-image-1",
			Prompt: prompt,
			Stream: common.GetPointer(true),
		}

		converted, err := (&Adaptor{}).ConvertImageRequest(c, info, request)
		require.NoError(t, err)
		convertedBody, ok := converted.(*bytes.Buffer)
		require.True(t, ok)

		replayedRequest := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(convertedBody.Bytes()))
		replayedRequest.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
		require.NoError(t, replayedRequest.ParseMultipartForm(32<<20))

		require.Equal(t, "gpt-image-1", replayedRequest.PostForm.Get("model"))
		require.Equal(t, prompt, replayedRequest.PostForm.Get("prompt"))
		require.Equal(t, "true", replayedRequest.PostForm.Get("stream"))
		require.Equal(t, "3", replayedRequest.PostForm.Get("partial_images"))
		require.Len(t, replayedRequest.MultipartForm.File["image"], 1)

		file, err := replayedRequest.MultipartForm.File["image"][0].Open()
		require.NoError(t, err)
		defer file.Close()
		fileBytes, err := io.ReadAll(file)
		require.NoError(t, err)
		require.Equal(t, []byte("fake image"), fileBytes)
	}

	t.Run("with pre-parsed form", func(t *testing.T) {
		prompt := "edit this image"
		c := newMultipartContext(t, prompt)
		require.NoError(t, c.Request.ParseMultipartForm(32<<20))

		convertAndReplay(t, c, prompt)
	})

	t.Run("re-parses reusable body when form is missing", func(t *testing.T) {
		prompt := "edit without pre-parsed form"
		c := newMultipartContext(t, prompt)

		storage, err := common.GetBodyStorage(c)
		require.NoError(t, err)
		c.Request.Body = io.NopCloser(storage)
		c.Request.MultipartForm = nil
		c.Request.PostForm = nil

		convertAndReplay(t, c, prompt)
	})
}

// A second channel attempt must see the original fields and all input files.
func TestImageEditDeletionPreservesFilesAndRetry(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, values := range map[string][]string{"model": {"client-model"}, "input_fidelity": {"high", "low"}, "tag": {"one", "two"}} {
		for _, value := range values {
			require.NoError(t, writer.WriteField(key, value))
		}
	}
	for _, file := range []struct{ field, name, content string }{{"image[]", "a.png", "first image"}, {"image[]", "b.png", "second image"}, {"mask", "mask.png", "mask bytes"}} {
		part, err := writer.CreateFormFile(file.field, file.name)
		require.NoError(t, err)
		_, err = io.WriteString(part, file.content)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	require.NoError(t, c.Request.ParseMultipartForm(32<<20))
	defer c.Request.MultipartForm.RemoveAll()
	for _, deleteField := range []bool{true, false} {
		info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesEdits, ChannelMeta: &relaycommon.ChannelMeta{}}
		if deleteField {
			info.ParamOverride = map[string]interface{}{"operations": []interface{}{map[string]interface{}{"mode": "delete", "path": "input_fidelity"}}}
		}
		converted, err := (&Adaptor{}).ConvertImageRequest(c, info, dto.ImageRequest{Model: "upstream-model"})
		require.NoError(t, err)
		buffer, ok := converted.(*bytes.Buffer)
		require.True(t, ok)
		assert.Equal(t, int64(buffer.Len()), info.UpstreamRequestBodySize)
		replay := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(buffer.Bytes()))
		replay.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
		require.NoError(t, replay.ParseMultipartForm(32<<20))
		defer replay.MultipartForm.RemoveAll()
		assert.Equal(t, "upstream-model", replay.FormValue("model"))
		assert.Equal(t, []string{"one", "two"}, replay.MultipartForm.Value["tag"])
		if deleteField {
			assert.NotContains(t, replay.MultipartForm.Value, "input_fidelity")
		} else {
			assert.Equal(t, []string{"high", "low"}, replay.MultipartForm.Value["input_fidelity"])
		}
		require.Len(t, replay.MultipartForm.File["image[]"], 2)
		require.Len(t, replay.MultipartForm.File["mask"], 1)
		for field, expected := range map[string][]string{"image[]": {"first image", "second image"}, "mask": {"mask bytes"}} {
			for i, header := range replay.MultipartForm.File[field] {
				file, err := header.Open()
				require.NoError(t, err)
				data, err := io.ReadAll(file)
				require.NoError(t, err)
				require.NoError(t, file.Close())
				assert.Equal(t, expected[i], string(data))
			}
		}
	}
}
