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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/operations/templates"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const namespaceAllowedTypesAnnotation = "ops.vworkspace.io/allowed-types"

var (
	pemPrivateKeyPattern = regexp.MustCompile(`^-----BEGIN .*PRIVATE KEY-----`)
	sensitiveExactKeys   = map[string]struct{}{
		"accesskey": {},
		"secretkey": {},
		"apikey":    {},
		"token":     {},
	}
	inlineSecretPlaceholders = map[string]struct{}{
		"<set via externalsecret>":  {},
		"<set via external-secret>": {},
	}
)

func validateKnownOperationType(op *opsv1alpha1.Operation) error {
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

func validateNamespaceAllowedTypes(ctx context.Context, cl client.Client, op *opsv1alpha1.Operation) error {
	ns := &corev1.Namespace{}
	if err := cl.Get(ctx, client.ObjectKey{Name: op.Namespace}, ns); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get namespace %q: %w", op.Namespace, err)
	}
	raw, ok := ns.Annotations[namespaceAllowedTypesAnnotation]
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}
	allowed := parseAllowedTypes(raw)
	if _, ok := allowed[string(op.Spec.Type)]; ok {
		return nil
	}
	return fmt.Errorf("operation type %q not allowed in namespace %q (annotation %s)", op.Spec.Type, op.Namespace, namespaceAllowedTypesAnnotation)
}

func parseAllowedTypes(raw string) map[string]struct{} {
	out := make(map[string]struct{})
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out[part] = struct{}{}
		}
	}
	return out
}

func validateOperationTargetExists(ctx context.Context, cl client.Client, op *opsv1alpha1.Operation) (*appsv1alpha1.ApplicationInstance, error) {
	target := &appsv1alpha1.ApplicationInstance{}
	key := client.ObjectKey{Namespace: op.Namespace, Name: op.Spec.TargetRef.Name}
	if err := cl.Get(ctx, key, target); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("target ApplicationInstance %q not found in namespace %q", op.Spec.TargetRef.Name, op.Namespace)
		}
		return nil, fmt.Errorf("get target ApplicationInstance: %w", err)
	}
	return target, nil
}

func validateOperationBuiltinTemplate(op *opsv1alpha1.Operation) error {
	if _, ok := templates.Lookup(op.Spec.Type, op.Spec.Engine); ok {
		return nil
	}
	return fmt.Errorf(
		"no built-in operation template for type %q and engine %q (expected refs such as %s)",
		op.Spec.Type, op.Spec.Engine, strings.Join(templates.BuiltinRefs, ", "),
	)
}

func validateOperationParameters(op *opsv1alpha1.Operation) error {
	var raw []byte
	if op.Spec.Parameters != nil {
		raw = op.Spec.Parameters.Raw
	}
	return templates.ValidateParameters(op.Spec.Type, op.Spec.Engine, raw)
}

func operationParametersChanged(old, new *opsv1alpha1.Operation) bool {
	oldRaw := []byte("{}")
	newRaw := []byte("{}")
	if old.Spec.Parameters != nil {
		oldRaw = old.Spec.Parameters.Raw
	}
	if new.Spec.Parameters != nil {
		newRaw = new.Spec.Parameters.Raw
	}
	return !bytes.Equal(bytes.TrimSpace(oldRaw), bytes.TrimSpace(newRaw))
}

func validateOperationSpecImmutability(old, new *opsv1alpha1.Operation) error {
	if old.Spec.TargetRef.APIVersion != new.Spec.TargetRef.APIVersion {
		return fmt.Errorf("spec.targetRef.apiVersion is immutable")
	}
	if old.Spec.TargetRef.Kind != new.Spec.TargetRef.Kind {
		return fmt.Errorf("spec.targetRef.kind is immutable")
	}
	if old.Spec.TargetRef.Name != new.Spec.TargetRef.Name {
		return fmt.Errorf("spec.targetRef.name is immutable")
	}
	if old.Spec.Type != new.Spec.Type {
		return fmt.Errorf("spec.type is immutable")
	}
	if old.Spec.Engine != new.Spec.Engine {
		return fmt.Errorf("spec.engine is immutable")
	}
	return nil
}

