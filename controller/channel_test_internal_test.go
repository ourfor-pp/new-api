package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	hostdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	kittypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetChannelDefaultBaseURLsUsesBuiltInDefaults(t *testing.T) {
	originalBaseURLs := constant.ChannelBaseURLs
	constant.ChannelBaseURLs = append([]string(nil), originalBaseURLs...)
	constant.ChannelBaseURLs[constant.ChannelTypeDeepSeek] = "https://deepseek.server.example"
	t.Cleanup(func() {
		constant.ChannelBaseURLs = originalBaseURLs
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/channel/default_base_urls", nil)
	GetChannelDefaultBaseURLs(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool           `json:"success"`
		Data    map[int]string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, "https://deepseek.server.example", response.Data[constant.ChannelTypeDeepSeek])
	assert.Equal(t, "https://api.openai.com", response.Data[constant.ChannelTypeOpenAI])
	assert.NotContains(t, response.Data, constant.ChannelTypeAzure)
	assert.NotContains(t, response.Data, constant.ChannelTypeNewAPI)
	assert.NotContains(t, response.Data, constant.ChannelTypeTaskPlugin)
}

func TestMappedVolcASRChannelTestUsesAudioProtocol(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	user := model.User{Id: 98765, Username: "speech-channel-test", Group: "default", Status: common.UserStatusEnabled, Quota: 1000000}
	require.NoError(t, db.Create(&user).Error)
	savedRatios, err := common.Marshal(ratio_setting.GetModelRatioCopy())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(savedRatios))) })
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"speech-test":1}`))
	if service.GetHttpClient() == nil {
		service.InitHttpClient()
	}
	client := service.GetHttpClient()
	savedTransport := client.Transport
	t.Cleanup(func() { client.Transport = savedTransport })
	for _, resourceID := range []string{constant.ModelVolcASRFlash, constant.ModelVolcASR20} {
		t.Run(resourceID, func(t *testing.T) {
			called := false
			client.Transport = speechChannelTestTransport(func(request *http.Request) (*http.Response, error) {
				called = true
				assert.Equal(t, "/api/v3/auc/bigmodel/recognize/flash", request.URL.Path)
				assert.Equal(t, resourceID, request.Header.Get("X-Api-Resource-Id"))
				var payload struct {
					Audio struct {
						Data string `json:"data"`
					} `json:"audio"`
				}
				require.NoError(t, common.DecodeJson(request.Body, &payload))
				assert.NotEmpty(t, payload.Audio.Data)
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Api-Status-Code": {"20000000"}}, Body: io.NopCloser(strings.NewReader(`{"audio_info":{"duration":1000},"result":{"text":"test"}}`))}, nil
			})
			mapping, err := common.Marshal(map[string]string{"speech-test": resourceID})
			require.NoError(t, err)
			channel := &model.Channel{Id: 17, Type: constant.ChannelTypeVolcEngine, Key: "test-key", Status: common.ChannelStatusEnabled, Group: "default", Models: "speech-test", ModelMapping: common.GetPointer(string(mapping))}
			result := testChannel(context.Background(), channel, user.Id, "speech-test", "", true)
			require.NoError(t, result.localErr)
			require.Nil(t, result.newAPIError)
			assert.True(t, called)
			assert.Equal(t, "/v1/audio/transcriptions", result.context.Request.URL.Path)
		})
	}
}

func TestValidateChannelProxy(t *testing.T) {
	tests := []struct {
		name    string
		proxy   string
		wantErr bool
	}{
		{name: "empty"},
		{name: "http", proxy: "http://proxy.example:8080"},
		{name: "https", proxy: "https://proxy.example:8443"},
		{name: "socks5", proxy: "socks5://proxy.example"},
		{name: "socks5h", proxy: "socks5h://proxy.example:1080/"},
		{name: "unsupported", proxy: "ftp://proxy.example", wantErr: true},
		{name: "path", proxy: "socks5://proxy.example:1080/path", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setting, err := common.Marshal(dto.ChannelSettings{Proxy: test.proxy})
			require.NoError(t, err)
			channel := &model.Channel{
				Type:    constant.ChannelTypeOpenAI,
				Setting: common.GetPointer(string(setting)),
			}

			err = validateChannel(channel, false)

			if test.wantErr {
				require.ErrorContains(t, err, "invalid channel proxy")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateChannelRequiresNewAPIBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL *string
		wantErr bool
	}{
		{name: "missing", wantErr: true},
		{name: "blank", baseURL: common.GetPointer("  "), wantErr: true},
		{name: "configured", baseURL: common.GetPointer("https://new-api.example")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := &model.Channel{
				Type:    constant.ChannelTypeNewAPI,
				BaseURL: test.baseURL,
			}

			err := validateChannel(channel, false)

			if test.wantErr {
				require.ErrorContains(t, err, "New API channel base URL cannot be empty")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestNewAPIChannelRegistration(t *testing.T) {
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeNewAPI)

	require.True(t, ok)
	assert.Equal(t, constant.APITypeNewAPI, apiType)
	assert.Equal(t, "New API", constant.GetChannelTypeName(constant.ChannelTypeNewAPI))
	require.Greater(t, len(constant.ChannelBaseURLs), constant.ChannelTypeNewAPI)
	assert.Empty(t, constant.ChannelBaseURLs[constant.ChannelTypeNewAPI])
}

func TestResponsesCompactChannelSupport(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		apiType     int
		want        bool
	}{
		{name: "OpenAI", channelType: constant.ChannelTypeOpenAI, apiType: constant.APITypeOpenAI, want: true},
		{name: "Azure", channelType: constant.ChannelTypeAzure, apiType: constant.APITypeOpenAI, want: true},
		{name: "Codex", channelType: constant.ChannelTypeCodex, apiType: constant.APITypeCodex, want: true},
		{name: "Advanced Custom", channelType: constant.ChannelTypeAdvancedCustom, apiType: constant.APITypeAdvancedCustom, want: true},
		{name: "Sub2API", channelType: constant.ChannelTypeSub2API, apiType: constant.APITypeSub2API, want: true},
		{name: "New API", channelType: constant.ChannelTypeNewAPI, apiType: constant.APITypeNewAPI, want: true},
		{name: "Anthropic", channelType: constant.ChannelTypeAnthropic, apiType: constant.APITypeAnthropic, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, common.SupportsResponsesCompact(test.channelType, test.apiType))
		})
	}
}

func TestMultiprotocolGatewayEndpointTypes(t *testing.T) {
	want := []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
		constant.EndpointTypeOpenAIResponseCompact,
		constant.EndpointTypeAnthropic,
		constant.EndpointTypeGemini,
		constant.EndpointTypeOpenAIAlphaSearch,
	}

	assert.Equal(t, want, common.GetEndpointTypesByChannelType(constant.ChannelTypeNewAPI, "gpt-5"))
	assert.Equal(t, want, common.GetEndpointTypesByChannelType(constant.ChannelTypeSub2API, "gpt-5"))
}

func TestCopyChannelRejectsInvalidLegacyProxySettings(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	settingBytes, err := common.Marshal(dto.ChannelSettings{
		Proxy: "socks5://proxy.example/legacy-path",
	})
	require.NoError(t, err)
	setting := string(settingBytes)
	origin := &model.Channel{
		Type:    constant.ChannelTypeOpenAI,
		Name:    "legacy proxy channel",
		Key:     "test-key",
		Models:  "gpt-test",
		Group:   "default",
		Setting: &setting,
	}
	require.NoError(t, db.Create(origin).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", origin.Id)}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/copy", nil)

	CopyChannel(ctx)

	assert.Contains(t, recorder.Body.String(), "invalid channel settings")
	var channelCount int64
	require.NoError(t, db.Model(&model.Channel{}).Count(&channelCount).Error)
	assert.Equal(t, int64(1), channelCount)
}

func TestDeleteChannelResetsProxyCacheWhenPreReadFails(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	service.ResetProxyClientCache()
	t.Cleanup(service.ResetProxyClientCache)

	proxyURL := "http://proxy.example:8080"
	beforeDelete, err := service.GetHttpClientWithProxy(proxyURL)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "999999"}}
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/999999", nil)

	DeleteChannel(ctx)

	assert.Contains(t, recorder.Body.String(), `"success":true`)
	afterDelete, err := service.GetHttpClientWithProxy(proxyURL)
	require.NoError(t, err)
	assert.NotSame(t, beforeDelete, afterDelete)
}

func TestDeleteChannelBatchReportsAndAuditsActualDeletedCount(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	channel := &model.Channel{Name: "existing", Key: "test-key"}
	require.NoError(t, db.Create(channel).Error)

	requestBody, err := common.Marshal(ChannelBatch{Ids: []int{channel.Id, 999999}})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/batch", bytes.NewReader(requestBody))
	ctx.Request.Header.Set("Content-Type", "application/json")

	DeleteChannelBatch(ctx)

	var response struct {
		Success bool  `json:"success"`
		Data    int64 `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, int64(1), response.Data)

	var auditLog model.AuditLog
	require.NoError(t, db.Order("id desc").First(&auditLog).Error)
	var auditData struct {
		Operation struct {
			Params map[string]any `json:"params"`
		} `json:"op"`
	}
	encodedAudit, err := common.Marshal(auditLog.Other)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(encodedAudit, &auditData))
	assert.Equal(t, float64(1), auditData.Operation.Params["count"])
}

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  common.QuotaPerUnit,
			ExprVersion:   1,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	requestRules := []billingexpr.RequestRuleTrace{{
		Cond:       `param("service_tier") == "fast"`,
		Multiplier: 2,
		Matched:    true,
	}}
	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier:  "base",
		RequestRules: requestRules,
	})

	fields := other.Snapshot()
	require.Equal(t, "tiered_expr", fields["billing_mode"])
	require.Equal(t, "base", fields["matched_tier"])
	require.Equal(t, requestRules, fields["request_rules"])
	require.NotEmpty(t, fields["expr_b64"])
}

