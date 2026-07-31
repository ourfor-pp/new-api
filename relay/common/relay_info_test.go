package common

import (
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}

func TestVolcSpeechAuditLogValueIncludesOnlySafeOptionMetadata(t *testing.T) {
	audit := &VolcSpeechAuditInfo{
		ResourceID:             "resource",
		Protocol:               "protocol",
		LogID:                  "log-id",
		BillingUnits:           12,
		SpeechOptions:          []string{"punctuation=false", "hotwords"},
		ContextTextCount:       2,
		HotwordCount:           3,
		ReplacementCount:       4,
		SubtitleSentenceCount:  5,
		SubtitleWordCount:      6,
		TimestampGranularities: []string{"segment"},
	}

	value := audit.LogValue()

	assert.Equal(t, []string{"punctuation=false", "hotwords"}, value["speech_options"])
	assert.Equal(t, 2, value["context_text_count"])
	assert.Equal(t, 3, value["hotword_count"])
	assert.Equal(t, 4, value["replacement_count"])
	assert.Equal(t, 5, value["subtitle_sentence_count"])
	assert.Equal(t, 6, value["subtitle_word_count"])
	assert.NotContains(t, value, "instructions")
	assert.NotContains(t, value, "context_texts")
	assert.NotContains(t, value, "hotwords")
	assert.NotContains(t, value, "replacements")
}
