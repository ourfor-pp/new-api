package constant

import "strings"

const (
	ModelDoubaoSeedTTS20 = "doubao-seed-tts-2.0"
	ModelVolcASRFlash    = "volc.bigasr.auc_turbo"
	ModelVolcASR20       = "volc.seedasr.auc"
)

// IsVolcASRModel recognizes recording-file ASR resource IDs, not streaming SAUC
// resources. The selected resource must be enabled and support the flash API.
func IsVolcASRModel(model string) bool {
	if !strings.HasPrefix(model, "volc.") {
		return false
	}
	family, variant, ok := strings.Cut(strings.TrimPrefix(model, "volc."), ".")
	return ok && strings.HasSuffix(family, "asr") &&
		(variant == "auc" || strings.HasPrefix(variant, "auc_"))
}

func IsVolcSpeechModel(model string) bool {
	return model == ModelDoubaoSeedTTS20 || IsVolcASRModel(model)
}
