package dto

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type AudioRequest struct {
	Model                  string          `json:"model"`
	Input                  string          `json:"input"`
	Voice                  string          `json:"voice"`
	Instructions           string          `json:"instructions,omitempty"`
	ResponseFormat         string          `json:"response_format,omitempty"`
	Speed                  *float64        `json:"speed,omitempty"`
	StreamFormat           string          `json:"stream_format,omitempty"`
	TimestampGranularities []string        `json:"timestamp_granularities,omitempty"`
	SubtitleFormats        []string        `json:"subtitle_formats,omitempty"`
	SpeechOptions          *SpeechOptions  `json:"speech_options,omitempty"`
	Metadata               json.RawMessage `json:"metadata,omitempty"`
	// vllm-omini
	TaskType                json.RawMessage `json:"task_type,omitempty"`
	Language                json.RawMessage `json:"language,omitempty"`
	RefAudio                json.RawMessage `json:"ref_audio,omitempty"`
	RefText                 json.RawMessage `json:"ref_text,omitempty"`
	XVectorOnlyMode         json.RawMessage `json:"x_vector_only_mode,omitempty"`
	MaxNewTokens            json.RawMessage `json:"max_new_tokens,omitempty"`
	InitialCodecChunkFrames json.RawMessage `json:"initial_codec_chunk_frames,omitempty"`
	// LocalAudioDurationMS 仅保存网关本地解析出的音频时长，不会发送给上游。
	LocalAudioDurationMS int64 `json:"-"`
	// LocalAudioFormat 仅保存经过校验的音频格式，不会发送给上游。
	LocalAudioFormat string `json:"-"`
	// TODO：ensure that the logic remains correct after the stream is started.
	//Stream                  json.RawMessage `json:"stream,omitempty"`
}

type SpeechOptions struct {
	SampleRate          *int                  `json:"sample_rate,omitempty"`
	TextNormalization   *bool                 `json:"text_normalization,omitempty"`
	Punctuation         *bool                 `json:"punctuation,omitempty"`
	SemanticSmoothing   *bool                 `json:"semantic_smoothing,omitempty"`
	SensitiveWordFilter *bool                 `json:"sensitive_word_filter,omitempty"`
	VADSegmentation     *bool                 `json:"vad_segmentation,omitempty"`
	SpeakerDiarization  *bool                 `json:"speaker_diarization,omitempty"`
	ChannelMode         string                `json:"channel_mode,omitempty"`
	ForceSegmentAfterMS *int                  `json:"force_segment_after_ms,omitempty"`
	Hotwords            []string              `json:"hotwords,omitempty"`
	Replacements        map[string]string     `json:"replacements,omitempty"`
	Detect              []string              `json:"detect,omitempty"`
	Context             *SpeechOptionsContext `json:"context,omitempty"`
}

type SpeechOptionsContext struct {
	Texts  []string                    `json:"texts,omitempty"`
	Images []SpeechOptionsContextImage `json:"images,omitempty"`
}

type SpeechOptionsContextImage struct {
	ImageURL string `json:"image_url"`
}

func (r *AudioRequest) GetTokenCountMeta() *types.TokenCountMeta {
	meta := &types.TokenCountMeta{
		CombineText: r.Input,
		TokenType:   types.TokenTypeTextNumber,
	}
	if strings.Contains(r.Model, "gpt") {
		meta.TokenType = types.TokenTypeTokenizer
	}
	return meta
}

func (r *AudioRequest) IsStream(c *gin.Context) bool {
	return r.StreamFormat == "sse"
}

func (r *AudioRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}

type AudioResponse struct {
	Text string `json:"text"`
}

type WhisperVerboseJSONResponse struct {
	Task        string             `json:"task,omitempty"`
	Language    string             `json:"language,omitempty"`
	Duration    float64            `json:"duration,omitempty"`
	Text        string             `json:"text,omitempty"`
	Segments    []Segment          `json:"segments,omitempty"`
	Words       []AudioWord        `json:"words,omitempty"`
	Annotations []SpeechAnnotation `json:"annotations,omitempty"`
}

type AudioWord struct {
	Word       string   `json:"word"`
	Start      float64  `json:"start"`
	End        float64  `json:"end"`
	Confidence *float64 `json:"confidence,omitempty"`
	Speaker    string   `json:"speaker,omitempty"`
	Channel    *int     `json:"channel,omitempty"`
}

type Segment struct {
	Id               int     `json:"id"`
	Seek             int     `json:"seek"`
	Start            float64 `json:"start"`
	End              float64 `json:"end"`
	Text             string  `json:"text"`
	Tokens           []int   `json:"tokens"`
	Temperature      float64 `json:"temperature"`
	AvgLogprob       float64 `json:"avg_logprob"`
	CompressionRatio float64 `json:"compression_ratio"`
	NoSpeechProb     float64 `json:"no_speech_prob"`
	Speaker          string  `json:"speaker,omitempty"`
	Channel          *int    `json:"channel,omitempty"`
}

type SpeechAnnotation struct {
	Type       string   `json:"type"`
	Start      float64  `json:"start"`
	End        float64  `json:"end"`
	Label      string   `json:"label"`
	Confidence *float64 `json:"confidence"`
}
