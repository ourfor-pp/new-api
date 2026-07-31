package helper

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	channelconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

func GetAndValidateRequest(c *gin.Context, format types.RelayFormat) (request dto.Request, err error) {
	relayMode := relayconstant.Path2RelayMode(c.Request.URL.Path)

	switch format {
	case types.RelayFormatOpenAI:
		request, err = GetAndValidateTextRequest(c, relayMode)
	case types.RelayFormatGemini:
		if strings.Contains(c.Request.URL.Path, ":embedContent") {
			request, err = GetAndValidateGeminiEmbeddingRequest(c)
		} else if strings.Contains(c.Request.URL.Path, ":batchEmbedContents") {
			request, err = GetAndValidateGeminiBatchEmbeddingRequest(c)
		} else {
			request, err = GetAndValidateGeminiRequest(c)
		}
	case types.RelayFormatClaude:
		request, err = GetAndValidateClaudeRequest(c)
	case types.RelayFormatOpenAIResponses:
		request, err = GetAndValidateResponsesRequest(c)
	case types.RelayFormatOpenAIResponsesCompaction:
		request, err = GetAndValidateResponsesCompactionRequest(c)
	case types.RelayFormatOpenAIAlphaSearch:
		request, err = GetAndValidateAlphaSearchRequest(c)

	case types.RelayFormatOpenAIImage:
		request, err = GetAndValidOpenAIImageRequest(c, relayMode)
	case types.RelayFormatEmbedding:
		request, err = GetAndValidateEmbeddingRequest(c, relayMode)
	case types.RelayFormatRerank:
		request, err = GetAndValidateRerankRequest(c)
	case types.RelayFormatOpenAIAudio:
		request, err = GetAndValidAudioRequest(c, relayMode)
	case types.RelayFormatOpenAIRealtime:
		request = &dto.BaseRequest{}
	default:
		return nil, fmt.Errorf("unsupported relay format: %s", format)
	}
	return request, err
}

func GetAndValidAudioRequest(c *gin.Context, relayMode int) (*dto.AudioRequest, error) {
	audioRequest := &dto.AudioRequest{}
	err := common.UnmarshalBodyReusable(c, audioRequest)
	if err != nil {
		return nil, err
	}
	normalizedGranularities := make([]string, 0, len(audioRequest.TimestampGranularities))
	seenGranularities := make(map[string]struct{}, len(audioRequest.TimestampGranularities))
	for _, granularity := range audioRequest.TimestampGranularities {
		granularity = strings.ToLower(strings.TrimSpace(granularity))
		if granularity == "" {
			continue
		}
		if _, exists := seenGranularities[granularity]; exists {
			continue
		}
		seenGranularities[granularity] = struct{}{}
		normalizedGranularities = append(normalizedGranularities, granularity)
	}
	audioRequest.TimestampGranularities = normalizedGranularities

	normalizedSubtitleFormats := make([]string, 0, len(audioRequest.SubtitleFormats))
	seenSubtitleFormats := make(map[string]struct{}, len(audioRequest.SubtitleFormats))
	for _, subtitleFormat := range audioRequest.SubtitleFormats {
		subtitleFormat = strings.ToLower(strings.TrimSpace(subtitleFormat))
		if subtitleFormat == "" {
			continue
		}
		if _, exists := seenSubtitleFormats[subtitleFormat]; exists {
			continue
		}
		seenSubtitleFormats[subtitleFormat] = struct{}{}
		normalizedSubtitleFormats = append(normalizedSubtitleFormats, subtitleFormat)
	}
	audioRequest.SubtitleFormats = normalizedSubtitleFormats
	if audioRequest.Model == "" {
		return nil, errors.New("model is required")
	}

	validationModel, _, err := relaycommon.ResolveMappedModelName(audioRequest.Model, c.GetString("model_mapping"))
	if err != nil {
		return nil, err
	}
	originModel := audioRequest.Model
	audioRequest.Model = validationModel
	err = ValidateVolcSpeechAudioRequest(c, relayMode, audioRequest)
	audioRequest.Model = originModel
	return audioRequest, err
}

