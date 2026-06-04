package controller

import (
	"fmt"
	"strings"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/engines"
)

func ValidateOperationSpec(op *opsv1alpha1.Operation) error {
	if op == nil {
		return fmt.Errorf("operation is nil")
	}
	spec := op.Spec
	if spec.TargetRef.APIVersion != appsv1alpha1.GroupVersion.String() {
		return fmt.Errorf("spec.targetRef.apiVersion must be %s", appsv1alpha1.GroupVersion.String())
	}
	if spec.TargetRef.Kind != "ApplicationInstance" {
		return fmt.Errorf("spec.targetRef.kind must be ApplicationInstance")
	}
	if strings.TrimSpace(spec.TargetRef.Name) == "" {
		return fmt.Errorf("spec.targetRef.name is required")
	}
	if strings.TrimSpace(string(spec.Type)) == "" {
		return fmt.Errorf("spec.type is required")
	}
	if strings.TrimSpace(string(spec.Engine)) == "" {
		return fmt.Errorf("spec.engine is required")
	}
	return engines.ValidateRuntimeParameters(op)
}
