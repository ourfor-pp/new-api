package volcengine

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
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
	Data   string `json:"data"`
	Format string `json:"format"`
}

type volcASRRequest struct {
	ModelName      string `json:"model_name"`
	EnableITN      bool   `json:"enable_itn"`
	EnablePunc     bool   `json:"enable_punc"`
	EnableDDC      bool   `json:"enable_ddc"`
	ShowUtterances bool   `json:"show_utterances"`
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
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
	Text      string `json:"text"`
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

func buildVolcASRFlashRequestBody(c *gin.Context, request dto.AudioRequest) (*volcASRRequestBody, error) {
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

	envelope := volcASRFlashRequest{
		User:  volcASRUser{UID: "new-api-relay"},
		Audio: volcASRAudio{Data: volcASRAudioDataPlaceholder, Format: request.LocalAudioFormat},
		Request: volcASRRequest{
			ModelName:      "bigmodel",
			EnableITN:      true,
			EnablePunc:     true,
			EnableDDC:      true,
			ShowUtterances: true,
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
		code := 0
		_, _ = fmt.Sscanf(statusCode, "%d", &code)
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("volcengine ASR failed: code=%s message=%s", statusCode, message),
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
		if result.Result != nil {
			for index, utterance := range result.Result.Utterances {
				verbose.Segments = append(verbose.Segments, dto.Segment{
					Id:    index,
					Start: float64(utterance.StartTime) / 1000,
					End:   float64(utterance.EndTime) / 1000,
					Text:  utterance.Text,
				})
			}
		}
		payload, marshalErr := common.Marshal(verbose)
		if marshalErr != nil {
			return nil, types.NewErrorWithStatusCode(marshalErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		c.Data(http.StatusOK, gin.MIMEJSON, payload)
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
	setVolcSpeechAuditContext(c, info.VolcSpeechAudit)
	logger.LogInfo(c, fmt.Sprintf(
		"火山语音请求完成: model=%s resource_id=%s protocol=%s log_id=%s audio_duration_ms=%d billing_units=%d",
		info.OriginModelName, volcASRFlashResourceID, volcASRFlashProtocol, info.VolcSpeechAudit.LogID, durationMS, billingUnits,
	))
	return &dto.Usage{PromptTokens: billingUnits, TotalTokens: billingUnits}, nil
}
