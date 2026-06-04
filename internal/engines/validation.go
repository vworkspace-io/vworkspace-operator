package engines

import (
	"encoding/json"
	"fmt"

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
)

// DefaultOperationRunnerServiceAccount is the only ServiceAccount permitted on runtime engines.
const DefaultOperationRunnerServiceAccount = defaultJobServiceAccount

type runtimeParameters struct {
	Image              string `json:"image"`
	HookName           string `json:"hookName"`
	WorkflowTemplate   string `json:"workflowTemplate"`
	Template           string `json:"template"`
	ServiceAccountName string `json:"serviceAccountName"`
}

// ValidateRuntimeParameters rejects unsafe or incomplete runtime engine parameters at admission.
func ValidateRuntimeParameters(op *opsv1alpha1.Operation) error {
	switch op.Spec.Engine {
	case opsv1alpha1.EngineJob, opsv1alpha1.EngineWorkflow, opsv1alpha1.EngineHelmHookJob:
	default:
		return nil
	}
	if op.Spec.Parameters == nil {
		return validateRequiredRuntimeFields(op.Spec.Engine, runtimeParameters{})
	}
	var params runtimeParameters
	if err := json.Unmarshal(op.Spec.Parameters.Raw, &params); err != nil {
		return fmt.Errorf("decode runtime parameters: %w", err)
	}
	if err := validateRequiredRuntimeFields(op.Spec.Engine, params); err != nil {
		return err
	}
	if params.ServiceAccountName != "" && params.ServiceAccountName != DefaultOperationRunnerServiceAccount {
		return fmt.Errorf(
			"parameters.serviceAccountName must be %q or omitted",
			DefaultOperationRunnerServiceAccount,
		)
	}
	return nil
}

func validateRequiredRuntimeFields(engine opsv1alpha1.OperationEngine, params runtimeParameters) error {
	switch engine {
	case opsv1alpha1.EngineJob:
		if params.Image == "" {
			return fmt.Errorf("parameters.image is required")
		}
	case opsv1alpha1.EngineHelmHookJob:
		if params.HookName == "" {
			return fmt.Errorf("parameters.hookName is required")
		}
		if params.Image == "" {
			return fmt.Errorf("parameters.image is required")
		}
	case opsv1alpha1.EngineWorkflow:
		if params.WorkflowTemplate == "" && params.Template == "" {
			return fmt.Errorf("parameters.workflowTemplate is required")
		}
	}
	return nil
}
