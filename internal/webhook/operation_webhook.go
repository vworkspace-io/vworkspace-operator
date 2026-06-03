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

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/controller"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-ops-vworkspace-io-v1alpha1-operation,mutating=false,sideEffects=None,groups=ops.vworkspace.io,resources=operations,verbs=create;update,versions=v1alpha1,name=voperation.kb.io,failurePolicy=fail,admissionReviewVersions=v1

// OperationWebhook validates Operation resources at admission time.
type OperationWebhook struct {
	decoder admission.Decoder
	client  client.Client
}

func NewOperationWebhook(scheme *runtime.Scheme, cl client.Client) (*OperationWebhook, error) {
	decoder := admission.NewDecoder(scheme)
	return &OperationWebhook{decoder: decoder, client: cl}, nil
}

func (w *OperationWebhook) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &opsv1alpha1.Operation{}).
		WithValidator(w).
		Complete()
}

func (w *OperationWebhook) ValidateCreate(ctx context.Context, op *opsv1alpha1.Operation) (admission.Warnings, error) {
	return nil, w.validateOperation(ctx, op, true)
}

func (w *OperationWebhook) ValidateUpdate(ctx context.Context, old, op *opsv1alpha1.Operation) (admission.Warnings, error) {
	if err := validateOperationSpecImmutability(old, op); err != nil {
		return nil, err
	}
	if err := validateOperationParametersImmutability(old, op); err != nil {
		return nil, err
	}
	validateParameters := operationParametersChanged(old, op)
	return nil, w.validateOperation(ctx, op, validateParameters)
}

func (w *OperationWebhook) ValidateDelete(_ context.Context, _ *opsv1alpha1.Operation) (admission.Warnings, error) {
	return nil, nil
}

func (w *OperationWebhook) validateOperation(ctx context.Context, op *opsv1alpha1.Operation, validateParameters bool) error {
	if err := controller.ValidateOperationSpec(op); err != nil {
		return err
	}
	if err := validateKnownOperationType(op); err != nil {
		return err
	}
	if err := validateNamespaceAllowedTypes(ctx, w.client, op); err != nil {
		return err
	}
	if err := validateOperationBuiltinTemplate(op); err != nil {
		return err
	}
	target, err := validateOperationTargetExists(ctx, w.client, op)
	if err != nil {
		return err
	}
	if err := validateOperationCapability(target, op); err != nil {
		return err
	}
	if validateParameters {
		if err := validateOperationParameters(op); err != nil {
			return err
		}
	}
	return validateOperationConcurrency(ctx, w.client, op)
}
