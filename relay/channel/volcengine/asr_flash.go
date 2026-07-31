package volcengine

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type volcASRFlashRequest struct {
	User    volcASRUser    `json:"user"`
	Audio   volcASRAudio   `json:"audio"`
	Request volcASRRequest `json:"request"`
}

type volcASRUser struct {
	UID string `json:"uid"`
}

type volcASRAudio struct {
	Data    string `json:"data"`
	Format  string `json:"format"`
	Channel *int   `json:"channel,omitempty"`
}

type volcASRRequest struct {
	ModelName            string         `json:"model_name"`
	EnableITN            *bool          `json:"enable_itn,omitempty"`
	EnablePunc           *bool          `json:"enable_punc,omitempty"`
	EnableDDC            *bool          `json:"enable_ddc,omitempty"`
	ShowUtterances       *bool          `json:"show_utterances,omitempty"`
	EnableSpeakerInfo    *bool          `json:"enable_speaker_info,omitempty"`
	EnableChannelSplit   *bool          `json:"enable_channel_split,omitempty"`
	SensitiveWordsFilter string         `json:"sensitive_words_filter,omitempty"`
	VADSegmentDuration   *int           `json:"vad_segment_duration,omitempty"`
	EndWindowSize        *int           `json:"end_window_size,omitempty"`
	Corpus               *volcASRCorpus `json:"corpus,omitempty"`
}

type volcASRCorpus struct {
	BoostingTableID string `json:"boosting_table_id,omitempty"`
	Context         string `json:"context,omitempty"`
}

type volcASRFlashResponse struct {
	AudioInfo *volcASRAudioInfo `json:"audio_info,omitempty"`
	Result    *volcASRResult    `json:"result,omitempty"`
}

type volcASRAudioInfo struct {
	Duration int64 `json:"duration"`
}

type volcASRResult struct {
	Text       string             `json:"text"`
	Utterances []volcASRUtterance `json:"utterances,omitempty"`
}

type volcASRUtterance struct {
	StartTime int64                   `json:"start_time"`
	EndTime   int64                   `json:"end_time"`
	Text      string                  `json:"text"`
	Words     []volcASRWord           `json:"words,omitempty"`
	Additions *volcASRResultAdditions `json:"additions,omitempty"`
}

type volcASRWord struct {
	StartTime  int64    `json:"start_time"`
	EndTime    int64    `json:"end_time"`
	Text       string   `json:"text"`
	Confidence *float64 `json:"confidence,omitempty"`
}

type volcASRResultAdditions struct {
	Speaker   json.RawMessage `json:"speaker,omitempty"`
	ChannelID json.RawMessage `json:"channel_id,omitempty"`
}

const volcASRAudioDataPlaceholder = "__NEW_API_VOLC_ASR_AUDIO_DATA__"

type volcASRRequestBody struct {
	*io.PipeReader
	contentLength int64
}

func (b *volcASRRequestBody) ContentLength() int64 {
	if b == nil {
		return 0
	}
	return b.contentLength
}