func validateOperationCapability(target *appsv1alpha1.ApplicationInstance, op *opsv1alpha1.Operation) error {
	tpl, ok := templates.Lookup(op.Spec.Type, op.Spec.Engine)
	if !ok {
		return nil
	}
	key := templates.CapabilityAnnotationKey(tpl.CapabilityVerb)
	want := string(op.Spec.Engine)
	got, ok := target.Annotations[key]
	if !ok || strings.TrimSpace(got) == "" {
		return fmt.Errorf(
			"target %q missing capability annotation %s=%q required for template %s",
			target.Name, key, want, tpl.Ref,
		)
	}
	if strings.TrimSpace(got) != want {
		return fmt.Errorf(
			"target %q capability %s=%q does not match operation engine %q (template %s)",
			target.Name, key, got, want, tpl.Ref,
		)
	}
	return nil
}

func validateOperationConcurrency(ctx context.Context, cl client.Client, op *opsv1alpha1.Operation) error {
	list := &opsv1alpha1.OperationList{}
	if err := cl.List(ctx, list, client.InNamespace(op.Namespace)); err != nil {
		return fmt.Errorf("list operations: %w", err)
	}
	for _, item := range list.Items {
		if item.Name == op.Name {
			continue
		}
		if item.Spec.TargetRef.Name != op.Spec.TargetRef.Name {
			continue
		}
		if !operationInFlight(&item) {
			continue
		}
		return fmt.Errorf("target %q already has active operation %q (phase=%s)",
			op.Spec.TargetRef.Name, item.Name, item.Status.Phase)
	}
	return nil
}

func operationInFlight(op *opsv1alpha1.Operation) bool {
	switch op.Status.Phase {
	case opsv1alpha1.PhaseSucceeded, opsv1alpha1.PhaseFailed, opsv1alpha1.PhaseCancelled:
		return false
	case opsv1alpha1.PhaseRunning:
		return true
	}
	for _, c := range op.Status.Conditions {
		if c.Type == opsv1alpha1.ConditionAccepted && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func validateInlineValues(values appsv1alpha1.ValuesSpec) error {
	switch values.Source {
	case appsv1alpha1.ValuesSourceInline:
		if values.Inline == nil {
			return fmt.Errorf("spec.values.inline is required when source is inline")
		}
		if err := DetectInlineSecret(values.Inline.Raw); err != nil {
			return err
		}
	case appsv1alpha1.ValuesSourceSecretRef:
		if values.SecretRef == nil || values.SecretRef.Name == "" || values.SecretRef.Key == "" {
			return fmt.Errorf("spec.values.secretRef name and key are required when source is secretRef")
		}
	case appsv1alpha1.ValuesSourceConfigMapRef:
		if values.ConfigMapRef == nil || values.ConfigMapRef.Name == "" || values.ConfigMapRef.Key == "" {
			return fmt.Errorf("spec.values.configMapRef name and key are required when source is configMapRef")
		}
	default:
		return fmt.Errorf("spec.values.source must be inline, secretRef, or configMapRef")
	}
	return nil
}

// DetectInlineSecret walks inline chart values and rejects leaf fields that match the
// placeholder secret rule set documented in docs/security/secrets-handling.md.
func DetectInlineSecret(raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("decode inline values: %w", err)
	}
	return walkInlineSecret("", root)
}

func walkInlineSecret(path string, node any) error {
	switch v := node.(type) {
	case map[string]any:
		for key, child := range v {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			if err := checkSensitiveLeaf(childPath, key, child); err != nil {
				return err
			}
			if err := walkInlineSecret(childPath, child); err != nil {
				return err
			}
		}
	case []any:
		for i, child := range v {
			childPath := fmt.Sprintf("%s[%d]", path, i)
			if err := walkInlineSecret(childPath, child); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkSensitiveLeaf(path, key string, value any) error {
	str, ok := value.(string)
	if !ok {
		return nil
	}
	trimmed := strings.TrimSpace(str)
	if trimmed == "" {
		return nil
	}
	if _, ok := inlineSecretPlaceholders[strings.ToLower(trimmed)]; ok {
		return nil
	}
	lowerKey := strings.ToLower(key)
	if strings.HasSuffix(lowerKey, "password") || strings.HasSuffix(lowerKey, "secret") {
		return fmt.Errorf("inline secret material rejected at %q: use secretRef or configMapRef", path)
	}
	if _, ok := sensitiveExactKeys[lowerKey]; ok {
		return fmt.Errorf("inline secret material rejected at %q: use secretRef or configMapRef", path)
	}
	if pemPrivateKeyPattern.MatchString(trimmed) {
		return fmt.Errorf("inline secret material rejected at %q: PEM private key not allowed inline", path)
	}
	return nil
}
