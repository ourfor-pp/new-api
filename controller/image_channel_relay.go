package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// imageChannelAttempt joins the two image execution paths at their retry
// boundary. Each owns its billing session and refunds a failed reservation
// before the next channel is selected. A durable task never crosses this boundary.
type imageChannelAttempt struct {
	remaining int
	index     int
	retry     bool
	err       *types.NewAPIError
	taskErr   *dto.TaskError
}

func RelayImageChannels(c *gin.Context) {
	modelName := c.GetString("original_model")
	retryParam := &service.RetryParam{
		Ctx: c, ModelName: modelName,
		TokenGroup:  common.GetContextKeyString(c, constant.ContextKeyTokenGroup),
		RequestPath: c.Request.URL.Path, Retry: common.GetPointer(0),
	}
	if retryParam.TokenGroup == "" {
		retryParam.TokenGroup = common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	}
	originalHeaders := c.Request.Header.Clone()
	originalLength := c.Request.ContentLength
	var attempt imageChannelAttempt
	for index := 0; retryParam.GetRetry() <= common.RetryTimes; index++ {
		// Native multipart conversion may replace the boundary in Content-Type.
		// Every provider must decode the original body with its original headers.
		c.Request.Header = originalHeaders.Clone()
		c.Request.ContentLength = originalLength
		attempt = imageChannelAttempt{index: index}
		if index > 0 {
			channel, _, selectErr := service.SelectChannelForRequest(c, modelName, retryParam)
			if selectErr != nil {
				message := selectErr.Message
				if selectErr.MessageID != "" {
					message = i18n.T(c, selectErr.MessageID, selectErr.Params)
				}
				respondPluginProtocolError(c, selectErr.StatusCode, string(selectErr.Code), message)
				return
			}
			if err := middleware.SetupContextForSelectedChannel(c, channel, modelName); err != nil {
				attempt.err = err
				break
			}
		}
		attempt.remaining = common.RetryTimes - retryParam.GetRetry()
		if _, exists := c.Get(pluginruntime.ContextKeyPinnedEndpoint); exists {
			middleware.PrepareImageChannelEndpoint(c)
			if c.IsAborted() {
				return
			}
			pinnedValue, _ := c.Get(pluginruntime.ContextKeyPinnedEndpoint)
			pinned := pinnedValue.(pluginruntime.PinnedEndpoint)
			deps := defaultPluginProtocolBridgeDeps()
			deps.submit = func(c *gin.Context, info *relaycommon.RelayInfo) (*taskSubmissionOutcome, *dto.TaskError) {
				attempt.prepareBilling(info)
				return executeTaskSubmissionAttempt(c, info, relay.RelayTaskSubmit, &attempt)
			}
			serveTaskPluginImageAttempt(c, pinned, deps, &attempt)
		} else {
			relayWithImageAttempt(c, types.RelayFormatOpenAIImage, &attempt)
		}
		if attempt.err == nil || c.Writer.Written() || c.Request.Context().Err() != nil {
			return
		}
		if !attempt.retry {
			break
		}
		retryParam.IncreaseRetry()
	}
	if attempt.taskErr != nil {
		respondTaskPluginImageError(c, attempt.taskErr)
	} else if attempt.err != nil {
		service.RecordRequestPolicyTermination(c, attempt.err)
		attempt.err.SetMessage(common.MessageWithRequestId(attempt.err.Error(), c.GetString(common.RequestIdKey)))
		c.JSON(attempt.err.StatusCode, gin.H{"error": attempt.err.ToOpenAIError(common.UpstreamAddresses(c)...)})
	}
}

func (attempt *imageChannelAttempt) refund(c *gin.Context, info *relaycommon.RelayInfo) {
	if billing, ok := info.Billing.(*service.BillingSession); ok {
		if err := billing.RefundBeforeRetry(c); err != nil {
			attempt.retry = false
			logger.LogError(c, common.LogText("image channel refund failed: %s", err.Error()))
		}
	}
}

func (attempt *imageChannelAttempt) prepareBilling(info *relaycommon.RelayInfo) {
	info.RetryIndex = attempt.index
	if attempt.index > 0 {
		// A refunded subscription reservation cannot be reopened. Give this
		// attempt its own billing id; client/log correlation stays on c.
		info.RequestId = common.NewRequestId()
	}
}