// ValidateVolcSpeechAudioRequest 按映射后的上游模型执行火山语音专用校验。
func ValidateVolcSpeechAudioRequest(c *gin.Context, relayMode int, audioRequest *dto.AudioRequest) error {
	switch relayMode {
	case relayconstant.RelayModeAudioSpeech:
		if audioRequest.Model == channelconstant.ModelDoubaoSeedTTS20 {
			if strings.TrimSpace(audioRequest.Input) == "" {
				return errors.New("input is required")
			}
			if strings.TrimSpace(audioRequest.Voice) == "" {
				return errors.New("voice is required")
			}
			if audioRequest.ResponseFormat == "" {
				audioRequest.ResponseFormat = "mp3"
			}
			switch strings.ToLower(audioRequest.ResponseFormat) {
			case "mp3", "opus", "pcm":
			default:
				return fmt.Errorf("doubao-seed-tts-2.0 only supports mp3, opus, or pcm response_format")
			}
			if audioRequest.Speed != nil && (*audioRequest.Speed < 0.5 || *audioRequest.Speed > 2.0) {
				return errors.New("speed must be between 0.5 and 2.0")
			}
			audioRequest.StreamFormat = strings.ToLower(strings.TrimSpace(audioRequest.StreamFormat))
			switch audioRequest.StreamFormat {
			case "", "audio", "sse":
			default:
				return fmt.Errorf("doubao-seed-tts-2.0 only supports audio or sse stream_format")
			}
			for _, granularity := range audioRequest.TimestampGranularities {
				if granularity != "segment" && granularity != "word" {
					return fmt.Errorf("unsupported timestamp granularity for doubao-seed-tts-2.0: %s", granularity)
				}
			}
			for _, subtitleFormat := range audioRequest.SubtitleFormats {
				switch subtitleFormat {
				case "json", "srt", "vtt":
				default:
					return fmt.Errorf("unsupported subtitle format for doubao-seed-tts-2.0: %s", subtitleFormat)
				}
			}
			subtitleRequested := len(audioRequest.TimestampGranularities) > 0 || len(audioRequest.SubtitleFormats) > 0
			if subtitleRequested && audioRequest.StreamFormat != "sse" {
				return errors.New("doubao-seed-tts-2.0 timestamps and subtitles require stream_format=sse")
			}
			if len(audioRequest.SubtitleFormats) > 0 && len(audioRequest.TimestampGranularities) == 0 {
				audioRequest.TimestampGranularities = []string{"segment", "word"}
			}
			if len(audioRequest.TimestampGranularities) > 0 && len(audioRequest.SubtitleFormats) == 0 {
				audioRequest.SubtitleFormats = []string{"json"}
			}
		}
	default:
		if audioRequest.ResponseFormat == "" {
			audioRequest.ResponseFormat = "json"
		}
		if audioRequest.Model == channelconstant.ModelDoubaoSeedASRFlash {
			if len(audioRequest.SubtitleFormats) > 0 {
				return errors.New("doubao-seed-asr-flash does not support subtitle_formats; use response_format=srt or vtt")
			}
			for _, granularity := range audioRequest.TimestampGranularities {
				if granularity != "segment" && granularity != "word" {
					return fmt.Errorf("unsupported timestamp granularity for doubao-seed-asr-flash: %s", granularity)
				}
			}
			if len(audioRequest.TimestampGranularities) > 0 && !strings.EqualFold(audioRequest.ResponseFormat, "verbose_json") {
				return errors.New("doubao-seed-asr-flash timestamp_granularities require response_format=verbose_json")
			}
			switch strings.ToLower(audioRequest.ResponseFormat) {
			case "json", "text", "verbose_json", "srt", "vtt":
			default:
				return fmt.Errorf("unsupported response_format for doubao-seed-asr-flash: %s", audioRequest.ResponseFormat)
			}

			if audioRequest.LocalAudioFormat != "" && audioRequest.LocalAudioDurationMS > 0 {
				return nil
			}

			form, formErr := common.ParseMultipartFormReusable(c)
			if formErr != nil {
				return fmt.Errorf("failed to parse audio form: %w", formErr)
			}
			defer form.RemoveAll()
			files := form.File["file"]
			if len(files) == 0 {
				return errors.New("file is required")
			}
			fileHeader := files[0]
			ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
			audioFormat, validationErr := validateDoubaoASRFile(fileHeader.Filename, fileHeader.Size, 1)
			if validationErr != nil {
				return validationErr
			}
			file, openErr := fileHeader.Open()
			if openErr != nil {
				return fmt.Errorf("failed to open audio file: %w", openErr)
			}
			duration, durationErr := common.GetAudioDuration(c.Request.Context(), file, ext)
			_ = file.Close()
			if durationErr != nil {
				return fmt.Errorf("failed to get audio duration: %w", durationErr)
			}
			_, validationErr = validateDoubaoASRFile(fileHeader.Filename, fileHeader.Size, duration)
			if validationErr != nil {
				return validationErr
			}
			audioRequest.LocalAudioFormat = audioFormat
			audioRequest.LocalAudioDurationMS = int64(math.Round(duration * 1000))
		}
	}
	return nil
}

