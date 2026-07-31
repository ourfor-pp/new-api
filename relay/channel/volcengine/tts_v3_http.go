package volcengine

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type volcTTSV3Request struct {
	User      volcTTSV3User      `json:"user"`
	ReqParams volcTTSV3ReqParams `json:"req_params"`
}

type volcTTSV3User struct {
	UID string `json:"uid"`
}

type volcTTSV3ReqParams struct {
	Text        string               `json:"text"`
	Speaker     string               `json:"speaker"`
	AudioParams volcTTSV3AudioParams `json:"audio_params"`
}

type volcTTSV3AudioParams struct {
	Format         string `json:"format"`
	SampleRate     int    `json:"sample_rate"`
	SpeechRate     *int   `json:"speech_rate,omitempty"`
	EnableSubtitle *bool  `json:"enable_subtitle,omitempty"`
}

type volcTTSV3Result struct {
	Code     int                `json:"code"`
	Message  string             `json:"message"`
	Data     string             `json:"data,omitempty"`
	Sentence *volcTTSV3Sentence `json:"sentence,omitempty"`
	Usage    *volcTTSV3Usage    `json:"usage,omitempty"`
}

type volcTTSV3Usage struct {
	TextWords int `json:"text_words"`
}

type volcTTSV3Sentence struct {
	Text  string          `json:"text"`
	Words []volcTTSV3Word `json:"words,omitempty"`
}

type volcTTSV3Word struct {
	Word       string   `json:"word"`
	StartTime  float64  `json:"startTime"`
	EndTime    float64  `json:"endTime"`
	Confidence *float64 `json:"confidence,omitempty"`
}

type volcTTSSSEAudioDelta struct {
	Type  string `json:"type"`
	Audio string `json:"audio"`
}

type volcTTSSSESubtitleDelta struct {
	Type      string          `json:"type"`
	Index     int             `json:"index"`
	Text      string          `json:"text"`
	StartTime *float64        `json:"startTime,omitempty"`
	EndTime   *float64        `json:"endTime,omitempty"`
	Words     []volcTTSV3Word `json:"words,omitempty"`
}

type volcTTSSSESubtitleDone struct {
	Type          string            `json:"type"`
	Available     bool              `json:"available"`
	SentenceCount int               `json:"sentence_count"`
	WordCount     int               `json:"word_count"`
	Formats       map[string]string `json:"formats,omitempty"`
}

type volcTTSSSEAudioDone struct {
	Type  string          `json:"type"`
	Usage volcTTSSSEUsage `json:"usage"`
}

type volcTTSSSEUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type volcTTSSSEError struct {
	Type  string                  `json:"type"`
	Error volcTTSSSEErrorContents `json:"error"`
}

type volcTTSSSEErrorContents struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

const volcTTSMaxFrameBytes = 16 << 20

var volcTTSMaxJSONLineBytes = base64.StdEncoding.EncodedLen(volcTTSMaxFrameBytes) + 64*1024

func resolveVolcTTSSpeaker(voice string, config *dto.VolcSpeechConfig) (string, error) {
	voice = strings.TrimSpace(voice)
	if _, standard := openAIToVolcengineVoiceMap[strings.ToLower(voice)]; !standard {
		return voice, nil
	}
	if config == nil || strings.TrimSpace(config.DefaultTTSSpeaker) == "" {
		return "", types.NewError(
			errors.New("OpenAI standard voice requires volc_speech.default_tts_speaker on the channel"),
			types.ErrorCodeChannelConfigInvalid,
		)
	}
	return strings.TrimSpace(config.DefaultTTSSpeaker), nil
}

func volcTTSV3Encoding(responseFormat string) (string, string, error) {
	switch strings.ToLower(responseFormat) {
	case "", "mp3":
		return "mp3", "audio/mpeg", nil
	case "opus":
		return "ogg_opus", "audio/ogg", nil
	case "ogg_opus":
		return "ogg_opus", "audio/ogg", nil
	case "pcm":
		return "pcm", "audio/pcm", nil
	default:
		return "", "", fmt.Errorf("unsupported response_format for doubao-seed-tts-2.0: %s", responseFormat)
	}
}

func volcTTSV3SpeechRate(speed *float64) (*int, error) {
	if speed == nil {
		return nil, nil
	}
	if *speed < 0.5 || *speed > 2.0 {
		return nil, errors.New("speed must be between 0.5 and 2.0")
	}
	rate := int(math.Round((*speed - 1) * 100))
	return &rate, nil
}

