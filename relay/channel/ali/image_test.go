package ali

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMappedQwenImageProtocol(t *testing.T) {
	for _, model := range []string{"qwen-image-2.0-pro", "qwen-image-2.0"} {
		for _, mode := range []int{constant.RelayModeImagesGenerations, constant.RelayModeImagesEdits} {
			for _, field := range []string{"image", "image[]"} {
				t.Run(model+"/"+strconv.Itoa(mode)+"/"+field, func(t *testing.T) {
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					var body bytes.Buffer
					writer := multipart.NewWriter(&body)
					images := []string{"first image", "second image"}
					for _, data := range images {
						part, err := writer.CreateFormFile(field, "reference.png")
						require.NoError(t, err)
						_, err = io.WriteString(part, data)
						require.NoError(t, err)
					}
					require.NoError(t, writer.Close())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
					c.Request.Header.Set("Content-Type", writer.FormDataContentType())
					t.Cleanup(func() {
						if c.Request.MultipartForm != nil {
							require.NoError(t, c.Request.MultipartForm.RemoveAll())
						}
						common.CleanupBodyStorage(c)
					})
					c.Set("model_mapping", `{"sxh-image-alternate-1":"intermediate","intermediate":"`+model+`"}`)
					info := &relaycommon.RelayInfo{OriginModelName: "sxh-image-alternate-1", RelayMode: mode, RelayFormat: types.RelayFormatOpenAI, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "sxh-image-alternate-1", ChannelBaseUrl: "https://example.invalid"}}
					n := uint(1)
					req := dto.ImageRequest{Model: info.OriginModelName, Prompt: "combine references", Size: "1536x1024", N: &n, ResponseFormat: "url", Extra: map[string]json.RawMessage{}}
					require.NoError(t, helper.ModelMappedHelper(c, info, &req))
					adaptor := &Adaptor{}
					converted, err := adaptor.ConvertImageRequest(c, info, req)
					require.NoError(t, err)
					assert.True(t, adaptor.IsSyncImageModel)
					url, err := adaptor.GetRequestURL(info)
					require.NoError(t, err)
					assert.Equal(t, "https://example.invalid/api/v1/services/aigc/multimodal-generation/generation", url)
					headers := http.Header{}
					require.NoError(t, adaptor.SetupRequestHeader(c, &headers, info))
					assert.Empty(t, headers.Get("X-DashScope-Async"))
					convertedReq := converted.(*AliImageRequest)
					assert.Equal(t, model, convertedReq.Model)
					assert.Equal(t, "1536*1024", convertedReq.Parameters.Size)
					assert.Equal(t, 1, convertedReq.Parameters.N)
					input := convertedReq.Input.(AliImageInput)
					require.Len(t, input.Messages, 1)
					content := input.Messages[0].Content.([]AliMediaContent)
					if mode == constant.RelayModeImagesEdits {
						require.Len(t, content, 3)
						for i, data := range images {
							assert.Equal(t, "data:text/plain; charset=utf-8;base64,"+base64.StdEncoding.EncodeToString([]byte(data)), content[i].Image)
						}
					} else {
						require.Len(t, content, 1)
					}
					assert.Equal(t, req.Prompt, content[len(content)-1].Text)
					// A sync result must be returned directly, without attempting task polling.
					if !adaptor.IsSyncImageModel {
						return
					}
					response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"output":{"choices":[{"message":{"content":[{"image":"https://example.invalid/result.png"}]}}]},"usage":{"image_count":1}}`))}
					_, apiErr := adaptor.DoResponse(c, response, info)
					require.Nil(t, apiErr)
					var result dto.ImageResponse
					require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
					require.Len(t, result.Data, 1)
					assert.Equal(t, "https://example.invalid/result.png", result.Data[0].Url)
					assert.Equal(t, "sxh-image-alternate-1", info.OriginModelName)
				})
			}
		}
	}
}

func TestAliImageRoutingUsesEffectiveModel(t *testing.T) {
	for _, tc := range []struct {
		name, origin, upstream, path string
		mode                         int
		async                        bool
	}{
		{"unmapped qwen", "qwen-image-2.0", "", "multimodal-generation/generation", constant.RelayModeImagesGenerations, false},
		{"mapped async generation", "qwen-image", "wanx-v1", "text2image/image-synthesis", constant.RelayModeImagesGenerations, true},
		{"mapped old wan edit", "image-alias", "wanx2.1-imageedit", "image2image/image-synthesis", constant.RelayModeImagesEdits, true},
		{"mapped wan edit", "image-alias", "wan2.6-image", "image-generation/generation", constant.RelayModeImagesEdits, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			info := &relaycommon.RelayInfo{OriginModelName: tc.origin, RelayMode: tc.mode, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: tc.upstream, ChannelBaseUrl: "https://example.invalid"}}
			adaptor := &Adaptor{}
			url, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, "https://example.invalid/api/v1/services/aigc/"+tc.path, url)
			headers := http.Header{}
			require.NoError(t, adaptor.SetupRequestHeader(c, &headers, info))
			assert.Equal(t, tc.async, headers.Get("X-DashScope-Async") == "enable")
		})
	}
}