func TestResolveChannelTestUserIDUsesRequestUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 2)

	userID, err := resolveChannelTestUserID(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, userID)
}

func TestSelectChannelsForAutomaticTestPassiveRecoveryOnlyUsesAutoDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModePassiveRecovery)

	require.Len(t, selected, 1)
	require.Equal(t, 2, selected[0].Id)
}

func TestSelectChannelsForAutomaticTestScheduledSkipsManualDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeScheduledAll)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 2, selected[1].Id)
}

func TestSelectChannelsForAutomaticTestAutoBanOnlyUsesEligibleChannels(t *testing.T) {
	autoBanEnabled := 1
	autoBanDisabled := 0
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled, AutoBan: &autoBanEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled, AutoBan: &autoBanDisabled},
		{Id: 3, Status: common.ChannelStatusAutoDisabled, AutoBan: &autoBanEnabled},
		{Id: 4, Status: common.ChannelStatusManuallyDisabled, AutoBan: &autoBanEnabled},
		{Id: 5, Status: common.ChannelStatusEnabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeAutoBanOnly)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 3, selected[1].Id)
}

func TestRunChannelTestWorkersHonorsConfiguredConcurrency(t *testing.T) {
	originalInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() { common.RequestInterval = originalInterval })

	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled},
		{Id: 3, Status: common.ChannelStatusEnabled},
		{Id: 4, Status: common.ChannelStatusEnabled},
	}
	started := make(chan struct{}, len(channels))
	release := make(chan struct{})
	var active atomic.Int32
	var maxActive atomic.Int32
	progress := make([]int, 0, len(channels)+1)
	summaryResult := make(chan channelTestSummary, 1)

	go func() {
		summaryResult <- runChannelTestWorkers(
			context.Background(),
			channels,
			2,
			func(_ context.Context, _ *model.Channel) channelTestSummary {
				current := active.Add(1)
				defer active.Add(-1)
				for {
					observed := maxActive.Load()
					if current <= observed || maxActive.CompareAndSwap(observed, current) {
						break
					}
				}
				started <- struct{}{}
				<-release
				return channelTestSummary{Tested: 1, Succeeded: 1}
			},
			func(processed, _ int) {
				progress = append(progress, processed)
			},
		)
	}()

	<-started
	<-started
	select {
	case <-started:
		t.Fatal("started more channel tests than the configured concurrency")
	default:
	}
	close(release)

	summary := <-summaryResult

	assert.Equal(t, int32(2), maxActive.Load())
	assert.Equal(t, channelTestSummary{Tested: 4, Succeeded: 4}, summary)
	assert.Equal(t, []int{0, 1, 2, 3, 4}, progress)
}

