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
