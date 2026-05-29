package helmengine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/labels"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

var (
	helmReleaseGVK = schema.GroupVersionKind{Group: "helm.toolkit.fluxcd.io", Version: "v2", Kind: "HelmRelease"}
	helmRepoGVK    = schema.GroupVersionKind{Group: "source.toolkit.fluxcd.io", Version: "v1", Kind: "HelmRepository"}
	ociRepoGVK     = schema.GroupVersionKind{Group: "source.toolkit.fluxcd.io", Version: "v1", Kind: "OCIRepository"}
)

// FluxEngine materializes Flux HelmRelease and chart source resources.
type FluxEngine struct {
	Client client.Client
}

func NewFluxEngine(c client.Client) *FluxEngine {
	return &FluxEngine{Client: c}
}

func (e *FluxEngine) EnsureRelease(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error {
	if err := e.ensureChartSource(ctx, app); err != nil {
		return fmt.Errorf("ensure chart source: %w", err)
	}
	return e.ensureHelmRelease(ctx, app)
}

func (e *FluxEngine) DeleteRelease(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error {
	releaseName := app.Spec.Release.Name
	ns := app.Namespace
	for _, gvk := range []schema.GroupVersionKind{helmReleaseGVK, helmRepoGVK, ociRepoGVK} {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		obj.SetName(chartSourceName(app))
		obj.SetNamespace(ns)
		if gvk.Kind == "HelmRelease" {
			obj.SetName(releaseName)
		}
		if err := e.Client.Delete(ctx, obj); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("delete %s/%s: %w", gvk.Kind, obj.GetName(), err)
		}
	}
	return nil
}

func (e *FluxEngine) SyncStatus(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (*StatusSnapshot, error) {
	hr := &unstructured.Unstructured{}
	hr.SetGroupVersionKind(helmReleaseGVK)
	if err := e.Client.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: app.Spec.Release.Name}, hr); err != nil {
		return nil, fmt.Errorf("get helmrelease: %w", err)
	}
	snapshot := &StatusSnapshot{
		ReleaseRef: appsv1alpha1.HelmReleaseRef{
			Name:      app.Spec.Release.Name,
			Namespace: app.Namespace,
		},
	}
	ready, _, _ := unstructured.NestedString(hr.Object, "status", "conditions")
	_ = ready
	conditions, found, err := unstructured.NestedSlice(hr.Object, "status", "conditions")
	if err != nil || !found {
		snapshot.Reason = "Reconciling"
		snapshot.Message = "HelmRelease status not yet available"
		return snapshot, nil
	}
	snapshot.Reason, snapshot.Message, snapshot.Ready, snapshot.Reconciling, snapshot.Degraded = mapHelmReleaseConditions(conditions)
	return snapshot, nil
}

func mapHelmReleaseConditions(conditions []any) (reason, message string, ready, reconciling, degraded bool) {
	for _, item := range conditions {
		cond, ok := item.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := cond["type"].(string)
		status, _ := cond["status"].(string)
		reasonVal, _ := cond["reason"].(string)
		messageVal, _ := cond["message"].(string)
		switch typ {
		case "Ready":
			switch status {
			case "True":
				ready = true
				reason = "HelmReleaseReady"
				message = messageVal
			case "False":
				reason = "HelmReleaseFailed"
				message = messageVal
			}
		case "Released":
			if status == "False" {
				degraded = true
				if reason == "" {
					reason = "HelmReleaseDegraded"
					message = messageVal
				}
			}
		case "Reconciling":
			if status == "True" {
				reconciling = true
				if reason == "" {
					reason = reasonVal
					message = messageVal
				}
			}
		}
	}
	if ready && degraded {
		degraded = false
	}
	if reason == "" && reconciling {
		reason = "HelmReleaseInstalling"
	}
	return reason, message, ready, reconciling, degraded
}

