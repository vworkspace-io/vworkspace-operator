package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/labels"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

const fieldManager = "vworkspace-agent"

// ApplyOutcome is the terminal result of applying a job.
type ApplyOutcome struct {
	Result     JobResult
	Idempotent bool
}

// Applier applies Pull-mode jobs to the local API server.
type Applier struct {
	Client    client.Client
	Scheme    *runtime.Scheme
	ClusterID string

	Idempotency *IdempotencyStore
}

// ApplyJob applies a single job after ack. Caller posts the returned JobResult.
func (a *Applier) ApplyJob(ctx context.Context, job Job) (ApplyOutcome, error) {
	now := time.Now().UTC()
	if !job.ExpiresAt.IsZero() && now.After(job.ExpiresAt) {
		return ApplyOutcome{Result: JobResult{
			Outcome:   OutcomeNoop,
			Error:     "expired",
			Timestamp: now,
		}}, nil
	}

	if job.IdempotencyKey != "" {
		if a.Idempotency != nil {
			seen, err := a.Idempotency.Contains(ctx, job.IdempotencyKey)
			if err != nil {
				return ApplyOutcome{}, fmt.Errorf("check idempotency key: %w", err)
			}
			if seen {
				return ApplyOutcome{
					Result: JobResult{
						Outcome:   OutcomeNoop,
						Timestamp: now,
					},
					Idempotent: true,
				}, nil
			}
		}
	}

	var result ApplyOutcome
	var applyErr error

	switch job.Kind {
	case "apply":
		ref, err := a.applyManifest(ctx, job.Payload)
		if err != nil {
			applyErr = err
		} else {
			result = ApplyOutcome{Result: JobResult{
				Outcome:    OutcomeSucceeded,
				AppliedRef: ref,
				Timestamp:  now,
			}}
		}
	case "delete":
		ref, err := a.deleteObject(ctx, job.Payload)
		if err != nil {
			applyErr = err
		} else {
			result = ApplyOutcome{Result: JobResult{
				Outcome:    OutcomeSucceeded,
				AppliedRef: ref,
				Timestamp:  now,
			}}
		}
	case "intent":
		ref, err := a.applyIntent(ctx, job.Payload)
		if err != nil {
			applyErr = err
		} else {
			result = ApplyOutcome{Result: JobResult{
				Outcome:    OutcomeSucceeded,
				AppliedRef: ref,
				Timestamp:  now,
			}}
		}
	default:
		return ApplyOutcome{}, fmt.Errorf("unsupported job kind %q", job.Kind)
	}

	if applyErr != nil {
		return ApplyOutcome{}, applyErr
	}

	if job.IdempotencyKey != "" && a.Idempotency != nil && result.Result.Outcome == OutcomeSucceeded {
		if err := a.Idempotency.Record(ctx, job.IdempotencyKey); err != nil {
			return ApplyOutcome{}, fmt.Errorf("record idempotency key: %w", err)
		}
	}
	return result, nil
}

func (a *Applier) applyManifest(ctx context.Context, payload json.RawMessage) (*AppliedRef, error) {
	obj := &unstructured.Unstructured{}
	if err := json.Unmarshal(payload, obj); err != nil {
		return nil, fmt.Errorf("decode apply payload: %w", err)
	}
	a.ensureManagedLabels(obj)

	if err := a.patchApply(ctx, obj); err != nil {
		return nil, err
	}

	latest := &unstructured.Unstructured{}
	latest.SetGroupVersionKind(obj.GroupVersionKind())
	if err := a.Client.Get(ctx, client.ObjectKeyFromObject(obj), latest); err != nil {
		return nil, fmt.Errorf("get applied object: %w", err)
	}
	return objectRef(latest), nil
}

func (a *Applier) deleteObject(ctx context.Context, payload json.RawMessage) (*AppliedRef, error) {
	var ref objectReference
	if err := json.Unmarshal(payload, &ref); err != nil {
		return nil, fmt.Errorf("decode delete payload: %w", err)
	}
	if ref.APIVersion == "" || ref.Kind == "" || ref.Name == "" {
		return nil, fmt.Errorf("delete payload requires apiVersion, kind, and name")
	}
	gv, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil {
		return nil, fmt.Errorf("parse apiVersion: %w", err)
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: gv.Group, Version: gv.Version, Kind: ref.Kind})
	obj.SetName(ref.Name)
	obj.SetNamespace(ref.Namespace)
	if err := a.Client.Delete(ctx, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return &AppliedRef{
				APIVersion: ref.APIVersion,
				Kind:       ref.Kind,
				Namespace:  ref.Namespace,
				Name:       ref.Name,
			}, nil
		}
		return nil, fmt.Errorf("delete %s/%s: %w", ref.Kind, ref.Name, err)
	}
	return &AppliedRef{
		APIVersion: ref.APIVersion,
		Kind:       ref.Kind,
		Namespace:  ref.Namespace,
		Name:       ref.Name,
	}, nil
}