func buildVolcTTSV3Request(request dto.AudioRequest, config *dto.VolcSpeechConfig) (volcTTSV3Request, string, error) {
	speaker, err := resolveVolcTTSSpeaker(request.Voice, config)
	if err != nil {
		return volcTTSV3Request{}, "", err
	}
	encoding, _, err := volcTTSV3Encoding(request.ResponseFormat)
	if err != nil {
		return volcTTSV3Request{}, "", err
	}
	speechRate, err := volcTTSV3SpeechRate(request.Speed)
	if err != nil {
		return volcTTSV3Request{}, "", err
	}
	var enableSubtitle *bool
	if len(request.TimestampGranularities) > 0 || len(request.SubtitleFormats) > 0 {
		enabled := true
		enableSubtitle = &enabled
	}
	return volcTTSV3Request{
		User: volcTTSV3User{UID: "new-api-relay"},
		ReqParams: volcTTSV3ReqParams{
			Text:    request.Input,
			Speaker: speaker,
			AudioParams: volcTTSV3AudioParams{
				Format:         encoding,
				SampleRate:     24000,
				SpeechRate:     speechRate,
				EnableSubtitle: enableSubtitle,
			},
		},
	}, encoding, nil
}

func readVolcTTSV3Frame(reader io.Reader) (*Message, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}
	headerSize := int(header[0]&0x0f) * 4
	if headerSize < 4 {
		return nil, fmt.Errorf("invalid volcengine v3 frame header size: %d", headerSize)
	}

	frame := bytes.NewBuffer(header)
	if padding := headerSize - 4; padding > 0 {
		if _, err := io.CopyN(frame, reader, int64(padding)); err != nil {
			return nil, err
		}
	}

	msgType := MsgType(header[1] >> 4)
	flag := MsgTypeFlagBits(header[1] & 0x0f)
	if flag == MsgTypeFlagWithEvent {
		eventBytes := make([]byte, 4)
		if _, err := io.ReadFull(reader, eventBytes); err != nil {
			return nil, err
		}
		frame.Write(eventBytes)
		event := EventType(int32(uint32(eventBytes[0])<<24 | uint32(eventBytes[1])<<16 | uint32(eventBytes[2])<<8 | uint32(eventBytes[3])))
		if !isVolcConnectionEvent(event) {
			if err := copyVolcLengthPrefixed(reader, frame); err != nil {
				return nil, err
			}
		}
		if isVolcConnectionResponseEvent(event) {
			if err := copyVolcLengthPrefixed(reader, frame); err != nil {
				return nil, err
			}
		}
	}

	switch msgType {
	case MsgTypeFullClientRequest, MsgTypeFullServerResponse, MsgTypeFrontEndResultServer, MsgTypeAudioOnlyClient, MsgTypeAudioOnlyServer:
		if flag == MsgTypeFlagPositiveSeq || flag == MsgTypeFlagNegativeSeq {
			if _, err := io.CopyN(frame, reader, 4); err != nil {
				return nil, err
			}
		}
	case MsgTypeError:
		if _, err := io.CopyN(frame, reader, 4); err != nil {
			return nil, err
		}
	}
	if err := copyVolcLengthPrefixed(reader, frame); err != nil {
		return nil, err
	}
	return NewMessageFromBytes(frame.Bytes())
}

func copyVolcLengthPrefixed(reader io.Reader, destination *bytes.Buffer) error {
	sizeBytes := make([]byte, 4)
	if _, err := io.ReadFull(reader, sizeBytes); err != nil {
		return err
	}
	size := uint32(sizeBytes[0])<<24 | uint32(sizeBytes[1])<<16 | uint32(sizeBytes[2])<<8 | uint32(sizeBytes[3])
	frameSize := uint64(destination.Len()) + uint64(len(sizeBytes)) + uint64(size)
	if frameSize > volcTTSMaxFrameBytes {
		return fmt.Errorf("volcengine v3 frame exceeds %d bytes", volcTTSMaxFrameBytes)
	}
	destination.Write(sizeBytes)
	if size == 0 {
		return nil
	}
	_, err := io.CopyN(destination, reader, int64(size))
	return err
}

