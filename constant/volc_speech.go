package constant

const (
	ModelDoubaoSeedTTS20    = "doubao-seed-tts-2.0"
	ModelDoubaoSeedASRFlash = "doubao-seed-asr-flash"
)

func IsVolcSpeechModel(model string) bool {
	return model == ModelDoubaoSeedTTS20 || model == ModelDoubaoSeedASRFlash
}
