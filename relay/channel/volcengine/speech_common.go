package volcengine

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	volcTTSV3URL           = "https://openspeech.bytedance.com/api/v3/tts/unidirectional"
	volcTTSResourceID      = "seed-tts-2.0"
	volcTTSProtocol        = "v3-http-chunked"
	volcASRFlashURL        = "https://openspeech.bytedance.com/api/v3/auc/bigmodel/recognize/flash"
	volcASRFlashResourceID = "volc.bigasr.auc_turbo"
	volcASRFlashProtocol   = "v3-http-flash"
)

type volcSpeechAuthKind string

const (
	volcSpeechAuthAPIKey volcSpeechAuthKind = "api_key"
	volcSpeechAuthLegacy volcSpeechAuthKind = "legacy"
)

func parseVolcSpeechCredential(apiKey string) (kind volcSpeechAuthKind, first string, second string, err error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return "", "", "", errors.New("empty volcengine speech credential")
	}
	parts := strings.Split(apiKey, "|")
	switch len(parts) {
	case 1:
		return volcSpeechAuthAPIKey, strings.TrimSpace(parts[0]), "", nil
	case 2:
		appID := strings.TrimSpace(parts[0])
		accessToken := strings.TrimSpace(parts[1])
		if appID == "" || accessToken == "" {
			return "", "", "", errors.New("invalid volcengine speech credential, expected appid|access_token")
		}
		return volcSpeechAuthLegacy, appID, accessToken, nil
	default:
		return "", "", "", errors.New("invalid volcengine speech credential, expected api_key or appid|access_token")
	}
}

func buildVolcSpeechHeaders(apiKey, resourceID, legacyAppHeader string) (http.Header, error) {
	kind, first, second, err := parseVolcSpeechCredential(apiKey)
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	if kind == volcSpeechAuthAPIKey {
		headers.Set("X-Api-Key", first)
	} else {
		headers.Set(legacyAppHeader, first)
		headers.Set("X-Api-Access-Key", second)
	}
	headers.Set("X-Api-Resource-Id", resourceID)
	headers.Set("X-Api-Request-Id", uuid.NewString())
	return headers, nil
}

func doVolcSpeechRequest(adaptor *Adaptor, c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	requestURL, err := adaptor.GetRequestURL(info)
	if err != nil {
		return nil, err
	}
	return doVolcSpeechRequestURL(adaptor, c, info, requestBody, requestURL)
}

func doVolcSpeechRequestURL(adaptor *Adaptor, c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader, requestURL string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, requestURL, requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to build volcengine speech request: %w", err)
	}
	if sizedBody, ok := requestBody.(interface{ ContentLength() int64 }); ok {
		request.ContentLength = sizedBody.ContentLength()
	}
	if err = adaptor.SetupRequestHeader(c, &request.Header, info); err != nil {
		_ = request.Body.Close()
		return nil, fmt.Errorf("failed to configure volcengine speech request: %w", err)
	}

	client, err := service.GetHttpClientWithProxy(info.ChannelSetting.Proxy)
	if err != nil {
		_ = request.Body.Close()
		return nil, fmt.Errorf("failed to create volcengine speech client: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("volcengine speech request failed: %w", err)
	}
	return response, nil
}

func setVolcSpeechAuditContext(c *gin.Context, audit *relaycommon.VolcSpeechAuditInfo) {
	if c == nil || audit == nil {
		return
	}
	c.Set("volc_speech_audit", audit.LogValue())
}

// volcSpeechProviderError keeps provider diagnostics useful without allowing an
// upstream message to copy request text or other sensitive content into logs.
func volcSpeechProviderError(operation, code, logID string) error {
	operation = strings.TrimSpace(operation)
	code = strings.TrimSpace(code)
	logID = strings.TrimSpace(logID)
	if logID == "" {
		return fmt.Errorf("volcengine %s failed: code=%s", operation, code)
	}
	return fmt.Errorf("volcengine %s failed: code=%s log_id=%s", operation, code, logID)
}
