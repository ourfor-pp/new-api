package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveMappedModelName(t *testing.T) {
	tests := []struct {
		name       string
		origin     string
		mapping    string
		wantModel  string
		wantMapped bool
		wantError  string
	}{
		{
			name:      "没有映射",
			origin:    "business-model",
			mapping:   "{}",
			wantModel: "business-model",
		},
		{
			name:       "单层映射",
			origin:     "任意业务别名",
			mapping:    `{"任意业务别名":"provider-model"}`,
			wantModel:  "provider-model",
			wantMapped: true,
		},
		{
			name:       "链式映射",
			origin:     "business-model",
			mapping:    `{"business-model":"middle-model","middle-model":"provider-model"}`,
			wantModel:  "provider-model",
			wantMapped: true,
		},
		{
			name:      "自身映射",
			origin:    "business-model",
			mapping:   `{"business-model":"business-model"}`,
			wantModel: "business-model",
		},
		{
			name:      "循环映射",
			origin:    "business-model",
			mapping:   `{"business-model":"middle-model","middle-model":"business-model"}`,
			wantError: "model_mapping_contains_cycle",
		},
		{
			name:      "损坏的配置",
			origin:    "business-model",
			mapping:   `{`,
			wantError: "unmarshal_model_mapping_failed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model, mapped, err := ResolveMappedModelName(test.origin, test.mapping)
			if test.wantError != "" {
				require.EqualError(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.wantModel, model)
			assert.Equal(t, test.wantMapped, mapped)
		})
	}
}

func TestEffectiveUpstreamModelName(t *testing.T) {
	assert.Equal(t, "business-model", (&RelayInfo{OriginModelName: "business-model"}).EffectiveUpstreamModelName())
	assert.Equal(t, "provider-model", (&RelayInfo{
		OriginModelName: "business-model",
		ChannelMeta:     &ChannelMeta{UpstreamModelName: "provider-model"},
	}).EffectiveUpstreamModelName())
}
