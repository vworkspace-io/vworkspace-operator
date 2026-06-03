package engines

import (
	"encoding/json"
	"fmt"

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
)

// DefaultOperationRunnerServiceAccount is the only ServiceAccount permitted on runtime engines.
const DefaultOperationRunnerServiceAccount = defaultJobServiceAccount

type runtimeParameters struct {
	ServiceAccountName string `json:"serviceAccountName"`
}

// ValidateRuntimeParameters rejects privileged ServiceAccount overrides on job/workflow/hook engines.
func ValidateRuntimeParameters(op *opsv1alpha1.Operation) error {
	switch op.Spec.Engine {
	case opsv1alpha1.EngineJob, opsv1alpha1.EngineWorkflow, opsv1alpha1.EngineHelmHookJob:
	default:
		return nil
	}
	if op.Spec.Parameters == nil {
		return nil
	}
	var params runtimeParameters
	if err := json.Unmarshal(op.Spec.Parameters.Raw, &params); err != nil {
		return nil
	}
	if params.ServiceAccountName != "" && params.ServiceAccountName != DefaultOperationRunnerServiceAccount {
		return fmt.Errorf(
			"parameters.serviceAccountName must be %q or omitted",
			DefaultOperationRunnerServiceAccount,
		)
	}
	return nil
}