func (e *FluxEngine) ensureChartSource(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error {
	name := chartSourceName(app)
	labelsMap := ownerLabels(app)
	switch app.Spec.Chart.SourceType {
	case appsv1alpha1.ChartSourceHelm:
		repo := &unstructured.Unstructured{}
		repo.SetGroupVersionKind(helmRepoGVK)
		repo.SetName(name)
		repo.SetNamespace(app.Namespace)
		_, err := controllerutil.CreateOrUpdate(ctx, e.Client, repo, func() error {
			repo.SetLabels(labelsMap)
			if err := unstructured.SetNestedField(repo.Object, "1m0s", "spec", "interval"); err != nil {
				return err
			}
			return unstructured.SetNestedField(repo.Object, app.Spec.Chart.URL, "spec", "url")
		})
		return err
	case appsv1alpha1.ChartSourceOCI:
		repo := &unstructured.Unstructured{}
		repo.SetGroupVersionKind(ociRepoGVK)
		repo.SetName(name)
		repo.SetNamespace(app.Namespace)
		_, err := controllerutil.CreateOrUpdate(ctx, e.Client, repo, func() error {
			repo.SetLabels(labelsMap)
			if err := unstructured.SetNestedField(repo.Object, "1m0s", "spec", "interval"); err != nil {
				return err
			}
			url := strings.TrimPrefix(app.Spec.Chart.URL, "oci://")
			if err := unstructured.SetNestedField(repo.Object, url, "spec", "url"); err != nil {
				return err
			}
			return unstructured.SetNestedField(repo.Object, app.Spec.Chart.Version, "spec", "ref", "tag")
		})
		return err
	default:
		return fmt.Errorf("unsupported chart source type %q", app.Spec.Chart.SourceType)
	}
}

func (e *FluxEngine) ensureHelmRelease(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error {
	hr := &unstructured.Unstructured{}
	hr.SetGroupVersionKind(helmReleaseGVK)
	hr.SetName(app.Spec.Release.Name)
	hr.SetNamespace(app.Namespace)
	sourceName := chartSourceName(app)
	sourceKind := "HelmRepository"
	if app.Spec.Chart.SourceType == appsv1alpha1.ChartSourceOCI {
		sourceKind = "OCIRepository"
	}
	_, err := controllerutil.CreateOrUpdate(ctx, e.Client, hr, func() error {
		if err := controllerutil.SetControllerReference(app, hr, e.Client.Scheme()); err != nil {
			return err
		}
		hr.SetLabels(ownerLabels(app))
		if err := unstructured.SetNestedField(hr.Object, "5m0s", "spec", "interval"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(hr.Object, app.Spec.Chart.Version, "spec", "chart", "spec", "version"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(hr.Object, app.Spec.Chart.Name, "spec", "chart", "spec", "chart"); err != nil {
			return err
		}
		if err := unstructured.SetNestedMap(hr.Object, map[string]any{
			"kind": sourceKind,
			"name": sourceName,
		}, "spec", "chart", "spec", "sourceRef"); err != nil {
			return err
		}
		values, err := e.buildValues(ctx, app)
		if err != nil {
			return err
		}
		if values != nil {
			if err := unstructured.SetNestedMap(hr.Object, values, "spec", "values"); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func (e *FluxEngine) buildValues(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (map[string]any, error) {
	switch app.Spec.Values.Source {
	case appsv1alpha1.ValuesSourceInline:
		if app.Spec.Values.Inline == nil {
			return nil, nil
		}
		values := map[string]any{}
		if err := json.Unmarshal(app.Spec.Values.Inline.Raw, &values); err != nil {
			return nil, fmt.Errorf("decode inline values: %w", err)
		}
		return values, nil
	case appsv1alpha1.ValuesSourceSecretRef:
		if app.Spec.Values.SecretRef == nil {
			return nil, fmt.Errorf("secretRef values source requires secretRef")
		}
		return e.loadValuesFromSecret(ctx, app.Namespace, app.Spec.Values.SecretRef)
	case appsv1alpha1.ValuesSourceConfigMapRef:
		if app.Spec.Values.ConfigMapRef == nil {
			return nil, fmt.Errorf("configMapRef values source requires configMapRef")
		}
		return e.loadValuesFromConfigMap(ctx, app.Namespace, app.Spec.Values.ConfigMapRef)
	default:
		return nil, fmt.Errorf("unsupported values source %q", app.Spec.Values.Source)
	}
}

func (e *FluxEngine) loadValuesFromSecret(ctx context.Context, defaultNS string, ref *appsv1alpha1.ObjectKeyRef) (map[string]any, error) {
	secret := &corev1.Secret{}
	ns := defaultNS
	if err := e.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: ref.Name}, secret); err != nil {
		return nil, fmt.Errorf("get secret %s/%s: %w", ns, ref.Name, err)
	}
	raw, ok := secret.Data[ref.Key]
	if !ok {
		return nil, fmt.Errorf("secret %s/%s missing key %q", ns, ref.Name, ref.Key)
	}
	return decodeValuesBytes(raw)
}

func (e *FluxEngine) loadValuesFromConfigMap(ctx context.Context, defaultNS string, ref *appsv1alpha1.ObjectKeyRef) (map[string]any, error) {
	cm := &corev1.ConfigMap{}
	if err := e.Client.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: ref.Name}, cm); err != nil {
		return nil, fmt.Errorf("get configmap %s/%s: %w", defaultNS, ref.Name, err)
	}
	raw, ok := cm.Data[ref.Key]
	if !ok {
		return nil, fmt.Errorf("configmap %s/%s missing key %q", defaultNS, ref.Name, ref.Key)
	}
	return decodeValuesBytes([]byte(raw))
}

func decodeValuesBytes(raw []byte) (map[string]any, error) {
	values := map[string]any{}
	if err := json.Unmarshal(raw, &values); err == nil {
		return values, nil
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("decode values as JSON or YAML: %w", err)
	}
	return values, nil
}

func chartSourceName(app *appsv1alpha1.ApplicationInstance) string {
	return fmt.Sprintf("%s-source", app.Spec.Release.Name)
}

func ownerLabels(app *appsv1alpha1.ApplicationInstance) map[string]string {
	labelsMap := map[string]string{
		labels.ManagedByKey:           labels.ManagedByOperator,
		labels.ApplicationInstanceKey: app.Name,
	}
	if clusterID := app.Labels[labels.ClusterIDKey]; clusterID != "" {
		labelsMap[labels.ClusterIDKey] = clusterID
	}
	if managedBy := app.Labels[labels.ManagedByKey]; managedBy != "" {
		labelsMap[labels.ManagedByKey] = managedBy
	}
	return labelsMap
}

// MaterializeHelmReleaseForTest exposes helm release object shape for unit tests.
func MaterializeHelmReleaseForTest(app *appsv1alpha1.ApplicationInstance) *unstructured.Unstructured {
	hr := &unstructured.Unstructured{}
	hr.SetGroupVersionKind(helmReleaseGVK)
	hr.SetName(app.Spec.Release.Name)
	hr.SetNamespace(app.Namespace)
	_ = unstructured.SetNestedField(hr.Object, app.Spec.Chart.Version, "spec", "chart", "spec", "version")
	_ = unstructured.SetNestedField(hr.Object, app.Spec.Chart.Name, "spec", "chart", "spec", "chart")
	return hr
}

var _ Engine = (*FluxEngine)(nil)

// Ensure compile-time reference to metav1 for owner refs in tests.
var _ = metav1.ObjectMeta{}
