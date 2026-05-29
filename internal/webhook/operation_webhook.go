/*
Copyright 2026 vWorkspace Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webhook

import (
	"context"
	"fmt"

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/controller"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-ops-vworkspace-io-v1alpha1-operation,mutating=false,sideEffects=None,groups=ops.vworkspace.io,resources=operations,verbs=create;update,versions=v1alpha1,name=voperation.kb.io,failurePolicy=fail,admissionReviewVersions=v1

// OperationWebhook validates Operation resources at admission time.
type OperationWebhook struct {
	decoder admission.Decoder
}

func NewOperationWebhook(scheme *runtime.Scheme) (*OperationWebhook, error) {
	decoder := admission.NewDecoder(scheme)
	return &OperationWebhook{decoder: decoder}, nil
}

func (w *OperationWebhook) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &opsv1alpha1.Operation{}).
		WithValidator(w).
		Complete()
}

func (w *OperationWebhook) ValidateCreate(_ context.Context, op *opsv1alpha1.Operation) (admission.Warnings, error) {
	if err := controller.ValidateOperationSpec(op); err != nil {
		return nil, err
	}
	return nil, validateOperationType(op)
}

func (w *OperationWebhook) ValidateUpdate(_ context.Context, _, op *opsv1alpha1.Operation) (admission.Warnings, error) {
	if err := controller.ValidateOperationSpec(op); err != nil {
		return nil, err
	}
	return nil, validateOperationType(op)
}

func (w *OperationWebhook) ValidateDelete(_ context.Context, _ *opsv1alpha1.Operation) (admission.Warnings, error) {
	return nil, nil
}

func validateOperationType(op *opsv1alpha1.Operation) error {
	switch op.Spec.Type {
	case opsv1alpha1.OperationTypeBackup,
		opsv1alpha1.OperationTypeRestore,
		opsv1alpha1.OperationTypeUpgrade,
		opsv1alpha1.OperationTypeMigration,
		opsv1alpha1.OperationTypeRunCommand,
		opsv1alpha1.OperationTypeRunbook:
		return nil
	default:
		return fmt.Errorf("unsupported operation type %q", op.Spec.Type)
	}
}

// TODO(user): add engine/type compatibility matrix once engine registry is exposed to webhooks.
