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

// SeaweedObject returns an empty Seaweed CR placeholder for controller watches.
func SeaweedObject() *unstructured.Unstructured {
	sw := &unstructured.Unstructured{}
	sw.SetGroupVersionKind(seaweedGVK)
	return sw
}

func validateReleaseRef(app *appsv1alpha1.ApplicationInstance) error {
	if app.Spec.Release == nil {
		return fmt.Errorf("spec.release is required for Seaweed workload")
	}
	if app.Spec.Release.Name == "" {
		return fmt.Errorf("spec.release.name is required for Seaweed workload")
	}
	return nil
}

// SeaweedEngine materializes seaweed.seaweedfs.com/v1 Seaweed resources.
type SeaweedEngine struct {
	Client client.Client
}

func NewSeaweedEngine(c client.Client) *SeaweedEngine {
	return &SeaweedEngine{Client: c}
}

func releaseNamespace(app *appsv1alpha1.ApplicationInstance) string {
	if app.Spec.Release != nil && app.Spec.Release.Namespace != "" {
		return app.Spec.Release.Namespace
	}
	return app.Namespace
}

func (e *SeaweedEngine) EnsureSeaweed(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error {
	if err := validateReleaseRef(app); err != nil {
		return err
	}
	values, err := loadValues(ctx, e.Client, app)
	if err != nil {
		return fmt.Errorf("load values: %w", err)
	}
	values, err = normalizeSeaweedValues(values)
	if err != nil {
		return fmt.Errorf("normalize values: %w", err)
	}

	ns := releaseNamespace(app)
	if ns != app.Namespace {
		return fmt.Errorf("spec.release.namespace %q must match metadata.namespace %q for Seaweed workloads", ns, app.Namespace)
	}
	sw := &unstructured.Unstructured{}
	sw.SetGroupVersionKind(seaweedGVK)
	sw.SetName(app.Spec.Release.Name)
	sw.SetNamespace(ns)

	_, err = controllerutil.CreateOrUpdate(ctx, e.Client, sw, func() error {
		if err := controllerutil.SetControllerReference(app, sw, e.Client.Scheme()); err != nil {
			return err
		}
		sw.SetLabels(ownerLabels(app))
		spec := map[string]any{}
		for _, key := range seaweedSpecKeys {
			if fragment, ok := values[key]; ok {
				spec[key] = fragment
			}
		}
		if len(spec) == 0 {
			return fmt.Errorf("values must include at least one native Seaweed spec section (master, volume, filer, s3, …); Helm chart-wrapped values require conversion before reconciliation")
		}
		return unstructured.SetNestedMap(sw.Object, spec, "spec")
	})
	return err
}

func (e *SeaweedEngine) DeleteSeaweed(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error {
	if err := validateReleaseRef(app); err != nil {
		return err
	}
	ns := releaseNamespace(app)
	sw := &unstructured.Unstructured{}
	sw.SetGroupVersionKind(seaweedGVK)
	sw.SetName(app.Spec.Release.Name)
	sw.SetNamespace(ns)
	if err := e.Client.Delete(ctx, sw); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("delete Seaweed/%s: %w", sw.GetName(), err)
	}
	return nil
}

func (e *SeaweedEngine) SeaweedExists(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (bool, error) {
	if err := validateReleaseRef(app); err != nil {
		return false, err
	}
	ns := releaseNamespace(app)
	sw := &unstructured.Unstructured{}
	sw.SetGroupVersionKind(seaweedGVK)
	if err := e.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: app.Spec.Release.Name}, sw); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return false, nil
		}
		return false, fmt.Errorf("get Seaweed: %w", err)
	}
	return true, nil
}

func (e *SeaweedEngine) SyncStatus(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (*StatusSnapshot, error) {
	if err := validateReleaseRef(app); err != nil {
		return nil, err
	}
	ns := releaseNamespace(app)
	sw := &unstructured.Unstructured{}
	sw.SetGroupVersionKind(seaweedGVK)
	if err := e.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: app.Spec.Release.Name}, sw); err != nil {
		return nil, fmt.Errorf("get Seaweed: %w", err)
	}

	snapshot := &StatusSnapshot{}
	conditions, found, err := unstructured.NestedSlice(sw.Object, "status", "conditions")
	if err != nil || !found {
		snapshot.Reason = "Reconciling"
		snapshot.Message = "Seaweed status not yet available"
		return snapshot, nil
	}
	snapshot.Reason, snapshot.Message, snapshot.Ready, snapshot.Reconciling, snapshot.Degraded = mapSeaweedConditions(conditions)
	snapshot.HasS3 = hasS3Spec(sw)
	if snapshot.Ready && snapshot.HasS3 && s3ServiceExists(ctx, e.Client, app.Spec.Release.Name, ns) {
		snapshot.S3Endpoint = S3Endpoint(app.Spec.Release.Name, ns)
	}
	return snapshot, nil
}