func isVolcConnectionEvent(event EventType) bool {
	switch event {
	case EventType_StartConnection, EventType_FinishConnection, EventType_ConnectionStarted,
		EventType_ConnectionFailed, EventType_ConnectionFinished:
		return true
	default:
		return false
	}
}

func isVolcConnectionResponseEvent(event EventType) bool {
	switch event {
	case EventType_ConnectionStarted, EventType_ConnectionFailed, EventType_ConnectionFinished:
		return true
	default:
		return false
	}
}

func volcSpeechProviderStatus(code int, message string) int {
	if strings.Contains(strings.ToLower(message), "quota") || strings.Contains(strings.ToLower(message), "concurr") {
		return http.StatusTooManyRequests
	}
	if code >= 55000000 {
		return http.StatusBadGateway
	}
	if code >= 45000000 {
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}

func writeVolcTTSSSEEvent(c *gin.Context, event any) (bool, error) {
	payload, err := common.Marshal(event)
	if err != nil {
		return false, err
	}
	relayhelper.SetEventStreamHeaders(c)
	data := append([]byte("data: "), payload...)
	data = append(data, '\n', '\n')
	written, writeErr := c.Writer.Write(data)
	wroteOutput := written > 0
	if wroteOutput {
		if flushErr := relayhelper.FlushWriter(c); flushErr != nil && writeErr == nil {
			writeErr = flushErr
		}
	}
	if writeErr != nil {
		return wroteOutput, writeErr
	}
	if written != len(data) {
		return wroteOutput, io.ErrShortWrite
	}
	return wroteOutput, nil
}

func volcTTSStreamError(c *gin.Context, info *relaycommon.RelayInfo, wroteAudio bool, err error, status int) *types.NewAPIError {
	options := []types.NewAPIErrorOptions{}
	if wroteAudio {
		if request, ok := info.Request.(*dto.AudioRequest); ok && strings.EqualFold(request.StreamFormat, "sse") {
			_, _ = writeVolcTTSSSEEvent(c, volcTTSSSEError{
				Type: "error",
				Error: volcTTSSSEErrorContents{
					Message: err.Error(),
					Code:    "bad_response",
				},
			})
		}
		if info.VolcSpeechAudit != nil {
			info.VolcSpeechAudit.PartialFailure = true
			info.VolcSpeechAudit.BillingUnits = 0
			setVolcSpeechAuditContext(c, info.VolcSpeechAudit)
		}
		options = append(options, types.ErrOptionWithSkipRetry())
	}
	return types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponse, status, options...)
}

