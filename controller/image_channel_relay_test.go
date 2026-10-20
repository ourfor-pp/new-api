package controller

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	jsplugin "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestImageChannelsPreserveRoutingAndBilling(t *testing.T) {
	require.NoError(t, i18n.Init())
	service.InitHttpClient()
	for _, tc := range []struct {
		name                                     string
		firstPlugin, failFirst                   bool
		pin, retries, wantStatus                 int
		wantCalls                                []string
		multipart, subscription, acceptedFailure bool
		refundFailure, streamFailure             bool
		legacy, origin                           bool
	}{
		{name: "native_priority", retries: 1, wantStatus: 200, wantCalls: []string{"native"}},
		{name: "plugin_priority", firstPlugin: true, retries: 1, wantStatus: 200, wantCalls: []string{"plugin"}},
		{name: "native_pin", firstPlugin: true, pin: 1, retries: 1, wantStatus: 200, wantCalls: []string{"native"}},
		{name: "plugin_pin", pin: 2, retries: 1, wantStatus: 200, wantCalls: []string{"plugin"}},
		{name: "native_to_plugin", failFirst: true, retries: 1, wantStatus: 200, wantCalls: []string{"native", "plugin"}},
		{name: "plugin_to_native", firstPlugin: true, failFirst: true, retries: 1, wantStatus: 200, wantCalls: []string{"plugin", "native"}},
		{name: "native_pin_no_retry", failFirst: true, pin: 1, retries: 1, wantStatus: 503, wantCalls: []string{"native"}},
		{name: "plugin_pin_no_retry", firstPlugin: true, failFirst: true, pin: 2, retries: 1, wantStatus: 503, wantCalls: []string{"plugin"}},
		{name: "native_budget", failFirst: true, wantStatus: 503, wantCalls: []string{"native"}},
		{name: "plugin_budget", firstPlugin: true, failFirst: true, wantStatus: 503, wantCalls: []string{"plugin"}},
		{name: "multipart_native_to_plugin", failFirst: true, retries: 1, wantStatus: 200, wantCalls: []string{"native", "plugin"}, multipart: true},
		{name: "multipart_plugin_to_native", firstPlugin: true, failFirst: true, retries: 1, wantStatus: 200, wantCalls: []string{"plugin", "native"}, multipart: true},
		{name: "subscription_native_to_plugin", failFirst: true, retries: 1, wantStatus: 200, wantCalls: []string{"native", "plugin"}, subscription: true},
		{name: "subscription_plugin_to_native", firstPlugin: true, failFirst: true, retries: 1, wantStatus: 200, wantCalls: []string{"plugin", "native"}, subscription: true},
		{name: "accepted_task_failure", firstPlugin: true, retries: 1, wantStatus: 400, wantCalls: []string{"plugin"}, acceptedFailure: true},
		{name: "subscription_refund_failure", failFirst: true, retries: 1, wantStatus: 503, wantCalls: []string{"native"}, subscription: true, refundFailure: true},
		{name: "accepted_stream_failure", firstPlugin: true, retries: 1, wantStatus: 502, wantCalls: []string{"plugin"}, streamFailure: true},
		{name: "legacy_native_to_plugin", failFirst: true, retries: 1, wantStatus: 200, wantCalls: []string{"native", "plugin"}, legacy: true},
		{name: "legacy_plugin_to_native", firstPlugin: true, failFirst: true, retries: 1, wantStatus: 200, wantCalls: []string{"plugin", "native"}, legacy: true},
		{name: "origin_channel_before_billing", firstPlugin: true, retries: 1, wantStatus: 200, wantCalls: []string{"plugin"}, origin: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, dialect := openTaskDialectDatabase(t, &model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.Task{}, &model.Log{}, &model.SubscriptionPlan{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{})
			oldDB, oldLogDB := model.DB, model.LOG_DB
			oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
			oldRedis, oldMemory, oldBatch, oldConsume, oldExport, oldRetry, oldError := common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled, common.RetryTimes, constant.ErrorLogEnabled
			model.DB, model.LOG_DB = db, db
			common.SetDatabaseTypes(dialect, dialect)
			common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled, common.RetryTimes, constant.ErrorLogEnabled = false, true, false, true, false, tc.retries, false
			t.Cleanup(func() {
				model.DB, model.LOG_DB = oldDB, oldLogDB
				common.SetDatabaseTypes(oldMain, oldLog)
				common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled, common.RetryTimes, constant.ErrorLogEnabled = oldRedis, oldMemory, oldBatch, oldConsume, oldExport, oldRetry, oldError
			})
			withTieredBillingConfig(t, map[string]string{"shared-image": "tiered_expr"}, map[string]string{"shared-image": `tier("request", fixed(0.01))`})
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.plugin_billing_expr": `{"mixed-image::shared-image":"0.006"}`}))
			if tc.legacy {
				previousPrice := ratio_setting.ModelPrice2JSONString()
				t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrice)) })
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"shared-image":0.01}`))
				require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_mode": "{}", "billing_setting.billing_expr": "{}", "billing_setting.plugin_billing_expr": "{}"}))
			}

			source := `