func TestRunChannelTestWorkersStopsAfterCancellation(t *testing.T) {
	originalInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() { common.RequestInterval = originalInterval })

	ctx, cancel := context.WithCancel(context.Background())
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled},
		{Id: 3, Status: common.ChannelStatusEnabled},
		{Id: 4, Status: common.ChannelStatusEnabled},
	}
	started := make(chan struct{}, len(channels))
	progress := make([]int, 0, 1)
	summaryResult := make(chan channelTestSummary, 1)

	go func() {
		summaryResult <- runChannelTestWorkers(
			ctx,
			channels,
			2,
			func(ctx context.Context, _ *model.Channel) channelTestSummary {
				started <- struct{}{}
				<-ctx.Done()
				return channelTestSummary{Tested: 1, Succeeded: 1}
			},
			func(processed, _ int) {
				progress = append(progress, processed)
			},
		)
	}()

	<-started
	<-started
	cancel()

	summary := <-summaryResult

	select {
	case <-started:
		t.Fatal("started another channel test after cancellation")
	default:
	}
	assert.Equal(t, channelTestSummary{Tested: 2, Succeeded: 2}, summary)
	assert.Equal(t, []int{0}, progress)
}

func TestBuildVolcSpeechChannelTestRequests(t *testing.T) {
	ttsRequest, ok := buildTestRequest(constant.ModelDoubaoSeedTTS20, "", nil, false).(*dto.AudioRequest)
	require.True(t, ok)
	assert.Equal(t, "alloy", ttsRequest.Voice)
	assert.Equal(t, "mp3", ttsRequest.ResponseFormat)
	assert.NotEmpty(t, ttsRequest.Input)

	for _, resourceID := range []string{constant.ModelVolcASRFlash, constant.ModelVolcASR20} {
		asrRequest, ok := buildTestRequest(resourceID, "", nil, false).(*dto.AudioRequest)
		require.True(t, ok)
		assert.Equal(t, "wav", asrRequest.LocalAudioFormat)
		assert.Equal(t, int64(200), asrRequest.LocalAudioDurationMS)
		assert.Equal(t, resourceID, asrRequest.Model)
	}

	body, contentType, err := buildVolcASRChannelTestBody(constant.ModelVolcASRFlash)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body))
	request.Header.Set("Content-Type", contentType)
	require.NoError(t, request.ParseMultipartForm(1<<20))
	file, _, err := request.FormFile("file")
	require.NoError(t, err)
	defer file.Close()
	wav, err := io.ReadAll(file)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(wav), 44)
	assert.Equal(t, "RIFF", string(wav[:4]))
	assert.Equal(t, "WAVE", string(wav[8:12]))
	assert.Equal(t, constant.ModelVolcASRFlash, request.FormValue("model"))
}

