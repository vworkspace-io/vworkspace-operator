package controller

import (
	"fmt"
	"strings"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/seaweedengine"
)

func ValidateApplicationInstanceSpec(app *appsv1alpha1.ApplicationInstance) error {
	if app == nil {
		return fmt.Errorf("application instance is nil")
	}
	spec := app.Spec
	if strings.TrimSpace(spec.AppRef.CatalogID) == "" {
		return fmt.Errorf("spec.appRef.catalogId is required")
	}
	if spec.IsPlaceholder() {
		return validatePlaceholderSpec(app.Namespace, spec)
	}
	if seaweedengine.IsSeaweedWorkload(app) {
		return validateNativeSeaweedSpec(app.Namespace, spec)
	}
	if spec.Chart == nil {
		return fmt.Errorf("spec.chart is required")
	}
	if err := validateChart(*spec.Chart); err != nil {
		return err
	}
	if spec.Release == nil {
		return fmt.Errorf("spec.release is required")
	}
	if err := validateRelease(app.Namespace, *spec.Release); err != nil {
		return err
	}
	if spec.Values == nil {
		return fmt.Errorf("spec.values is required")
	}
	return validateValues(*spec.Values)
}

// validatePlaceholderSpec relaxes chart/release/values requirements for a
// placeholder (cluster-ops) instance, which owns no Helm release. A placeholder
// must not declare a chart or values, and if a release is set its namespace must
// still match metadata.namespace so capability-scoped engines (velero) stay
// namespace-bound.
func validatePlaceholderSpec(namespace string, spec appsv1alpha1.ApplicationInstanceSpec) error {
	if spec.Chart != nil {
		return fmt.Errorf("spec.chart must not be set in placeholder mode")
	}
	if spec.Values != nil {
		return fmt.Errorf("spec.values must not be set in placeholder mode")
	}
	if spec.Release != nil {
		if strings.TrimSpace(spec.Release.Namespace) != "" && spec.Release.Namespace != namespace {
			return fmt.Errorf("spec.release.namespace must match metadata.namespace")
		}
	}
	return nil
}

// validateNativeSeaweedSpec relaxes chart requirements for catalogId=seaweedfs
// instances reconciled by SeaweedEngine (P10-T004). Release and values are
// still required; chart must be omitted so Flux Helm is not invoked.
func validateNativeSeaweedSpec(namespace string, spec appsv1alpha1.ApplicationInstanceSpec) error {
	if spec.Chart != nil {
		return fmt.Errorf("spec.chart must not be set for native SeaweedFS workloads")
	}
	if spec.Release == nil {
		return fmt.Errorf("spec.release is required")
	}
	if err := validateRelease(namespace, *spec.Release); err != nil {
		return err
	}
	if spec.Values == nil {
		return fmt.Errorf("spec.values is required")
	}
	return validateValues(*spec.Values)
}

func validateChart(chart appsv1alpha1.ChartSpec) error {
	switch chart.SourceType {
	case appsv1alpha1.ChartSourceOCI, appsv1alpha1.ChartSourceHelm:
	default:
		return fmt.Errorf("spec.chart.sourceType must be oci or helm")
	}
	if strings.TrimSpace(chart.URL) == "" {
		return fmt.Errorf("spec.chart.url is required")
	}
	if strings.TrimSpace(chart.Name) == "" {
		return fmt.Errorf("spec.chart.name is required")
	}
	if strings.TrimSpace(chart.Version) == "" {
		return fmt.Errorf("spec.chart.version is required")
	}
	return nil
}

func validateRelease(namespace string, release appsv1alpha1.ReleaseSpec) error {
	if strings.TrimSpace(release.Name) == "" {
		return fmt.Errorf("spec.release.name is required")
	}
	if strings.TrimSpace(release.Namespace) == "" {
		return fmt.Errorf("spec.release.namespace is required")
	}
	if release.Namespace != namespace {
		return fmt.Errorf("spec.release.namespace must match metadata.namespace")
	}
	return nil
}

func validateValues(values appsv1alpha1.ValuesSpec) error {
	switch values.Source {
	case appsv1alpha1.ValuesSourceInline:
		if values.Inline == nil {
			return fmt.Errorf("spec.values.inline is required when source is inline")
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
