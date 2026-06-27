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

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-apps-vworkspace-io-v1alpha1-applicationinstance,mutating=false,sideEffects=None,groups=apps.vworkspace.io,resources=applicationinstances,verbs=create;update,versions=v1alpha1,name=vapplicationinstance.kb.io,failurePolicy=fail,admissionReviewVersions=v1

// ApplicationInstanceWebhook validates ApplicationInstance resources at admission time.
type ApplicationInstanceWebhook struct {
	decoder admission.Decoder
}

func NewApplicationInstanceWebhook(scheme *runtime.Scheme) (*ApplicationInstanceWebhook, error) {
	decoder := admission.NewDecoder(scheme)
	return &ApplicationInstanceWebhook{decoder: decoder}, nil
}

func (w *ApplicationInstanceWebhook) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &appsv1alpha1.ApplicationInstance{}).
		WithValidator(w).
		Complete()
}

func (w *ApplicationInstanceWebhook) ValidateCreate(_ context.Context, app *appsv1alpha1.ApplicationInstance) (admission.Warnings, error) {
	return nil, validateApplicationInstance(app)
}

func (w *ApplicationInstanceWebhook) ValidateUpdate(_ context.Context, _, app *appsv1alpha1.ApplicationInstance) (admission.Warnings, error) {
	return nil, validateApplicationInstance(app)
}

// validateApplicationInstance runs admission-time checks. Placeholder
// (cluster-ops) instances own no Helm release and carry no chart values, so the
// inline-secret scan is skipped for them; forbidden fields (chart/values) and an
// out-of-namespace release are still rejected at admission rather than only at
// reconcile time.
func validateApplicationInstance(app *appsv1alpha1.ApplicationInstance) error {
	if app.Spec.IsPlaceholder() {
		return validatePlaceholderSpec(app.Namespace, app.Spec)
	}
	if app.Spec.Values == nil {
		return nil
	}
	return validateInlineValues(*app.Spec.Values)
}

func (w *ApplicationInstanceWebhook) ValidateDelete(_ context.Context, _ *appsv1alpha1.ApplicationInstance) (admission.Warnings, error) {
	return nil, nil
}
