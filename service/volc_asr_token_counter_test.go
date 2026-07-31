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

func TestEstimateRequestTokenUsesMappedVolcSpeechModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("TTS 任意别名按字符预扣", func(t *testing.T) {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Set("model_mapping", `{"customer-tts-alias":"`+constant.ModelDoubaoSeedTTS20+`"}`)
		info := &relaycommon.RelayInfo{
			OriginModelName: "customer-tts-alias",
			Request:         &dto.AudioRequest{Input: "你好A"},
		}

		tokens, err := EstimateRequestToken(context, &types.TokenCountMeta{}, info)
		require.NoError(t, err)
		assert.Equal(t, 3, tokens)
	})

	t.Run("ASR 任意别名按时长预扣", func(t *testing.T) {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Set("model_mapping", `{"customer-asr-alias":"`+constant.ModelDoubaoSeedASRFlash+`"}`)
		info := &relaycommon.RelayInfo{
			OriginModelName: "customer-asr-alias",
			Request:         &dto.AudioRequest{LocalAudioDurationMS: 2499},
		}

		tokens, err := EstimateRequestToken(context, &types.TokenCountMeta{}, info)
		require.NoError(t, err)
		assert.Equal(t, 42, tokens)
	})
}