export const meta={apiVersion:1,key:"mixed-image",name:"Mixed image",version:"1.0.0",author:{name:"Test"},models:["vendor-image"],fetchMode:"per_task",protocols:["openai_image"]};
export const protocols={openai_image:{decodeRequest:function(ctx){return {kind:"submit",model:ctx.model,originTaskIds:ctx.body.kind==="json"?(ctx.body.value.origin_task_ids||[]):[],requestBody:ctx.body.kind==="multipart"?{prompt:ctx.body.fields.prompt[0],reference:ctx.body.files[0].ref,input_fidelity:ctx.body.fields.input_fidelity[0]}:ctx.body.value};},render:function(ctx,task){return task.data;}}};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/plugin",body:{channel_key:ctx.apiKey,model:ctx.upstreamModel,prompt:ctx.requestBody.prompt,reference:ctx.requestBody.reference?{__fileRef:ctx.requestBody.reference,encoding:"base64"}:null,input_fidelity:ctx.requestBody.input_fidelity}};}
export function parseSubmitResponse(ctx,resp){return {taskId:"upstream",taskData:resp.body,immediate:{status:resp.body.failed?"FAILURE":"SUCCESS",reason:"provider rejected image"}};}
export function buildQueryRequest(){throw new Error("unexpected polling");}
export function parseTaskResult(){throw new Error("unexpected polling");}`
			if tc.streamFailure {
				source = strings.Replace(source, `protocols:["openai_image"]`, `protocols:["openai_image"],submitResponseTypes:["json","sse"]`, 1)
				source = strings.Replace(source, `url:ctx.baseUrl+"/plugin",body:`, `url:ctx.baseUrl+"/plugin",responseType:"sse",body:`, 1)
				source += `
