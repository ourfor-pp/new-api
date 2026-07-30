package volcengine

import (
	"errors"
	"net/http"
	"testing"

	channelconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildVolcSpeechHeadersSelectsCredentialMode(t *testing.T) {
	tests := []struct {
		name            string
		credential      string
		legacyAppHeader string
		want            map[string]string
	}{
		{
			name:            "新版单段 Key",
			credential:      "new-console-key",
			legacyAppHeader: "X-Api-App-Id",
			want: map[string]string{
				"X-Api-Key":         "new-console-key",
				"X-Api-Resource-Id": volcTTSResourceID,
			},
		},
		{
			name:            "旧版 TTS 鉴权",
			credential:      "app-id|access-token",
			legacyAppHeader: "X-Api-App-Id",
			want: map[string]string{
				"X-Api-App-Id":      "app-id",
				"X-Api-Access-Key":  "access-token",
				"X-Api-Resource-Id": volcTTSResourceID,
			},
		},
		{
			name:            "旧版 ASR 鉴权",
			credential:      "app-key|access-token",
			legacyAppHeader: "X-Api-App-Key",
			want: map[string]string{
				"X-Api-App-Key":     "app-key",
				"X-Api-Access-Key":  "access-token",
				"X-Api-Resource-Id": volcTTSResourceID,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			headers, err := buildVolcSpeechHeaders(test.credential, volcTTSResourceID, test.legacyAppHeader)
			require.NoError(t, err)
			for key, want := range test.want {
				assert.Equal(t, want, headers.Get(key))
			}
			assert.NotEmpty(t, headers.Get("X-Api-Request-Id"))
			if headers.Get("X-Api-Key") != "" {
				assert.Empty(t, headers.Get("X-Api-Access-Key"))
			}
		})
	}
}

func TestBuildVolcSpeechHeadersRejectsMalformedCredential(t *testing.T) {
	for _, credential := range []string{"", "app|", "|token", "a|b|c"} {
		t.Run(credential, func(t *testing.T) {
			headers, err := buildVolcSpeechHeaders(credential, volcTTSResourceID, "X-Api-App-Id")
			require.Error(t, err)
			assert.Nil(t, headers)
		})
	}
}

func TestVolcSpeechInvalidCredentialIsChannelFailure(t *testing.T) {
	context, _ := newVolcSpeechTestContext()
	info := &relaycommon.RelayInfo{
		OriginModelName: channelconstant.ModelDoubaoSeedTTS20,
		ChannelMeta:     &relaycommon.ChannelMeta{ApiKey: ""},
	}

	_, err := (&Adaptor{}).ConvertAudioRequest(context, info, dto.AudioRequest{
		Input:          "测试",
		Voice:          "S_seed_special",
		ResponseFormat: "mp3",
	})
	require.Error(t, err)

	var apiErr *types.NewAPIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, types.ErrorCodeChannelInvalidKey, apiErr.GetErrorCode())
	assert.True(t, types.IsChannelError(apiErr))
	assert.False(t, types.IsSkipRetryError(apiErr))
}

func TestVolcSpeechProviderStatusPreservesRetryableFailures(t *testing.T) {
	assert.Equal(t, http.StatusTooManyRequests, volcSpeechProviderStatus(45000000, "quota exceeded for types: concurrency"))
	assert.Equal(t, http.StatusBadGateway, volcSpeechProviderStatus(55000031, "server busy"))
	assert.Equal(t, http.StatusBadRequest, volcSpeechProviderStatus(45000001, "invalid request"))
}
