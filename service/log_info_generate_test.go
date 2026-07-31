package service

import (
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateTextOtherInfoPersistsPrivacySafeVolcSpeechOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/audio/transcriptions", nil)
	now := time.Now()
	info := &relaycommon.RelayInfo{
		StartTime:         now,
		FirstResponseTime: now,
		ChannelMeta:       &relaycommon.ChannelMeta{},
		VolcSpeechAudit: &relaycommon.VolcSpeechAuditInfo{
			ResourceID:       "volc.bigasr.auc_turbo",
			Protocol:         "v3-http-flash",
			SpeechOptions:    []string{"punctuation=false", "hotwords"},
			ContextTextCount: 1,
			HotwordCount:     2,
			ReplacementCount: 3,
		},
	}

	other := GenerateTextOtherInfo(ctx, info, 1, 1, 1, 0, 1, -1, -1)

	audit, ok := other["volc_speech"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, []string{"punctuation=false", "hotwords"}, audit["speech_options"])
	assert.Equal(t, 1, audit["context_text_count"])
	assert.Equal(t, 2, audit["hotword_count"])
	assert.Equal(t, 3, audit["replacement_count"])
	assert.NotContains(t, audit, "instructions")
	assert.NotContains(t, audit, "context_texts")
}