func handleVolcTTSV3Response(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, encoding string) (any, *types.NewAPIError) {
	defer resp.Body.Close()

	_, contentType, formatErr := volcTTSV3Encoding(encoding)
	if formatErr != nil {
		return nil, types.NewErrorWithStatusCode(formatErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if info.VolcSpeechAudit == nil {
		info.VolcSpeechAudit = &relaycommon.VolcSpeechAuditInfo{ResourceID: volcTTSResourceID, Protocol: volcTTSProtocol}
	}
	info.VolcSpeechAudit.LogID = strings.TrimSpace(resp.Header.Get("X-Tt-Logid"))
	setVolcSpeechAuditContext(c, info.VolcSpeechAudit)
	if info.VolcSpeechAudit.LogID != "" {
		c.Header("X-Volc-Logid", info.VolcSpeechAudit.LogID)
	}

	request, _ := info.Request.(*dto.AudioRequest)
	isSSE := request != nil && strings.EqualFold(request.StreamFormat, "sse")
	subtitleRequested := request != nil &&
		(len(request.TimestampGranularities) > 0 || len(request.SubtitleFormats) > 0)
	wantSegments := false
	wantWords := false
	wantJSON := false
	wantSRT := false
	wantVTT := false
	if request != nil {
		for _, granularity := range request.TimestampGranularities {
			if granularity == "segment" {
				wantSegments = true
			}
			if granularity == "word" {
				wantWords = true
			}
		}
		for _, subtitleFormat := range request.SubtitleFormats {
			switch subtitleFormat {
			case "json":
				wantJSON = true
			case "srt":
				wantSRT = true
			case "vtt":
				wantVTT = true
			}
		}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), volcTTSMaxJSONLineBytes)
	wroteAudio := false
	finished := false
	textWords := 0
	usagePresent := false
	sentences := make([]volcTTSV3Sentence, 0)
	sentenceIndexes := make(map[string]int)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var result volcTTSV3Result
		if unmarshalErr := common.Unmarshal(line, &result); unmarshalErr != nil {
			return nil, volcTTSStreamError(c, info, wroteAudio, fmt.Errorf("invalid volcengine TTS JSON chunk: %w", unmarshalErr), http.StatusBadGateway)
		}
		if result.Code != 0 && result.Code != 20000000 {
			return nil, volcTTSStreamError(
				c,
				info,
				wroteAudio,
				fmt.Errorf("volcengine TTS failed: code=%d message=%s", result.Code, result.Message),
				volcSpeechProviderStatus(result.Code, result.Message),
			)
		}

		if result.Data != "" {
			if base64.StdEncoding.DecodedLen(len(result.Data)) > volcTTSMaxFrameBytes {
				return nil, volcTTSStreamError(
					c,
					info,
					wroteAudio,
					fmt.Errorf("volcengine TTS audio chunk exceeds %d bytes", volcTTSMaxFrameBytes),
					http.StatusBadGateway,
				)
			}
			audio, decodeErr := base64.StdEncoding.DecodeString(result.Data)
			if decodeErr != nil {
				return nil, volcTTSStreamError(c, info, wroteAudio, fmt.Errorf("invalid volcengine TTS base64 audio: %w", decodeErr), http.StatusBadGateway)
			}
			if len(audio) == 0 {
				continue
			}
			if isSSE {
				wrote, writeErr := writeVolcTTSSSEEvent(c, volcTTSSSEAudioDelta{
					Type:  "speech.audio.delta",
					Audio: result.Data,
				})
				wroteAudio = wroteAudio || wrote
				if writeErr != nil {
					return nil, volcTTSStreamError(c, info, wroteAudio, fmt.Errorf("failed to write TTS SSE audio: %w", writeErr), 499)
				}
			} else {
				if !wroteAudio {
					c.Header("Content-Type", contentType)
					c.Header("Transfer-Encoding", "chunked")
				}
				written, writeErr := c.Writer.Write(audio)
				if written > 0 {
					wroteAudio = true
					c.Writer.Flush()
				}
				if writeErr != nil {
					return nil, volcTTSStreamError(c, info, wroteAudio, fmt.Errorf("failed to write TTS audio: %w", writeErr), 499)
				}
				if written != len(audio) {
					return nil, volcTTSStreamError(c, info, wroteAudio, io.ErrShortWrite, 499)
				}
			}
		}

		if subtitleRequested && result.Sentence != nil &&
			strings.TrimSpace(result.Sentence.Text) != "" && len(result.Sentence.Words) > 0 {
			firstWord := result.Sentence.Words[0]
			lastWord := result.Sentence.Words[len(result.Sentence.Words)-1]
			key := fmt.Sprintf(
				"%s\x00%.9f\x00%.9f",
				result.Sentence.Text,
				firstWord.StartTime,
				lastWord.EndTime,
			)
			sentence := volcTTSV3Sentence{
				Text:  result.Sentence.Text,
				Words: append([]volcTTSV3Word(nil), result.Sentence.Words...),
			}
			if index, exists := sentenceIndexes[key]; exists {
				sentences[index] = sentence
			} else {
				sentenceIndexes[key] = len(sentences)
				sentences = append(sentences, sentence)
			}
		}

		if result.Code == 20000000 {
			finished = true
			if result.Usage != nil {
				usagePresent = true
				textWords = result.Usage.TextWords
			}
			break
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return nil, volcTTSStreamError(c, info, wroteAudio, fmt.Errorf("volcengine TTS stream interrupted: %w", scanErr), http.StatusBadGateway)
	}

	if !wroteAudio {
		return nil, volcTTSStreamError(c, info, false, errors.New("volcengine TTS completed without audio"), http.StatusBadGateway)
	}
	if !finished {
		return nil, volcTTSStreamError(c, info, true, errors.New("volcengine TTS stream ended before SessionFinished"), http.StatusBadGateway)
	}
	requestTextWords := 0
	if request != nil {
		requestTextWords = utf8.RuneCountInString(request.Input)
	}
	usageSource := "upstream"
	if !usagePresent {
		textWords = requestTextWords
		usageSource = "fallback_unicode"
	}
	if usagePresent && (textWords <= 0 || textWords > requestTextWords) {
		return nil, volcTTSStreamError(c, info, true, errors.New("volcengine TTS returned invalid text_words usage"), http.StatusBadGateway)
	}
	if textWords <= 0 {
		if request != nil {
			textWords = utf8.RuneCountInString(request.Input)
			usageSource = "fallback_unicode"
		}
	}
	if textWords <= 0 {
		return nil, volcTTSStreamError(c, info, true, errors.New("volcengine TTS returned no billable usage"), http.StatusBadGateway)
	}

	info.VolcSpeechAudit.TextWords = textWords
	info.VolcSpeechAudit.BillingUnits = textWords
	info.VolcSpeechAudit.UsageSource = usageSource
	info.VolcSpeechAudit.SubtitleSentenceCount = len(sentences)
	for _, sentence := range sentences {
		info.VolcSpeechAudit.SubtitleWordCount += len(sentence.Words)
	}
	setVolcSpeechAuditContext(c, info.VolcSpeechAudit)

	if isSSE {
		if subtitleRequested {
			cues := make([]volcSubtitleCue, 0, len(sentences))
			for index, sentence := range sentences {
				firstWord := sentence.Words[0]
				lastWord := sentence.Words[len(sentence.Words)-1]
				if wantJSON {
					delta := volcTTSSSESubtitleDelta{
						Type:  "sxh.speech.subtitle.delta",
						Index: index,
						Text:  sentence.Text,
					}
					if wantSegments {
						startTime := firstWord.StartTime
						endTime := lastWord.EndTime
						delta.StartTime = &startTime
						delta.EndTime = &endTime
					}
					if wantWords {
						delta.Words = append([]volcTTSV3Word(nil), sentence.Words...)
					}
					wrote, writeErr := writeVolcTTSSSEEvent(c, delta)
					wroteAudio = wroteAudio || wrote
					if writeErr != nil {
						return nil, volcTTSStreamError(c, info, wroteAudio, fmt.Errorf("failed to write TTS subtitle event: %w", writeErr), 499)
					}
				}
				cues = append(cues, volcSubtitleCue{
					StartMS: int64(math.Round(firstWord.StartTime * 1000)),
					EndMS:   int64(math.Round(lastWord.EndTime * 1000)),
					Text:    sentence.Text,
				})
			}
			formats := make(map[string]string)
			if wantSRT {
				formats["srt"] = formatVolcSRT(cues)
			}
			if wantVTT {
				formats["vtt"] = formatVolcVTT(cues)
			}
			wrote, writeErr := writeVolcTTSSSEEvent(c, volcTTSSSESubtitleDone{
				Type:          "sxh.speech.subtitle.done",
				Available:     len(sentences) > 0,
				SentenceCount: len(sentences),
				WordCount:     info.VolcSpeechAudit.SubtitleWordCount,
				Formats:       formats,
			})
			wroteAudio = wroteAudio || wrote
			if writeErr != nil {
				return nil, volcTTSStreamError(c, info, wroteAudio, fmt.Errorf("failed to write TTS subtitle completion: %w", writeErr), 499)
			}
		}
		wrote, writeErr := writeVolcTTSSSEEvent(c, volcTTSSSEAudioDone{
			Type: "speech.audio.done",
			Usage: volcTTSSSEUsage{
				InputTokens: textWords,
				TotalTokens: textWords,
			},
		})
		wroteAudio = wroteAudio || wrote
		if writeErr != nil {
			return nil, volcTTSStreamError(c, info, wroteAudio, fmt.Errorf("failed to write TTS SSE completion: %w", writeErr), 499)
		}
	}

	logger.LogInfo(c, fmt.Sprintf(
		"火山语音请求完成: model=%s resource_id=%s protocol=%s log_id=%s text_words=%d billing_units=%d timestamp_granularities=%v subtitle_formats=%v subtitle_sentence_count=%d subtitle_word_count=%d usage_source=%s",
		info.OriginModelName, volcTTSResourceID, volcTTSProtocol, info.VolcSpeechAudit.LogID, textWords, textWords,
		info.VolcSpeechAudit.TimestampGranularities, info.VolcSpeechAudit.SubtitleFormats,
		info.VolcSpeechAudit.SubtitleSentenceCount, info.VolcSpeechAudit.SubtitleWordCount, usageSource,
	))
	return &dto.Usage{PromptTokens: textWords, TotalTokens: textWords}, nil
}