func validateDoubaoASRFile(filename string, size int64, duration float64) (string, error) {
	if size > 100*1024*1024 {
		return "", errors.New("audio file must not exceed 100MB")
	}
	var audioFormat string
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".wav":
		audioFormat = "wav"
	case ".mp3":
		audioFormat = "mp3"
	case ".ogg", ".opus":
		audioFormat = "ogg"
	default:
		return "", errors.New("audio file must be WAV, MP3, OGG, or Opus")
	}
	if duration <= 0 {
		return "", errors.New("audio duration must be greater than 0")
	}
	if duration > 2*60*60 {
		return "", errors.New("audio duration must not exceed 2 hours")
	}
	return audioFormat, nil
}

func GetAndValidateRerankRequest(c *gin.Context) (*dto.RerankRequest, error) {
	var rerankRequest *dto.RerankRequest
	err := common.UnmarshalBodyReusable(c, &rerankRequest)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("getAndValidateTextRequest failed: %s", err.Error()))
		return nil, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	if rerankRequest.Query == "" {
		return nil, types.NewError(fmt.Errorf("query is empty"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if len(rerankRequest.Documents) == 0 {
		return nil, types.NewError(fmt.Errorf("documents is empty"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	return rerankRequest, nil
}

func GetAndValidateEmbeddingRequest(c *gin.Context, relayMode int) (*dto.EmbeddingRequest, error) {
	var embeddingRequest *dto.EmbeddingRequest
	err := common.UnmarshalBodyReusable(c, &embeddingRequest)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("getAndValidateTextRequest failed: %s", err.Error()))
		return nil, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	if embeddingRequest.Input == nil {
		return nil, fmt.Errorf("input is empty")
	}
	if relayMode == relayconstant.RelayModeModerations && embeddingRequest.Model == "" {
		embeddingRequest.Model = "omni-moderation-latest"
	}
	if relayMode == relayconstant.RelayModeEmbeddings && embeddingRequest.Model == "" {
		embeddingRequest.Model = c.Param("model")
	}
	return embeddingRequest, nil
}

// maxTokensLimit bounds user-supplied max token fields. These values feed
// pre-consume quota math (preConsumedTokens * ratio); an unbounded value can
// overflow the conversion and corrupt billing.
const maxTokensLimit = math.MaxInt32 / 2

func exceedsMaxTokensLimit(values ...*uint) bool {
	for _, v := range values {
		if lo.FromPtrOr(v, uint(0)) > maxTokensLimit {
			return true
		}
	}
	return false
}

func GetAndValidateResponsesRequest(c *gin.Context) (*dto.OpenAIResponsesRequest, error) {
	request := &dto.OpenAIResponsesRequest{}
	err := common.UnmarshalBodyReusable(c, request)
	if err != nil {
		return nil, err
	}
	if request.Model == "" {
		return nil, errors.New("model is required")
	}
	if request.Input == nil {
		return nil, errors.New("input is required")
	}
	if exceedsMaxTokensLimit(request.MaxOutputTokens) {
		return nil, errors.New("max_output_tokens is invalid")
	}
	return request, nil
}

func GetAndValidateAlphaSearchRequest(c *gin.Context) (*dto.AlphaSearchRequest, error) {
	request := &dto.AlphaSearchRequest{}
	if err := common.UnmarshalBodyReusable(c, request); err != nil {
		return nil, err
	}
	if request.Model == "" {
		return nil, errors.New("model is required")
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	rawBody, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	request.RawBody = rawBody
	return request, nil
}

func GetAndValidateResponsesCompactionRequest(c *gin.Context) (*dto.OpenAIResponsesCompactionRequest, error) {
	request := &dto.OpenAIResponsesCompactionRequest{}
	if err := common.UnmarshalBodyReusable(c, request); err != nil {
		return nil, err
	}
	if request.Model == "" {
		return nil, errors.New("model is required")
	}
	return request, nil
}

func GetAndValidOpenAIImageRequest(c *gin.Context, relayMode int) (*dto.ImageRequest, error) {
	imageRequest := &dto.ImageRequest{}

	switch relayMode {
	case relayconstant.RelayModeImagesEdits:
		if strings.Contains(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
			form, err := common.ParseMultipartFormReusable(c)
			if err != nil {
				return nil, fmt.Errorf("failed to parse image edit form request: %w", err)
			}
			formData := url.Values(form.Value)
			c.Request.MultipartForm = form
			c.Request.PostForm = formData
			imageRequest.Prompt = formData.Get("prompt")
			imageRequest.Model = formData.Get("model")
			if nValue := strings.TrimSpace(formData.Get("n")); nValue != "" {
				n, err := strconv.Atoi(nValue)
				if err != nil || n < 0 || n > dto.MaxImageN {
					return nil, fmt.Errorf("n must be an integer between 1 and %d", dto.MaxImageN)
				}
				imageRequest.N = common.GetPointer(uint(n))
			}
			imageRequest.Quality = formData.Get("quality")
			imageRequest.Size = formData.Get("size")
			if streamValue := strings.TrimSpace(formData.Get("stream")); streamValue != "" {
				stream, err := strconv.ParseBool(streamValue)
				if err != nil {
					return nil, fmt.Errorf("invalid stream value: %w", err)
				}
				imageRequest.Stream = common.GetPointer(stream)
			}
			if imageValue := formData.Get("image"); imageValue != "" {
				imageRequest.Image, _ = common.Marshal(imageValue)
			}

			if imageRequest.Model == "gpt-image-1" {
				if imageRequest.Quality == "" {
					imageRequest.Quality = "standard"
				}
			}
			if imageRequest.N == nil || *imageRequest.N == 0 {
				imageRequest.N = common.GetPointer(uint(1))
			}

			hasWatermark := formData.Has("watermark")
			if hasWatermark {
				watermark := formData.Get("watermark") == "true"
				imageRequest.Watermark = &watermark
			}
			break
		}
		fallthrough
	default:
		err := common.UnmarshalBodyReusable(c, imageRequest)
		if err != nil {
			return nil, err
		}

		if imageRequest.Model == "" {
			//imageRequest.Model = "dall-e-3"
			return nil, errors.New("model is required")
		}

		if strings.Contains(imageRequest.Size, "×") {
			return nil, errors.New("size an unexpected error occurred in the parameter, please use 'x' instead of the multiplication sign '×'")
		}

		if imageRequest.N != nil && *imageRequest.N > dto.MaxImageN {
			return nil, fmt.Errorf("n must be an integer between 1 and %d", dto.MaxImageN)
		}

		// Not "256x256", "512x512", or "1024x1024"
		if imageRequest.Model == "dall-e-2" || imageRequest.Model == "dall-e" {
			if imageRequest.Size != "" && imageRequest.Size != "256x256" && imageRequest.Size != "512x512" && imageRequest.Size != "1024x1024" {
				return nil, errors.New("size must be one of 256x256, 512x512, or 1024x1024 for dall-e-2 or dall-e")
			}
			if imageRequest.Size == "" {
				imageRequest.Size = "1024x1024"
			}
		} else if imageRequest.Model == "dall-e-3" {
			if imageRequest.Size != "" && imageRequest.Size != "1024x1024" && imageRequest.Size != "1024x1792" && imageRequest.Size != "1792x1024" {
				return nil, errors.New("size must be one of 1024x1024, 1024x1792 or 1792x1024 for dall-e-3")
			}
			if imageRequest.Quality == "" {
				imageRequest.Quality = "standard"
			}
			if imageRequest.Size == "" {
				imageRequest.Size = "1024x1024"
			}
		} else if imageRequest.Model == "gpt-image-1" {
			if imageRequest.Quality == "" {
				imageRequest.Quality = "auto"
			}
		}

		//if imageRequest.Prompt == "" {
		//	return nil, errors.New("prompt is required")
		//}

		if imageRequest.N == nil || *imageRequest.N == 0 {
			imageRequest.N = common.GetPointer(uint(1))
		}
	}

	return imageRequest, nil
}

func GetAndValidateClaudeRequest(c *gin.Context) (textRequest *dto.ClaudeRequest, err error) {
	textRequest = &dto.ClaudeRequest{}
	err = common.UnmarshalBodyReusable(c, textRequest)
	if err != nil {
		return nil, err
	}
	if textRequest.Messages == nil || len(textRequest.Messages) == 0 {
		return nil, errors.New("field messages is required")
	}
	if textRequest.Model == "" {
		return nil, errors.New("field model is required")
	}
	if exceedsMaxTokensLimit(textRequest.MaxTokens, textRequest.MaxTokensToSample) {
		return nil, errors.New("max_tokens is invalid")
	}

	//if textRequest.Stream {
	//	relayInfo.IsStream = true
	//}

	return textRequest, nil
}

func GetAndValidateTextRequest(c *gin.Context, relayMode int) (*dto.GeneralOpenAIRequest, error) {
	textRequest := &dto.GeneralOpenAIRequest{}
	err := common.UnmarshalBodyReusable(c, textRequest)
	if err != nil {
		return nil, err
	}

	if relayMode == relayconstant.RelayModeModerations && textRequest.Model == "" {
		textRequest.Model = "text-moderation-latest"
	}
	if relayMode == relayconstant.RelayModeEmbeddings && textRequest.Model == "" {
		textRequest.Model = c.Param("model")
	}

	if exceedsMaxTokensLimit(textRequest.MaxTokens, textRequest.MaxCompletionTokens) {
		return nil, errors.New("max_tokens is invalid")
	}
	if textRequest.Model == "" {
		return nil, errors.New("model is required")
	}
	if textRequest.WebSearchOptions != nil {
		if textRequest.WebSearchOptions.SearchContextSize != "" {
			validSizes := map[string]bool{
				"high":   true,
				"medium": true,
				"low":    true,
			}
			if !validSizes[textRequest.WebSearchOptions.SearchContextSize] {
				return nil, errors.New("invalid search_context_size, must be one of: high, medium, low")
			}
		} else {
			textRequest.WebSearchOptions.SearchContextSize = "medium"
		}
	}
	switch relayMode {
	case relayconstant.RelayModeCompletions:
		if textRequest.Prompt == "" {
			return nil, errors.New("field prompt is required")
		}
	case relayconstant.RelayModeChatCompletions:
		// For FIM (Fill-in-the-middle) requests with prefix/suffix, messages is optional
		// It will be filled by provider-specific adaptors if needed (e.g., SiliconFlow)。Or it is allowed by model vendor(s) (e.g., DeepSeek)
		if len(textRequest.Messages) == 0 && textRequest.Prefix == nil && textRequest.Suffix == nil {
			return nil, errors.New("field messages is required")
		}
	case relayconstant.RelayModeEmbeddings:
	case relayconstant.RelayModeModerations:
		if textRequest.Input == nil || textRequest.Input == "" {
			return nil, errors.New("field input is required")
		}
	case relayconstant.RelayModeEdits:
		if textRequest.Instruction == "" {
			return nil, errors.New("field instruction is required")
		}
	}
	return textRequest, nil
}

func GetAndValidateGeminiRequest(c *gin.Context) (*dto.GeminiChatRequest, error) {
	request := &dto.GeminiChatRequest{}
	err := common.UnmarshalBodyReusable(c, request)
	if err != nil {
		return nil, err
	}
	if len(request.Contents) == 0 && len(request.Requests) == 0 {
		return nil, errors.New("contents is required")
	}
	if exceedsMaxTokensLimit(request.GenerationConfig.MaxOutputTokens) {
		return nil, errors.New("maxOutputTokens is invalid")
	}

	//if c.Query("alt") == "sse" {
	//	relayInfo.IsStream = true
	//}

	return request, nil
}

func GetAndValidateGeminiEmbeddingRequest(c *gin.Context) (*dto.GeminiEmbeddingRequest, error) {
	request := &dto.GeminiEmbeddingRequest{}
	err := common.UnmarshalBodyReusable(c, request)
	if err != nil {
		return nil, err
	}
	return request, nil
}

func GetAndValidateGeminiBatchEmbeddingRequest(c *gin.Context) (*dto.GeminiBatchEmbeddingRequest, error) {
	request := &dto.GeminiBatchEmbeddingRequest{}
	err := common.UnmarshalBodyReusable(c, request)
	if err != nil {
		return nil, err
	}
	return request, nil
}