func s3ServiceExists(ctx context.Context, c client.Client, releaseName, namespace string) bool {
	svc := &corev1.Service{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: s3ServiceName(releaseName)}, svc); err != nil {
		return false
	}
	return true
}

// s3ServiceName returns the in-cluster Service name for the SeaweedFS S3 gateway.
// The seaweedfs-operator creates "{releaseName}-s3" on port 8333 when spec.s3 is
// enabled on the Seaweed CR (seaweed.seaweedfs.com/v1).
func s3ServiceName(releaseName string) string {
	return releaseName + "-s3"
}

const s3GatewayPort = 8333

func hasS3Spec(sw *unstructured.Unstructured) bool {
	spec, found, err := unstructured.NestedMap(sw.Object, "spec")
	if err != nil || !found {
		return false
	}
	return s3EnabledInSpec(spec)
}

func s3EnabledInSpec(spec map[string]any) bool {
	s3, ok := spec["s3"]
	if !ok {
		return false
	}
	s3Map, ok := s3.(map[string]any)
	if !ok || len(s3Map) == 0 {
		return false
	}
	if enabled, found, err := unstructured.NestedBool(s3Map, "enabled"); err == nil && found && !enabled {
		return false
	}
	replicas, found, err := unstructured.NestedInt64(s3Map, "replicas")
	if err != nil {
		return false
	}
	if found {
		return replicas > 0
	}
	return true
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
// The Seaweed CR status does not publish service URLs; the seaweedfs-operator
// names the gateway Service s3ServiceName(releaseName) on port s3GatewayPort.
func S3Endpoint(releaseName, namespace string) string {
	return fmt.Sprintf("http://%s.%s.svc:%d", s3ServiceName(releaseName), namespace, s3GatewayPort)
}

var seaweedValueWrapperKeys = []string{"seaweedfs", "seaweed"}

func normalizeSeaweedValues(values map[string]any) (map[string]any, error) {
	for _, key := range seaweedSpecKeys {
		if _, ok := values[key]; ok {
			return values, nil
		}
	}
	for _, wrapper := range seaweedValueWrapperKeys {
		nested, ok := values[wrapper].(map[string]any)
		if !ok {
			continue
		}
		for _, key := range seaweedSpecKeys {
			if _, ok := nested[key]; ok {
				return nested, nil
			}
		}
	}
	var candidates []map[string]any
	for _, v := range values {
		nested, ok := v.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range seaweedSpecKeys {
			if _, ok := nested[key]; ok {
				candidates = append(candidates, nested)
				break
			}
		}
	}
	switch len(candidates) {
	case 0:
		return values, nil
	case 1:
		return candidates[0], nil
	default:
		return nil, fmt.Errorf("ambiguous Seaweed values: multiple wrapper sections contain Seaweed spec keys")
	}
}

var seaweedSpecKeys = []string{"master", "volume", "filer", "s3", "admin", "worker", "sftp"}

func loadValues(ctx context.Context, c client.Client, app *appsv1alpha1.ApplicationInstance) (map[string]any, error) {
	if app.Spec.Values == nil {
		return nil, fmt.Errorf("spec.values is required for Seaweed workload")
	}
	switch app.Spec.Values.Source {
	case appsv1alpha1.ValuesSourceInline:
		if app.Spec.Values.Inline == nil {
			return nil, fmt.Errorf("inline values are required for Seaweed workload")
		}
		values := map[string]any{}
		if err := json.Unmarshal(app.Spec.Values.Inline.Raw, &values); err != nil {
			return nil, fmt.Errorf("decode inline values: %w", err)
		}
		return normalizeSeaweedValues(values)
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
		return normalizeSeaweedValues(values)
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("decode values as JSON or YAML: %w", err)
	}
	return normalizeSeaweedValues(values)
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
	sw.SetNamespace(releaseNamespace(app))
	_ = unstructured.SetNestedMap(sw.Object, values, "spec")
	return sw
}

var _ = metav1.ObjectMeta{}
