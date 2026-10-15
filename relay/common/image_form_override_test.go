package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageFormDeletes(t *testing.T) {
	for _, tc := range []struct {
		name, model, path string
		deleted           bool
	}{
		{"matched", "gpt-image-2", "/v1/images/edits", true},
		{"other model", "gpt-image-1", "/v1/images/edits", false},
		{"other endpoint", "gpt-image-2", "/v1/images/generations", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &RelayInfo{ChannelMeta: &ChannelMeta{UpstreamModelName: tc.model, ParamOverride: map[string]any{
				"operations": []any{map[string]any{"mode": "delete", "path": "input_fidelity", "logic": "AND", "conditions": []any{
					map[string]any{"path": "upstream_model", "mode": "full", "value": "gpt-image-2"},
					map[string]any{"path": "request_path", "mode": "full", "value": "/v1/images/edits"},
				}}},
			}}, RequestURLPath: tc.path}
			original := map[string][]string{"model": {tc.model}, "input_fidelity": {"high", "low"}, "tag": {"a", "b"}}
			got, err := ApplyImageFormDeletes(original, info)
			require.NoError(t, err)
			_, exists := got["input_fidelity"]
			assert.Equal(t, !tc.deleted, exists)
			assert.Equal(t, []string{"high", "low"}, original["input_fidelity"])
			assert.Equal(t, []string{"a", "b"}, got["tag"])
			if tc.deleted {
				assert.Equal(t, []string{"delete input_fidelity"}, info.ParamOverrideAudit)
			}
			got["tag"][0] = "changed"
			assert.Equal(t, "a", original["tag"][0])
		})
	}
}

func TestImageFormDeletesRejectUnsupportedRules(t *testing.T) {
	for _, rules := range []map[string]any{
		{"input_fidelity": "low"},
		{"operations": "invalid"},
		{"operations": []any{map[string]any{"mode": "set", "path": "input_fidelity", "value": "low"}}},
		{"operations": []any{map[string]any{"mode": "delete", "path": "n"}}},
		{"operations": []any{map[string]any{"mode": "delete", "path": "size"}}},
		{"operations": []any{map[string]any{"mode": "delete", "path": "quality"}}},
		{"operations": []any{map[string]any{"mode": "delete", "path": "image"}}},
		{"operations": []any{map[string]any{"mode": "delete", "path": "tag.0"}}},
	} {
		_, err := ApplyImageFormDeletes(map[string][]string{"input_fidelity": {"high"}}, &RelayInfo{ChannelMeta: &ChannelMeta{ParamOverride: rules}})
		require.Error(t, err)
	}
}

func TestImageFormDeletesSkipsUnmatchedUnsupportedOperations(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/images/edits"} {
		t.Run(path, func(t *testing.T) {
			info := &RelayInfo{RequestURLPath: "/v1/images/edits", ChannelMeta: &ChannelMeta{ParamOverride: map[string]any{
				"operations": []any{
					map[string]any{"mode": "set", "path": "temperature", "value": 0.5, "conditions": []any{map[string]any{"path": "request_path", "mode": "full", "value": path}}},
					map[string]any{"mode": "delete", "path": "input_fidelity"},
				},
			}}}
			original := map[string][]string{"input_fidelity": {"high"}, "tag": {"a", "b"}}
			got, err := ApplyImageFormDeletes(original, info)
			if path == "/v1/images/edits" {
				require.Error(t, err)
				assert.Empty(t, info.ParamOverrideAudit)
			} else {
				require.NoError(t, err)
				assert.NotContains(t, got, "input_fidelity")
				assert.Equal(t, []string{"a", "b"}, got["tag"])
				assert.Equal(t, []string{"delete input_fidelity"}, info.ParamOverrideAudit)
			}
			assert.Equal(t, []string{"high"}, original["input_fidelity"])
		})
	}
}

func TestImageFormDeletesUsesTrustedRoutingContext(t *testing.T) {
	for _, field := range []string{"model", "upstream_model", "original_model", "request_path"} {
		for _, tc := range []struct {
			name, server, client string
			deleted              bool
		}{
			{"cannot force match", "other", "target", false},
			{"cannot prevent match", "target", "other", true},
			{"missing server metadata", "", "target", false},
		} {
			t.Run(field+"/"+tc.name, func(t *testing.T) {
				info := &RelayInfo{ChannelMeta: &ChannelMeta{}}
				switch field {
				case "model", "upstream_model":
					info.UpstreamModelName = tc.server
				case "original_model":
					info.OriginModelName = tc.server
				case "request_path":
					info.RequestURLPath = tc.server
				}
				info.ParamOverride = map[string]any{"operations": []any{map[string]any{
					"mode": "delete", "path": "input_fidelity", "conditions": []any{map[string]any{"path": field, "mode": "full", "value": "target"}},
				}}}
				got, err := ApplyImageFormDeletes(map[string][]string{field: {tc.client}, "input_fidelity": {"high"}}, info)
				require.NoError(t, err)
				_, exists := got["input_fidelity"]
				assert.Equal(t, !tc.deleted, exists)
				assert.Equal(t, []string{tc.client}, got[field])
			})
		}
	}
}

func TestImageFormDeleteConditionsObserveEarlierDeletes(t *testing.T) {
	info := &RelayInfo{ChannelMeta: &ChannelMeta{ParamOverride: map[string]any{"operations": []any{
		map[string]any{"mode": "delete", "path": "input_fidelity", "conditions": []any{map[string]any{"path": "input_fidelity", "mode": "full", "value": "high"}}},
		map[string]any{"mode": "delete", "path": "tag", "conditions": []any{map[string]any{"path": "input_fidelity", "mode": "full", "value": "high"}}},
	}}}}
	got, err := ApplyImageFormDeletes(map[string][]string{"input_fidelity": {"high"}, "tag": {"keep"}}, info)
	require.NoError(t, err)
	assert.NotContains(t, got, "input_fidelity")
	assert.Equal(t, []string{"keep"}, got["tag"])
	assert.Equal(t, []string{"delete input_fidelity"}, info.ParamOverrideAudit)
}
