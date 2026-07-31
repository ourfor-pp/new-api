package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelMappedHelperKeepsBusinessModelAndUpdatesUpstreamRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("model_mapping", `{"customer-defined-alias":"`+constant.ModelDoubaoSeedTTS20+`"}`)

	request := &dto.AudioRequest{Model: "customer-defined-alias"}
	info := &relaycommon.RelayInfo{
		OriginModelName: "customer-defined-alias",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "customer-defined-alias",
		},
	}

	require.NoError(t, ModelMappedHelper(context, info, request))
	assert.Equal(t, "customer-defined-alias", info.OriginModelName)
	assert.Equal(t, constant.ModelDoubaoSeedTTS20, info.UpstreamModelName)
	assert.True(t, info.IsModelMapped)
	assert.Equal(t, constant.ModelDoubaoSeedTTS20, request.Model)
}
