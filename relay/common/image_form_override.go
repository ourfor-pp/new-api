package common

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/QuantumNous/new-api/common"
)

var imageFormFieldName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// ApplyImageFormDeletes applies channel deletion rules to a private copy of the
// ordinary multipart fields. Files are deliberately outside the rule engine.
func ApplyImageFormDeletes(fields map[string][]string, info *RelayInfo) (map[string][]string, error) {
	if info != nil {
		info.ParamOverrideAudit = nil
	}
	result := make(map[string][]string, len(fields))
	for key, values := range fields {
		result[key] = slices.Clone(values)
	}
	rules := getParamOverrideMap(info)
	if len(rules) == 0 {
		return result, nil
	}
	operations, ok := tryParseOperations(rules)
	if !ok || len(buildLegacyParamOverride(rules)) != 0 {
		return nil, fmt.Errorf("multipart image overrides require delete operations")
	}
	// Single values retain JSON scalar semantics; repeated fields remain arrays.
	values := make(map[string]interface{}, len(result))
	for key, entries := range result {
		if len(entries) == 1 {
			values[key] = entries[0]
		} else {
			values[key] = entries
		}
	}
	context := BuildParamOverrideContext(info)
	// Reserve routing names even when context metadata is absent. Client form
	// fields must never supply a model or endpoint used to scope channel rules.
	for _, key := range []string{"model", "upstream_model", "original_model", "request_path"} {
		delete(values, key)
	}
	for key := range context {
		delete(values, key)
	}
	contextJSON, err := marshalContextJSON(context)
	if err != nil {
		return nil, err
	}
	var audit []string
	for _, op := range operations {
		data, err := common.Marshal(values)
		if err != nil {
			return nil, err
		}
		matched, err := checkConditions(data, contextJSON, op.Conditions, op.Logic)
		if err != nil {
			return nil, err
		}
		if !matched {
			continue
		}
		if op.Mode != "delete" || !imageFormFieldName.MatchString(op.Path) {
			return nil, fmt.Errorf("multipart image overrides support only deletion of ordinary top-level fields")
		}
		switch op.Path {
		case "model", "n", "size", "quality", "image", "mask", "prompt", "stream", "partial_images":
			return nil, fmt.Errorf("multipart image override cannot delete protected field %q", op.Path)
		}
		if _, exists := result[op.Path]; exists {
			delete(result, op.Path)
			delete(values, op.Path)
			// Deletion audit contains field names only, never field values.
			audit = append(audit, "delete "+op.Path)
		}
	}
	info.ParamOverrideAudit = audit
	return result, nil
}