func TestShouldRetryHonorsForceRetryWithoutChangingDefaultGatewayTimeout(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)

	defaultError := kittypes.NewErrorWithStatusCode(fmt.Errorf("gateway timeout"), kittypes.ErrorCodeBadResponse, http.StatusGatewayTimeout)
	assert.False(t, service.ShouldRetryRelayError(context, defaultError, 1))

	forcedError := kittypes.NewErrorWithStatusCode(
		fmt.Errorf("gateway timeout"),
		kittypes.ErrorCodeBadResponse,
		http.StatusGatewayTimeout,
		kittypes.ErrOptionWithForceRetry(),
	)
	assert.True(t, service.ShouldRetryRelayError(context, forcedError, 1))
	assert.False(t, service.ShouldRetryRelayError(context, forcedError, 0))

	service.GetChannelConstraints(context).AddPin(hostdto.ChannelPin{ChannelId: 1, Source: hostdto.PinSourceToken, Rank: hostdto.PinRankToken, RetryMode: hostdto.PinRetrySingleAttempt})
	assert.False(t, service.ShouldRetryRelayError(context, forcedError, 1))
}

func TestTestAllChannelsRejectsExistingActiveTask(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))

	existing, err := model.CreateSystemTask(model.SystemTaskTypeChannelTest, nil, nil)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/test", nil)

	TestAllChannels(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), existing.TaskID)
	require.Contains(t, recorder.Body.String(), "已有通道测试任务正在运行或等待中")
}