func buildVolcASRFlashRequestBody(c *gin.Context, request dto.AudioRequest, config *dto.VolcSpeechConfig) (*volcASRRequestBody, error) {
	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return nil, fmt.Errorf("failed to parse audio form: %w", err)
	}
	files := form.File["file"]
	if len(files) == 0 {
		_ = form.RemoveAll()
		return nil, errors.New("file is required")
	}
	fileHeader := files[0]
	if fileHeader.Size > 100*1024*1024 {
		_ = form.RemoveAll()
		return nil, errors.New("audio file must not exceed 100MB")
	}
	file, err := fileHeader.Open()
	if err != nil {
		_ = form.RemoveAll()
		return nil, fmt.Errorf("failed to open audio file: %w", err)
	}

	enableITN := true
	enablePunc := true
	enableDDC := true
	showUtterances := true
	var enableSpeakerInfo *bool
	var enableChannelSplit *bool
	var channel *int
	var sensitiveWordsFilter string
	var vadSegmentDuration *int
	var endWindowSize *int
	contextPayload := map[string]any{}
	if request.SpeechOptions != nil {
		if request.SpeechOptions.TextNormalization != nil {
			enableITN = *request.SpeechOptions.TextNormalization
		}
		if request.SpeechOptions.Punctuation != nil {
			enablePunc = *request.SpeechOptions.Punctuation
		}
		if request.SpeechOptions.SemanticSmoothing != nil {
			enableDDC = *request.SpeechOptions.SemanticSmoothing
		}
		enableSpeakerInfo = request.SpeechOptions.SpeakerDiarization
		if request.SpeechOptions.ChannelMode != "" {
			separate := request.SpeechOptions.ChannelMode == "separate"
			enableChannelSplit = &separate
			if separate {
				twoChannels := 2
				channel = &twoChannels
			}
		}
		if request.SpeechOptions.SensitiveWordFilter != nil {
			filter, marshalErr := common.Marshal(map[string]bool{
				"system_reserved_filter": *request.SpeechOptions.SensitiveWordFilter,
			})
			if marshalErr != nil {
				_ = file.Close()
				_ = form.RemoveAll()
				return nil, fmt.Errorf("failed to marshal sensitive word filter: %w", marshalErr)
			}
			sensitiveWordsFilter = string(filter)
		}
		if request.SpeechOptions.VADSegmentation != nil && *request.SpeechOptions.VADSegmentation {
			defaultDuration := 3000
			vadSegmentDuration = &defaultDuration
		}
		endWindowSize = request.SpeechOptions.ForceSegmentAfterMS
		if len(request.SpeechOptions.Hotwords) > 0 {
			hotwords := make([]map[string]string, 0, len(request.SpeechOptions.Hotwords))
			for _, hotword := range request.SpeechOptions.Hotwords {
				hotwords = append(hotwords, map[string]string{"word": hotword})
			}
			contextPayload["hotwords"] = hotwords
		}
		if len(request.SpeechOptions.Replacements) > 0 {
			contextPayload["correct_words"] = request.SpeechOptions.Replacements
		}
		if request.SpeechOptions.Context != nil && len(request.SpeechOptions.Context.Texts) > 0 {
			contextData := make([]map[string]string, 0, len(request.SpeechOptions.Context.Texts))
			for _, text := range request.SpeechOptions.Context.Texts {
				contextData = append(contextData, map[string]string{"text": text})
			}
			contextPayload["context_type"] = "dialog_ctx"
			contextPayload["context_data"] = contextData
		}
	}
	var corpus *volcASRCorpus
	hotwordTableID := ""
	if config != nil {
		hotwordTableID = strings.TrimSpace(config.ASRHotwordTableID)
	}
	if hotwordTableID != "" || len(contextPayload) > 0 {
		corpus = &volcASRCorpus{BoostingTableID: hotwordTableID}
		if len(contextPayload) > 0 {
			contextData, marshalErr := common.Marshal(contextPayload)
			if marshalErr != nil {
				_ = file.Close()
				_ = form.RemoveAll()
				return nil, fmt.Errorf("failed to marshal ASR context: %w", marshalErr)
			}
			corpus.Context = string(contextData)
		}
	}

	envelope := volcASRFlashRequest{
		User: volcASRUser{UID: "new-api-relay"},
		Audio: volcASRAudio{
			Data:    volcASRAudioDataPlaceholder,
			Format:  request.LocalAudioFormat,
			Channel: channel,
		},
		Request: volcASRRequest{
			ModelName:            "bigmodel",
			EnableITN:            &enableITN,
			EnablePunc:           &enablePunc,
			EnableDDC:            &enableDDC,
			ShowUtterances:       &showUtterances,
			EnableSpeakerInfo:    enableSpeakerInfo,
			EnableChannelSplit:   enableChannelSplit,
			SensitiveWordsFilter: sensitiveWordsFilter,
			VADSegmentDuration:   vadSegmentDuration,
			EndWindowSize:        endWindowSize,
			Corpus:               corpus,
		},
	}
	payload, err := common.Marshal(envelope)
	if err != nil {
		_ = file.Close()
		_ = form.RemoveAll()
		return nil, fmt.Errorf("failed to marshal volcengine ASR request: %w", err)
	}
	placeholderIndex := bytes.Index(payload, []byte(volcASRAudioDataPlaceholder))
	if placeholderIndex < 0 {
		_ = file.Close()
		_ = form.RemoveAll()
		return nil, errors.New("failed to build volcengine ASR streaming request")
	}
	prefix := payload[:placeholderIndex]
	suffix := payload[placeholderIndex+len(volcASRAudioDataPlaceholder):]
	contentLength := int64(len(prefix)) + int64(base64.StdEncoding.EncodedLen(int(fileHeader.Size))) + int64(len(suffix))

	pipeReader, pipeWriter := io.Pipe()
	go streamVolcASRRequest(pipeWriter, form, file, prefix, suffix)
	return &volcASRRequestBody{
		PipeReader:    pipeReader,
		contentLength: contentLength,
	}, nil
}

