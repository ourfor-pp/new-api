package relay

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageOverrideRejectsPassthrough(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(map[bool]string{false: "channel", true: "global"}[global], func(t *testing.T) {
			settings := model_setting.GetGlobalSettings()
			previous := settings.PassThroughRequestEnabled
			settings.PassThroughRequestEnabled = global
			defer func() { settings.PassThroughRequestEnabled = previous }()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-image-2")
			common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: !global})
			common.SetContextKey(c, constant.ContextKeyChannelParamOverride, map[string]interface{}{"operations": []interface{}{map[string]interface{}{"mode": "delete", "path": "input_fidelity"}}})
			info := &relaycommon.RelayInfo{OriginModelName: "gpt-image-2", RelayMode: relayconstant.RelayModeImagesEdits, Request: &dto.ImageRequest{Model: "gpt-image-2"}}
			err := ImageHelper(c, info)
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), "passthrough to be disabled")
		})
	}
}

// imageRetryTransport captures the complete outbound request without contacting
// an upstream or entering response settlement.
type imageRetryTransport struct {
	bodies       [][]byte
	contentTypes []string
	lengths      []int64
}

func (tr *imageRetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	tr.bodies = append(tr.bodies, body)
	tr.contentTypes = append(tr.contentTypes, req.Header.Get("Content-Type"))
	tr.lengths = append(tr.lengths, req.ContentLength)
	return nil, fmt.Errorf("test upstream unavailable")
}

func TestImageRetryFromDeletionToPassthrough(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previous := settings.PassThroughRequestEnabled
	settings.PassThroughRequestEnabled = false
	defer func() { settings.PassThroughRequestEnabled = previous }()
	if service.GetHttpClient() == nil {
		service.InitHttpClient()
	}
	client := service.GetHttpClient()
	previousTransport := client.Transport
	transport := &imageRetryTransport{}
	client.Transport = transport
	defer func() { client.Transport = previousTransport }()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-2"))
	require.NoError(t, writer.WriteField("input_fidelity", "high"))
	file, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = io.WriteString(file, "image bytes")
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	original := bytes.Clone(body.Bytes())
	originalType := writer.FormDataContentType()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(original))
	c.Request.Header.Set("Content-Type", originalType)
	storage, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	defer common.CleanupBodyStorage(c)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-image-2")
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://upstream.invalid")
	info := &relaycommon.RelayInfo{OriginModelName: "gpt-image-2", RequestURLPath: "/v1/images/edits", RelayMode: relayconstant.RelayModeImagesEdits, Request: &dto.ImageRequest{Model: "gpt-image-2"}}
	for attempt := 0; attempt < 2; attempt++ {
		info.RetryIndex = attempt
		_, err = storage.Seek(0, io.SeekStart)
		require.NoError(t, err)
		c.Request.Body = io.NopCloser(storage)
		common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: attempt == 1})
		rules := map[string]interface{}{}
		if attempt == 0 {
			rules["operations"] = []interface{}{map[string]interface{}{"mode": "delete", "path": "input_fidelity"}}
		}
		common.SetContextKey(c, constant.ContextKeyChannelParamOverride, rules)
		relayErr := ImageHelper(c, info)
		require.NotNil(t, relayErr)
		require.Len(t, transport.bodies, attempt+1)
		if attempt == 0 {
			assert.Equal(t, []string{"delete input_fidelity"}, info.ParamOverrideAudit)
		}
	}
	require.Len(t, transport.bodies, 2)
	assert.NotEqual(t, len(original), len(transport.bodies[0]))
	assert.Equal(t, original, transport.bodies[1])
	assert.Equal(t, originalType, transport.contentTypes[1])
	assert.Empty(t, info.ParamOverrideAudit)
	for i, sent := range transport.bodies {
		assert.Equal(t, int64(len(sent)), transport.lengths[i])
		replay := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(sent))
		replay.Header.Set("Content-Type", transport.contentTypes[i])
		require.NoError(t, replay.ParseMultipartForm(32<<20))
		defer replay.MultipartForm.RemoveAll()
		if i == 0 {
			assert.Empty(t, replay.FormValue("input_fidelity"))
		} else {
			assert.Equal(t, "high", replay.FormValue("input_fidelity"))
		}
		require.Len(t, replay.MultipartForm.File["image"], 1)
	}
}