type speechChannelTestTransport func(*http.Request) (*http.Response, error)

func (f speechChannelTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMappedVolcChannelTestSendsAudioProtocolWithBusinessModel(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	user := model.User{Username: "speech-review-fixture", Group: "default", Quota: 1000000, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	ratios, err := common.Marshal(ratio_setting.GetModelRatioCopy())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(ratios))) })
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"customer-tts":1,"customer-asr":1}`))
	if service.GetHttpClient() == nil {
		service.InitHttpClient()
	}
	client := service.GetHttpClient()
	originalTransport := client.Transport
	t.Cleanup(func() { client.Transport = originalTransport })
	for _, tc := range []struct{ alias, upstream, path, upstreamPath string }{
		{"customer-tts", constant.ModelDoubaoSeedTTS20, "/v1/audio/speech", "/api/v3/tts/unidirectional"},
		{"customer-asr", constant.ModelVolcASRFlash, "/v1/audio/transcriptions", "/api/v3/auc/bigmodel/recognize/flash"},
		{"customer-asr", constant.ModelVolcASR20, "/v1/audio/transcriptions", "/api/v3/auc/bigmodel/recognize/flash"},
	} {
		t.Run(tc.alias, func(t *testing.T) {
			channel := &model.Channel{Id: 1001, Type: constant.ChannelTypeVolcEngine, Key: "synthetic-key", Models: tc.alias, Group: "default",
				ModelMapping:  common.GetPointer(fmt.Sprintf(`{"%s":"speech-intermediate","speech-intermediate":"%s"}`, tc.alias, tc.upstream)),
				OtherSettings: `{"volc_speech":{"default_tts_speaker":"fixture-speaker"}}`}
			calls := 0
			client.Transport = speechChannelTestTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				assert.Equal(t, tc.upstreamPath, r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				data, readErr := io.ReadAll(r.Body)
				require.NoError(t, readErr)
				require.NoError(t, r.Body.Close())
				var payload map[string]any
				require.NoError(t, common.Unmarshal(data, &payload))
				if tc.alias == "customer-tts" {
					require.Contains(t, payload, "req_params")
				} else {
					require.Contains(t, payload, "audio")
				}
				assert.NotContains(t, payload, "messages")
				return nil, errors.New("fixture stops before provider execution")
			})
			result := testChannel(context.Background(), channel, user.Id, tc.alias, "", true)
			require.ErrorContains(t, result.localErr, "fixture stops before provider execution")
			assert.Equal(t, 1, calls)
			require.NotNil(t, result.context)
			defer common.CleanupBodyStorage(result.context)
			assert.Equal(t, tc.path, result.context.Request.URL.Path)
			assert.Equal(t, tc.alias, common.GetContextKeyString(result.context, constant.ContextKeyOriginalModel))
			if tc.alias == "customer-asr" {
				assert.True(t, strings.HasPrefix(result.context.Request.Header.Get("Content-Type"), "multipart/form-data;"))
				form, parseErr := common.ParseMultipartFormReusable(result.context)
				require.NoError(t, parseErr)
				defer form.RemoveAll()
				assert.Equal(t, []string{tc.alias}, form.Value["model"])
				require.Len(t, form.File["file"], 1)
			}
		})
	}
}