func streamVolcASRRequest(
	pipeWriter *io.PipeWriter,
	form *multipart.Form,
	file multipart.File,
	prefix []byte,
	suffix []byte,
) {
	defer file.Close()
	defer form.RemoveAll()

	if _, err := pipeWriter.Write(prefix); err != nil {
		_ = pipeWriter.CloseWithError(err)
		return
	}
	encoder := base64.NewEncoder(base64.StdEncoding, pipeWriter)
	if _, err := io.Copy(encoder, file); err != nil {
		_ = encoder.Close()
		_ = pipeWriter.CloseWithError(err)
		return
	}
	if err := encoder.Close(); err != nil {
		_ = pipeWriter.CloseWithError(err)
		return
	}
	if _, err := pipeWriter.Write(suffix); err != nil {
		_ = pipeWriter.CloseWithError(err)
		return
	}
	_ = pipeWriter.Close()
}

func volcASRBillingUnits(durationMS int64) int {
	if durationMS <= 0 {
		return 0
	}
	return common.QuotaRound(float64(durationMS) / 60000 * 1000)
}

func volcASRAdditionString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if err := common.Unmarshal(raw, &value); err != nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	default:
		return ""
	}
}

func volcASRAdditionChannel(raw json.RawMessage) *int {
	value := volcASRAdditionString(raw)
	if value == "" {
		return nil
	}
	channel, err := strconv.Atoi(value)
	if err != nil || channel < 0 {
		return nil
	}
	return &channel
}

func handleVolcASRFlashResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, responseFormat string) (any, *types.NewAPIError) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
	}
	if info.VolcSpeechAudit == nil {
		info.VolcSpeechAudit = &relaycommon.VolcSpeechAuditInfo{ResourceID: volcASRFlashResourceID, Protocol: volcASRFlashProtocol}
	}
	info.VolcSpeechAudit.LogID = strings.TrimSpace(resp.Header.Get("X-Tt-Logid"))
	setVolcSpeechAuditContext(c, info.VolcSpeechAudit)
	if info.VolcSpeechAudit.LogID != "" {
		c.Header("X-Volc-Logid", info.VolcSpeechAudit.LogID)
	}

	statusCode := strings.TrimSpace(resp.Header.Get("X-Api-Status-Code"))
	if statusCode == "" {
		return nil, types.NewErrorWithStatusCode(
			errors.New("volcengine ASR response missing X-Api-Status-Code"),
			types.ErrorCodeBadResponse,
			http.StatusBadGateway,
		)
	}
	if statusCode != "20000000" && statusCode != "20000003" {
		message := strings.TrimSpace(resp.Header.Get("X-Api-Message"))
		code, parseErr := strconv.Atoi(statusCode)
		if parseErr != nil {
			return nil, types.NewErrorWithStatusCode(
				errors.New("volcengine ASR response has invalid X-Api-Status-Code"),
				types.ErrorCodeBadResponse,
				http.StatusBadGateway,
			)
		}
		return nil, types.NewErrorWithStatusCode(
			volcSpeechProviderError("ASR", strconv.Itoa(code), info.VolcSpeechAudit.LogID),
			types.ErrorCodeBadResponse,
			volcSpeechProviderStatus(code, message),
		)
	}

	var result volcASRFlashResponse
	if err = common.Unmarshal(body, &result); err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	transcript := ""
	if result.Result != nil {
		transcript = result.Result.Text
	}
	durationMS := int64(0)
	if result.AudioInfo != nil {
		durationMS = result.AudioInfo.Duration
	}
	if durationMS <= 0 {
		if request, ok := info.Request.(*dto.AudioRequest); ok {
			durationMS = request.LocalAudioDurationMS
		}
	}
	if durationMS > 2*60*60*1000 {
		return nil, types.NewErrorWithStatusCode(errors.New("volcengine ASR returned duration above 2 hours"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	billingUnits := volcASRBillingUnits(durationMS)
	if billingUnits <= 0 {
		return nil, types.NewErrorWithStatusCode(errors.New("volcengine ASR returned no billable duration"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}

	switch strings.ToLower(responseFormat) {
	case "", "json":
		payload, marshalErr := common.Marshal(dto.AudioResponse{Text: transcript})
		if marshalErr != nil {
			return nil, types.NewErrorWithStatusCode(marshalErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		c.Data(http.StatusOK, gin.MIMEJSON, payload)
	case "text":
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(transcript))
	case "verbose_json":
		verbose := dto.WhisperVerboseJSONResponse{
			Task:     "transcribe",
			Duration: float64(durationMS) / 1000,
			Text:     transcript,
		}
		wantSegments := true
		wantWords := false
		if request, ok := info.Request.(*dto.AudioRequest); ok && len(request.TimestampGranularities) > 0 {
			wantSegments = false
			for _, granularity := range request.TimestampGranularities {
				if granularity == "segment" {
					wantSegments = true
				}
				if granularity == "word" {
					wantWords = true
				}
			}
		}
		if result.Result != nil {
			for index, utterance := range result.Result.Utterances {
				speaker := ""
				var channel *int
				if utterance.Additions != nil {
					speaker = volcASRAdditionString(utterance.Additions.Speaker)
					channel = volcASRAdditionChannel(utterance.Additions.ChannelID)
				}
				if wantSegments {
					verbose.Segments = append(verbose.Segments, dto.Segment{
						Id:      index,
						Start:   float64(utterance.StartTime) / 1000,
						End:     float64(utterance.EndTime) / 1000,
						Text:    utterance.Text,
						Speaker: speaker,
						Channel: channel,
					})
				}
				if wantWords {
					for _, word := range utterance.Words {
						verbose.Words = append(verbose.Words, dto.AudioWord{
							Word:       word.Text,
							Start:      float64(word.StartTime) / 1000,
							End:        float64(word.EndTime) / 1000,
							Confidence: word.Confidence,
							Speaker:    speaker,
							Channel:    channel,
						})
					}
				}
			}
		}
		payload, marshalErr := common.Marshal(verbose)
		if marshalErr != nil {
			return nil, types.NewErrorWithStatusCode(marshalErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		c.Data(http.StatusOK, gin.MIMEJSON, payload)
	case "srt", "vtt":
		cues := make([]volcSubtitleCue, 0)
		if result.Result != nil {
			cues = make([]volcSubtitleCue, 0, len(result.Result.Utterances))
			for _, utterance := range result.Result.Utterances {
				cues = append(cues, volcSubtitleCue{
					StartMS: utterance.StartTime,
					EndMS:   utterance.EndTime,
					Text:    utterance.Text,
				})
			}
		}
		if strings.EqualFold(responseFormat, "srt") {
			c.Data(http.StatusOK, "application/x-subrip; charset=utf-8", []byte(formatVolcSRT(cues)))
		} else {
			c.Data(http.StatusOK, "text/vtt; charset=utf-8", []byte(formatVolcVTT(cues)))
		}
	default:
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("unsupported response_format for doubao-seed-asr-flash: %s", responseFormat),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	info.VolcSpeechAudit.AudioDurationMS = durationMS
	info.VolcSpeechAudit.BillingUnits = billingUnits
	if result.Result != nil {
		info.VolcSpeechAudit.SubtitleSentenceCount = len(result.Result.Utterances)
		for _, utterance := range result.Result.Utterances {
			info.VolcSpeechAudit.SubtitleWordCount += len(utterance.Words)
		}
	}
	setVolcSpeechAuditContext(c, info.VolcSpeechAudit)
	logger.LogInfo(c, fmt.Sprintf(
		"火山语音请求完成: model=%s resource_id=%s protocol=%s log_id=%s audio_duration_ms=%d billing_units=%d timestamp_granularities=%v subtitle_formats=%v speech_options=%v context_text_count=%d hotword_count=%d replacement_count=%d subtitle_sentence_count=%d subtitle_word_count=%d",
		info.OriginModelName, volcASRFlashResourceID, volcASRFlashProtocol, info.VolcSpeechAudit.LogID, durationMS, billingUnits,
		info.VolcSpeechAudit.TimestampGranularities, info.VolcSpeechAudit.SubtitleFormats,
		info.VolcSpeechAudit.SpeechOptions, info.VolcSpeechAudit.ContextTextCount,
		info.VolcSpeechAudit.HotwordCount, info.VolcSpeechAudit.ReplacementCount,
		info.VolcSpeechAudit.SubtitleSentenceCount, info.VolcSpeechAudit.SubtitleWordCount,
	))
	return &dto.Usage{PromptTokens: billingUnits, TotalTokens: billingUnits}, nil
}
