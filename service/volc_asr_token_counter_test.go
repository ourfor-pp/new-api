package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEstimateRequestTokenUsesValidatedVolcASRDuration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: constant.ModelDoubaoSeedASRFlash,
		Request:         &dto.AudioRequest{LocalAudioDurationMS: 2499},
	}

	tokens, err := EstimateRequestToken(context, &types.TokenCountMeta{}, info)
	require.NoError(t, err)
	assert.Equal(t, 42, tokens)
}

func TestEstimateRequestTokenUsesUnicodeCharactersForVolcTTS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: constant.ModelDoubaoSeedTTS20,
		Request:         &dto.AudioRequest{Input: "你好A"},
	}

	tokens, err := EstimateRequestToken(context, &types.TokenCountMeta{}, info)
	require.NoError(t, err)
	assert.Equal(t, 3, tokens)
}
