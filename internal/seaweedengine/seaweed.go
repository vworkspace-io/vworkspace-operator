package seaweedengine

import (
	"context"
	"encoding/json"
	"fmt"

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

var seaweedGVK = schema.GroupVersionKind{Group: "seaweed.seaweedfs.com", Version: "v1", Kind: "Seaweed"}

// SeaweedEngine materializes seaweed.seaweedfs.com/v1 Seaweed resources.
type SeaweedEngine struct {
	Client client.Client
}

func NewSeaweedEngine(c client.Client) *SeaweedEngine {
	return &SeaweedEngine{Client: c}
}

func (e *SeaweedEngine) EnsureSeaweed(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error {
	values, err := loadValues(ctx, e.Client, app)
	if err != nil {
		return fmt.Errorf("load values: %w", err)
	}

	sw := &unstructured.Unstructured{}
	sw.SetGroupVersionKind(seaweedGVK)
	sw.SetName(app.Spec.Release.Name)
	sw.SetNamespace(app.Namespace)

	_, err = controllerutil.CreateOrUpdate(ctx, e.Client, sw, func() error {
		if err := controllerutil.SetControllerReference(app, sw, e.Client.Scheme()); err != nil {
			return err
		}
		sw.SetLabels(ownerLabels(app))
		spec := map[string]any{}
		for _, key := range []string{"master", "volume", "filer", "s3", "admin", "worker", "sftp"} {
			if fragment, ok := values[key]; ok {
				spec[key] = fragment
			}
		}
		if len(spec) == 0 {
			return fmt.Errorf("inline values must include at least one Seaweed spec section (master, volume, filer, s3, …)")
		}
		return unstructured.SetNestedMap(sw.Object, spec, "spec")
	})
	return err
}

func (e *SeaweedEngine) DeleteSeaweed(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error {
	sw := &unstructured.Unstructured{}
	sw.SetGroupVersionKind(seaweedGVK)
	sw.SetName(app.Spec.Release.Name)
	sw.SetNamespace(app.Namespace)
	if err := e.Client.Delete(ctx, sw); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("delete Seaweed/%s: %w", sw.GetName(), err)
	}
	return nil
}

func (e *SeaweedEngine) SyncStatus(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (*StatusSnapshot, error) {
	sw := &unstructured.Unstructured{}
	sw.SetGroupVersionKind(seaweedGVK)
	if err := e.Client.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: app.Spec.Release.Name}, sw); err != nil {
		return nil, fmt.Errorf("get Seaweed: %w", err)
	}

	snapshot := &StatusSnapshot{
		S3Endpoint: S3Endpoint(app.Spec.Release.Name, app.Namespace),
	}
	conditions, found, err := unstructured.NestedSlice(sw.Object, "status", "conditions")
	if err != nil || !found {
		snapshot.Reason = "Reconciling"
		snapshot.Message = "Seaweed status not yet available"
		return snapshot, nil
	}
	snapshot.Reason, snapshot.Message, snapshot.Ready, snapshot.Reconciling, snapshot.Degraded = mapSeaweedConditions(conditions)
	return snapshot, nil
}

func mapSeaweedConditions(conditions []any) (reason, message string, ready, reconciling, degraded bool) {
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
			case string(metav1.ConditionTrue):
				ready = true
				reason = "SeaweedReady"
				message = messageVal
			case string(metav1.ConditionFalse):
				reason = "SeaweedFailed"
				message = messageVal
			}
		case "Progressing":
			if status == string(metav1.ConditionTrue) {
				reconciling = true
				if reason == "" {
					reason = reasonVal
					message = messageVal
				}
			}
		case "Degraded":
			if status == string(metav1.ConditionTrue) {
				degraded = true
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
		reason = "SeaweedReconciling"
	}
	return reason, message, ready, reconciling, degraded
}

// S3Endpoint returns the in-cluster S3 gateway URL for a Seaweed cluster.
func S3Endpoint(releaseName, namespace string) string {
	return fmt.Sprintf("http://%s-s3.%s.svc:8333", releaseName, namespace)
}

func loadValues(ctx context.Context, c client.Client, app *appsv1alpha1.ApplicationInstance) (map[string]any, error) {
	switch app.Spec.Values.Source {
	case appsv1alpha1.ValuesSourceInline:
		if app.Spec.Values.Inline == nil {
			return nil, fmt.Errorf("inline values are required for Seaweed workload")
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
		return loadValuesFromSecret(ctx, c, app.Namespace, app.Spec.Values.SecretRef)
	case appsv1alpha1.ValuesSourceConfigMapRef:
		if app.Spec.Values.ConfigMapRef == nil {
			return nil, fmt.Errorf("configMapRef values source requires configMapRef")
		}
		return loadValuesFromConfigMap(ctx, c, app.Namespace, app.Spec.Values.ConfigMapRef)
	default:
		return nil, fmt.Errorf("unsupported values source %q", app.Spec.Values.Source)
	}
}

func loadValuesFromSecret(ctx context.Context, c client.Client, defaultNS string, ref *appsv1alpha1.ObjectKeyRef) (map[string]any, error) {
	secret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: ref.Name}, secret); err != nil {
		return nil, fmt.Errorf("get secret %s/%s: %w", defaultNS, ref.Name, err)
	}
	raw, ok := secret.Data[ref.Key]
	if !ok {
		return nil, fmt.Errorf("secret %s/%s missing key %q", defaultNS, ref.Name, ref.Key)
	}
	return decodeValuesBytes(raw)
}

func loadValuesFromConfigMap(ctx context.Context, c client.Client, defaultNS string, ref *appsv1alpha1.ObjectKeyRef) (map[string]any, error) {
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: ref.Name}, cm); err != nil {
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

var _ Engine = (*SeaweedEngine)(nil)

// MaterializeSeaweedForTest exposes Seaweed object shape for unit tests.
func MaterializeSeaweedForTest(app *appsv1alpha1.ApplicationInstance, values map[string]any) *unstructured.Unstructured {
	sw := &unstructured.Unstructured{}
	sw.SetGroupVersionKind(seaweedGVK)
	sw.SetName(app.Spec.Release.Name)
	sw.SetNamespace(app.Namespace)
	_ = unstructured.SetNestedMap(sw.Object, values, "spec")
	return sw
}

var _ = metav1.ObjectMeta{}
