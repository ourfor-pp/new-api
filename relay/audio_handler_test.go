package relay

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
)

func TestMarkVolcSpeechGatewayTimeoutRetry(t *testing.T) {
	for _, modelName := range []string{
		constant.ModelDoubaoSeedTTS20,
		constant.ModelDoubaoSeedASRFlash,
	} {
		for _, statusCode := range []int{http.StatusGatewayTimeout, 524} {
			apiErr := types.NewErrorWithStatusCode(errors.New("gateway timeout"), types.ErrorCodeBadResponse, statusCode)
			result := markVolcSpeechGatewayTimeoutRetry(modelName, statusCode, apiErr)
			assert.Same(t, apiErr, result)
			assert.True(t, types.IsForceRetryError(result))
		}
	}

	nonSpeechError := types.NewErrorWithStatusCode(errors.New("gateway timeout"), types.ErrorCodeBadResponse, http.StatusGatewayTimeout)
	assert.False(t, types.IsForceRetryError(markVolcSpeechGatewayTimeoutRetry("gpt-4o-mini", http.StatusGatewayTimeout, nonSpeechError)))

	otherStatusError := types.NewErrorWithStatusCode(errors.New("server error"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	assert.False(t, types.IsForceRetryError(markVolcSpeechGatewayTimeoutRetry(constant.ModelDoubaoSeedTTS20, http.StatusInternalServerError, otherStatusError)))
}

func TestNewAudioConvertErrorKeepsChannelFailureRetryable(t *testing.T) {
	channelConfigErr := types.NewError(errors.New("channel config invalid"), types.ErrorCodeChannelConfigInvalid)
	result := newAudioConvertError(channelConfigErr)
	assert.True(t, types.IsChannelError(result))
	assert.False(t, types.IsSkipRetryError(result))

	requestErr := newAudioConvertError(errors.New("request format invalid"))
	assert.False(t, types.IsChannelError(requestErr))
	assert.True(t, types.IsSkipRetryError(requestErr))
}