type intentPayload struct {
	Intent string `json:"intent"`
	// ensure-application-instance
	ApplicationInstance *appsv1alpha1.ApplicationInstance `json:"applicationInstance,omitempty"`
	// request-operation
	Operation *opsv1alpha1.Operation `json:"operation,omitempty"`
	// delete-application-instance
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

func (a *Applier) applyIntent(ctx context.Context, payload json.RawMessage) (*AppliedRef, error) {
	var intent intentPayload
	if err := json.Unmarshal(payload, &intent); err != nil {
		return nil, fmt.Errorf("decode intent payload: %w", err)
	}
	switch intent.Intent {
	case "ensure-application-instance":
		if intent.ApplicationInstance == nil {
			return nil, fmt.Errorf("ensure-application-instance requires applicationInstance")
		}
		app := intent.ApplicationInstance
		a.ensureManagedLabelsOnTyped(app)
		if err := a.patchApplyTyped(ctx, app); err != nil {
			return nil, err
		}
		latest := &appsv1alpha1.ApplicationInstance{}
		if err := a.Client.Get(ctx, client.ObjectKeyFromObject(app), latest); err != nil {
			return nil, fmt.Errorf("get application instance: %w", err)
		}
		return a.typedRef(latest)
	case "request-operation":
		if intent.Operation == nil {
			return nil, fmt.Errorf("request-operation requires operation")
		}
		op := intent.Operation
		a.ensureManagedLabelsOnTyped(op)
		if err := a.patchApplyTyped(ctx, op); err != nil {
			return nil, err
		}
		latest := &opsv1alpha1.Operation{}
		if err := a.Client.Get(ctx, client.ObjectKeyFromObject(op), latest); err != nil {
			return nil, fmt.Errorf("get operation: %w", err)
		}
		return a.typedRef(latest)
	case "delete-application-instance":
		if intent.Name == "" {
			return nil, fmt.Errorf("delete-application-instance requires name")
		}
		app := &appsv1alpha1.ApplicationInstance{}
		key := client.ObjectKey{Namespace: intent.Namespace, Name: intent.Name}
		if err := a.Client.Get(ctx, key, app); err != nil {
			if apierrors.IsNotFound(err) {
				return &AppliedRef{
					APIVersion: appsv1alpha1.GroupVersion.String(),
					Kind:       "ApplicationInstance",
					Namespace:  intent.Namespace,
					Name:       intent.Name,
				}, nil
			}
			return nil, fmt.Errorf("get application instance: %w", err)
		}
		if err := a.Client.Delete(ctx, app); err != nil && !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("delete application instance: %w", err)
		}
		return a.typedRef(app)
	default:
		return nil, fmt.Errorf("unsupported intent %q", intent.Intent)
	}
}

func (a *Applier) patchApply(ctx context.Context, obj *unstructured.Unstructured) error {
	//nolint:staticcheck // unstructured SSA uses Patch + client.Apply until typed ApplyConfiguration is generated.
	if err := a.Client.Patch(ctx, obj, client.Apply, client.FieldOwner(fieldManager), client.ForceOwnership); err != nil {
		if apierrors.IsConflict(err) {
			return fmt.Errorf("apply conflict: %w", err)
		}
		return fmt.Errorf("server-side apply: %w", err)
	}
	return nil
}

func (a *Applier) patchApplyTyped(ctx context.Context, obj client.Object) error {
	//nolint:staticcheck // server-side apply for typed CRs via Patch is the supported controller-runtime path.
	if err := a.Client.Patch(ctx, obj, client.Apply, client.FieldOwner(fieldManager), client.ForceOwnership); err != nil {
		if apierrors.IsConflict(err) {
			return fmt.Errorf("apply conflict: %w", err)
		}
		return fmt.Errorf("server-side apply: %w", err)
	}
	return nil
}

func (a *Applier) ensureManagedLabels(obj *unstructured.Unstructured) {
	labelsMap := obj.GetLabels()
	if labelsMap == nil {
		labelsMap = map[string]string{}
	}
	labelsMap[labels.ManagedByKey] = labels.ManagedByControlPlane
	if a.ClusterID != "" {
		labelsMap[labels.ClusterIDKey] = a.ClusterID
	}
	obj.SetLabels(labelsMap)
}

func (a *Applier) ensureManagedLabelsOnTyped(obj metav1.Object) {
	labelsMap := obj.GetLabels()
	if labelsMap == nil {
		labelsMap = map[string]string{}
	}
	labelsMap[labels.ManagedByKey] = labels.ManagedByControlPlane
	if a.ClusterID != "" {
		labelsMap[labels.ClusterIDKey] = a.ClusterID
	}
	obj.SetLabels(labelsMap)
}

type objectReference struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
}

func objectRef(obj *unstructured.Unstructured) *AppliedRef {
	ref := &AppliedRef{
		APIVersion: obj.GetAPIVersion(),
		Kind:       obj.GetKind(),
		Namespace:  obj.GetNamespace(),
		Name:       obj.GetName(),
		UID:        string(obj.GetUID()),
		Generation: obj.GetGeneration(),
	}
	return ref
}

func (a *Applier) typedRef(obj client.Object) (*AppliedRef, error) {
	gvk, err := apiutil.GVKForObject(obj, a.Scheme)
	if err != nil {
		return nil, fmt.Errorf("resolve GVK: %w", err)
	}
	meta, ok := obj.(metav1.Object)
	if !ok {
		return nil, fmt.Errorf("object is not metav1.Object")
	}
	return &AppliedRef{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Namespace:  meta.GetNamespace(),
		Name:       meta.GetName(),
		UID:        string(meta.GetUID()),
		Generation: meta.GetGeneration(),
	}, nil
}

// IsConflict reports whether err represents a field-manager conflict.
func IsConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "conflict")
}

// ConflictResult builds a job result for apply conflicts.
func ConflictResult(err error) JobResult {
	return JobResult{
		Outcome:   OutcomeConflict,
		Error:     err.Error(),
		Timestamp: time.Now().UTC(),
	}
}

// MarkApplied records idempotency for a resource generation (uid@generation).
func MarkApplied(uid types.UID, generation int64) string {
	return fmt.Sprintf("%s@%d", uid, generation)
}