export function parseSubmitEvent(){throw new Error("invalid accepted stream");}`
			}
			_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister("mixed-image")) })
			calls := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				kind := "native"
				wantModel := "gpt-image-1"
				if r.URL.Path == "/plugin" {
					kind = "plugin"
					wantModel = "vendor-image"
				}
				calls = append(calls, kind)
				var body map[string]any
				if tc.multipart && kind == "native" {
					require.NoError(t, r.ParseMultipartForm(1<<20))
					body = map[string]any{"model": r.FormValue("model"), "prompt": r.FormValue("prompt")}
					file, _, err := r.FormFile("image")
					if assert.NoError(t, err) {
						data, err := io.ReadAll(file)
						assert.NoError(t, err)
						assert.Equal(t, "original image bytes", string(data))
						_ = file.Close()
					}
					assert.Empty(t, r.FormValue("input_fidelity"))
				} else {
					assert.NoError(t, common.DecodeJson(r.Body, &body))
					if tc.multipart {
						assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("original image bytes")), body["reference"])
						assert.Equal(t, "high", body["input_fidelity"])
					}
				}
				assert.Equal(t, wantModel, body["model"])
				assert.Equal(t, "original prompt", body["prompt"])
				if tc.origin {
					assert.Equal(t, "fixture-2", body["channel_key"], "origin task must select credentials before submission")
				}
				w.Header().Set("Content-Type", "application/json")
				if tc.failFirst && len(calls) == 1 {
					w.WriteHeader(503)
					_, _ = w.Write([]byte(`{"error":{"message":"temporarily unavailable","type":"upstream_error"}}`))
					return
				}
				if tc.streamFailure {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {}\n\n")
					return
				}
				if tc.acceptedFailure {
					_, _ = w.Write([]byte(`{"failed":true}`))
					return
				}
				_, _ = w.Write([]byte(`{"created":123,"data":[{"b64_json":"aW1hZ2U="}]}`))
			}))
			defer server.Close()
			const initial = 6000
			user := model.User{Username: "mixed-user", AffCode: "mixed", Group: "default", Quota: initial, Setting: `{"billing_preference":"wallet_only"}`}
			if tc.subscription {
				user.Setting = `{"billing_preference":"subscription_only"}`
			}
			require.NoError(t, db.Create(&user).Error)
			var subscription model.UserSubscription
			if tc.subscription {
				plan := model.SubscriptionPlan{Title: "image quota", QuotaResetPeriod: model.SubscriptionResetNever}
				require.NoError(t, db.Create(&plan).Error)
				subscription = model.UserSubscription{UserId: user.Id, PlanId: plan.Id, Status: "active", StartTime: time.Now().Unix(), EndTime: time.Now().Add(time.Hour).Unix(), AmountTotal: initial}
				require.NoError(t, db.Create(&subscription).Error)
			}
			if tc.refundFailure {
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("image-refund-failure", func(tx *gorm.DB) {
					if record, ok := tx.Statement.Model.(*model.SubscriptionPreConsumeRecord); ok && record.Status == "refunded" {
						tx.AddError(errors.New("injected refund record failure"))
					}
				}))
			}
			token := model.Token{UserId: user.Id, Key: strings.Repeat("x", 48), Name: "mixed", RemainQuota: initial, Status: common.TokenStatusEnabled}
			require.NoError(t, db.Create(&token).Error)
			channelCount := 2
			if tc.origin {
				channelCount = 3
				require.NoError(t, db.Create(&model.Task{TaskID: "task_origin", UserId: user.Id, ChannelId: 2, Platform: "mixed-image", Status: model.TaskStatusSuccess}).Error)
			}
			for i := 1; i <= channelCount; i++ {
				typ := constant.ChannelTypeOpenAI
				mapping := `{"shared-image":"gpt-image-1"}`
				priority := int64(20)
				if i >= 2 {
					typ = constant.ChannelTypeTaskPlugin
					mapping = `{"shared-image":"vendor-image"}`
				}
				if (i == 2) == tc.firstPlugin {
					priority = 30
				}
				if i == 3 {
					priority = 40
				}
				setting := `{"task_plugin_key":"mixed-image"}`
				ch := model.Channel{Id: i, Name: fmt.Sprint(i), Type: typ, Key: fmt.Sprintf("fixture-%d", i), Status: common.ChannelStatusEnabled, Models: "shared-image", Group: "default", Priority: &priority, BaseURL: &server.URL, ModelMapping: &mapping, Setting: &setting, AutoBan: common.GetPointer(0)}
				if tc.multipart && i == 1 {
					override := `{"operations":[{"mode":"delete","path":"input_fidelity"}]}`
					ch.ParamOverride = &override
				}
				require.NoError(t, ch.Insert())
			}
			model.InitChannelCache()
			router := gin.New()
			path := "/v1/images/generations"
			if tc.multipart {
				path = "/v1/images/edits"
			}
			router.POST(path, func(c *gin.Context) {
				c.Set(common.RequestIdKey, "image-test-correlation")
				defer common.CleanupBodyStorage(c)
				common.SetContextKey(c, constant.ContextKeyUserId, user.Id)
				(&model.UserBase{Id: user.Id, Username: user.Username, Group: "default", Quota: initial, Setting: user.Setting}).WriteContext(c)
				common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
				common.SetContextKey(c, constant.ContextKeyTokenId, token.Id)
				common.SetContextKey(c, constant.ContextKeyTokenKey, token.Key)
				if tc.pin != 0 {
					service.GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: tc.pin, Source: dto.PinSourceToken, Rank: dto.PinRankToken, RetryMode: dto.PinRetrySingleAttempt})
				}
				c.Next()
			}, middleware.ImageChannelRouting(), middleware.Distribute(), RelayImageChannels)
			requestBody := []byte(`{"model":"shared-image","prompt":"original prompt","size":"1024x1024","n":1}`)
			if tc.origin {
				requestBody = []byte(`{"model":"shared-image","prompt":"original prompt","n":1,"origin_task_ids":["task_origin"]}`)
			}
			contentType := "application/json"
			if tc.multipart {
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				for _, field := range [][2]string{{"model", "shared-image"}, {"prompt", "original prompt"}, {"size", "1024x1024"}, {"input_fidelity", "high"}} {
					require.NoError(t, writer.WriteField(field[0], field[1]))
				}
				file, err := writer.CreateFormFile("image", "input.png")
				require.NoError(t, err)
				_, err = io.WriteString(file, "original image bytes")
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				requestBody = body.Bytes()
				contentType = writer.FormDataContentType()
			}
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(requestBody))
			req.Header.Set("Content-Type", contentType)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			assert.Equal(t, tc.wantCalls, calls)
			wantQuota := 0
			if tc.wantStatus == 200 {
				price := 0.01
				if !tc.legacy && tc.wantCalls[len(tc.wantCalls)-1] == "plugin" {
					price = 0.006
				}
				wantQuota = common.QuotaRound(price * common.QuotaPerUnit)
			}
			var updatedUser model.User
			var updatedToken model.Token
			require.NoError(t, db.First(&updatedUser, user.Id).Error)
			require.NoError(t, db.First(&updatedToken, token.Id).Error)
			walletQuota := wantQuota
			if tc.subscription {
				walletQuota = 0
				require.NoError(t, db.First(&subscription, subscription.Id).Error)
				if tc.refundFailure {
					assert.Equal(t, int64(5000), subscription.AmountUsed)
				} else {
					assert.Equal(t, int64(wantQuota), subscription.AmountUsed)
				}
				var reservations []model.SubscriptionPreConsumeRecord
				require.NoError(t, db.Order("id").Find(&reservations).Error)
				if tc.refundFailure {
					require.Len(t, reservations, 1)
					assert.Equal(t, "consumed", reservations[0].Status, "failed refund must roll back both writes")
					require.NoError(t, db.Callback().Update().Remove("image-refund-failure"))
					require.NoError(t, model.RefundSubscriptionPreConsume(reservations[0].RequestId))
					require.NoError(t, model.RefundSubscriptionPreConsume(reservations[0].RequestId))
					require.NoError(t, db.First(&subscription, subscription.Id).Error)
					assert.Zero(t, subscription.AmountUsed, "recovery refund must be idempotent")
				} else {
					require.Len(t, reservations, 2)
					assert.Equal(t, "refunded", reservations[0].Status)
					assert.Equal(t, "consumed", reservations[1].Status)
					assert.NotEqual(t, reservations[0].RequestId, reservations[1].RequestId)
				}
			}
			assert.Equal(t, initial-walletQuota, updatedUser.Quota)
			assert.Equal(t, wantQuota, updatedUser.UsedQuota)
			assert.Equal(t, initial-wantQuota, updatedToken.RemainQuota)
			assert.Equal(t, wantQuota, updatedToken.UsedQuota)
			var logs []model.Log
			require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
			if wantQuota > 0 {
				require.Len(t, logs, 1)
				assert.Equal(t, wantQuota, logs[0].Quota)
				assert.Equal(t, "shared-image", logs[0].ModelName)
				assert.Equal(t, "image-test-correlation", logs[0].RequestId)
			} else if tc.acceptedFailure {
				require.Len(t, logs, 1)
				assert.Zero(t, logs[0].Quota)
			} else {
				assert.Empty(t, logs)
			}
		})
	}
}
