package volcengine

import (
	"bufio"
	"bytes"
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
	Format     string `json:"format"`
	SampleRate int    `json:"sample_rate"`
	SpeechRate *int   `json:"speech_rate,omitempty"`
}

type volcTTSV3Result struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Usage   *volcTTSV3Usage `json:"usage,omitempty"`
}

type volcTTSV3Usage struct {
	TextWords int `json:"text_words"`
}

const volcTTSMaxFrameBytes = 16 << 20

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
	return volcTTSV3Request{
		User: volcTTSV3User{UID: "new-api-relay"},
		ReqParams: volcTTSV3ReqParams{
			Text:    request.Input,
			Speaker: speaker,
			AudioParams: volcTTSV3AudioParams{
				Format:     encoding,
				SampleRate: 24000,
				SpeechRate: speechRate,
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

func volcTTSStreamError(c *gin.Context, info *relaycommon.RelayInfo, wroteAudio bool, err error, status int) *types.NewAPIError {
	options := []types.NewAPIErrorOptions{}
	if wroteAudio {
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

	reader := bufio.NewReader(resp.Body)
	wroteAudio := false
	finished := false
	textWords := 0
	usagePresent := false
	for {
		message, readErr := readVolcTTSV3Frame(reader)
		if readErr != nil {
			if errors.Is(readErr, io.EOF) && finished {
				break
			}
			return nil, volcTTSStreamError(c, info, wroteAudio, fmt.Errorf("volcengine TTS stream interrupted: %w", readErr), http.StatusBadGateway)
		}
		switch message.MsgType {
		case MsgTypeAudioOnlyServer:
			if len(message.Payload) == 0 {
				continue
			}
			if !wroteAudio {
				c.Header("Content-Type", contentType)
				c.Header("Transfer-Encoding", "chunked")
			}
			written, writeErr := c.Writer.Write(message.Payload)
			if written > 0 {
				wroteAudio = true
				c.Writer.Flush()
			}
			if writeErr != nil {
				return nil, volcTTSStreamError(c, info, wroteAudio, fmt.Errorf("failed to write TTS audio: %w", writeErr), 499)
			}
			if written != len(message.Payload) {
				return nil, volcTTSStreamError(c, info, wroteAudio, io.ErrShortWrite, 499)
			}
		case MsgTypeError:
			return nil, volcTTSStreamError(
				c,
				info,
				wroteAudio,
				fmt.Errorf("volcengine TTS error frame: code=%d", message.ErrorCode),
				volcSpeechProviderStatus(int(message.ErrorCode), string(message.Payload)),
			)
		case MsgTypeFullServerResponse:
			if message.EventType != EventType_SessionFinished && message.EventType != EventType_SessionFailed &&
				message.EventType != EventType_ConnectionFailed {
				continue
			}
			var result volcTTSV3Result
			if unmarshalErr := common.Unmarshal(message.Payload, &result); unmarshalErr != nil {
				return nil, volcTTSStreamError(c, info, wroteAudio, fmt.Errorf("invalid volcengine TTS result: %w", unmarshalErr), http.StatusBadGateway)
			}
			if message.EventType != EventType_SessionFinished || (result.Code != 0 && result.Code != 20000000) {
				return nil, volcTTSStreamError(
					c,
					info,
					wroteAudio,
					fmt.Errorf("volcengine TTS failed: code=%d message=%s", result.Code, result.Message),
					volcSpeechProviderStatus(result.Code, result.Message),
				)
			}
			finished = true
			if result.Usage != nil {
				usagePresent = true
				textWords = result.Usage.TextWords
			}
		}
		if finished {
			break
		}
	}

	if !wroteAudio {
		return nil, volcTTSStreamError(c, info, false, errors.New("volcengine TTS completed without audio"), http.StatusBadGateway)
	}
	if !finished {
		return nil, volcTTSStreamError(c, info, true, errors.New("volcengine TTS stream ended before SessionFinished"), http.StatusBadGateway)
	}
	requestTextWords := 0
	if request, ok := info.Request.(*dto.AudioRequest); ok {
		requestTextWords = utf8.RuneCountInString(request.Input)
	}
	if !usagePresent {
		textWords = requestTextWords
	}
	if usagePresent && (textWords <= 0 || textWords > requestTextWords) {
		return nil, volcTTSStreamError(c, info, true, errors.New("volcengine TTS returned invalid text_words usage"), http.StatusBadGateway)
	}
	if textWords <= 0 {
		if request, ok := info.Request.(*dto.AudioRequest); ok {
			textWords = utf8.RuneCountInString(request.Input)
		}
	}
	if textWords <= 0 {
		return nil, volcTTSStreamError(c, info, true, errors.New("volcengine TTS returned no billable usage"), http.StatusBadGateway)
	}

	info.VolcSpeechAudit.TextWords = textWords
	info.VolcSpeechAudit.BillingUnits = textWords
	setVolcSpeechAuditContext(c, info.VolcSpeechAudit)
	logger.LogInfo(c, fmt.Sprintf(
		"火山语音请求完成: model=%s resource_id=%s protocol=%s log_id=%s text_words=%d billing_units=%d",
		info.OriginModelName, volcTTSResourceID, volcTTSProtocol, info.VolcSpeechAudit.LogID, textWords, textWords,
	))
	return &dto.Usage{PromptTokens: textWords, TotalTokens: textWords}, nil
}
