package common

import (
	"errors"

	rootcommon "github.com/QuantumNous/new-api/common"
)

// ResolveMappedModelName 解析渠道模型映射并返回最终上游模型。
// originModelName 始终由调用方保留，用于权限、定价和日志。
func ResolveMappedModelName(originModelName string, modelMapping string) (string, bool, error) {
	if modelMapping == "" || modelMapping == "{}" {
		return originModelName, false, nil
	}

	modelMap := make(map[string]string)
	if err := rootcommon.UnmarshalJsonStr(modelMapping, &modelMap); err != nil {
		return "", false, errors.New("unmarshal_model_mapping_failed")
	}

	currentModel := originModelName
	isMapped := false
	visitedModels := map[string]bool{
		currentModel: true,
	}
	for {
		mappedModel, exists := modelMap[currentModel]
		if !exists || mappedModel == "" {
			return currentModel, isMapped, nil
		}
		if visitedModels[mappedModel] {
			if mappedModel == currentModel {
				return currentModel, isMapped, nil
			}
			return "", false, errors.New("model_mapping_contains_cycle")
		}
		visitedModels[mappedModel] = true
		currentModel = mappedModel
		isMapped = true
	}
}
