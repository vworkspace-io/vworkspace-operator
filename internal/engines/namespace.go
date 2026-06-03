package engines

import (
	"context"
	"fmt"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const maxK8sNameLen = 63

func materializedName(op *opsv1alpha1.Operation) string {
	base := fmt.Sprintf("%s--%s", op.Namespace, op.Name)
	if len(base) <= maxK8sNameLen {
		return base
	}
	return string(op.UID)
}

func resolveEngineLocationForStatus(
	ctx context.Context,
	c client.Client,
	op *opsv1alpha1.Operation,
	fallback func(context.Context, client.Client, *opsv1alpha1.Operation) (namespace, name string, err error),
) (namespace, name string, err error) {
	namespace, name, skip, err := resolveEngineLocationDetailed(ctx, c, op)
	if err != nil {
		return "", "", err
	}
	if skip {
		return fallback(ctx, c, op)
	}
	return namespace, name, nil
}

func resolveEngineLocationForCancel(ctx context.Context, c client.Client, op *opsv1alpha1.Operation) (namespace, name string, skip bool, err error) {
	return resolveEngineLocationDetailed(ctx, c, op)
}

func resolveEngineLocationDetailed(ctx context.Context, c client.Client, op *opsv1alpha1.Operation) (namespace, name string, skip bool, err error) {
	if ref := op.Status.EngineRef; ref != nil && ref.Namespace != "" && ref.Name != "" {
		return ref.Namespace, ref.Name, false, nil
	}
	target := &appsv1alpha1.ApplicationInstance{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: op.Namespace, Name: op.Spec.TargetRef.Name}, target); err != nil {
		if apierrors.IsNotFound(err) {
			return "", "", true, nil
		}
		return "", "", false, fmt.Errorf("get target ApplicationInstance: %w", err)
	}
	return targetNamespace(target), materializedName(op), false, nil
}

// NewEngineResourceRef returns the stable engine workload coordinates for an Operation.
func NewEngineResourceRef(op *opsv1alpha1.Operation, target *appsv1alpha1.ApplicationInstance, kind string) opsv1alpha1.EngineResourceRef {
	return opsv1alpha1.EngineResourceRef{
		Kind:      kind,
		Name:      materializedName(op),
		Namespace: targetNamespace(target),
	}
}

func setOwnerReferenceIfSameNamespace(op *opsv1alpha1.Operation, obj client.Object, ns string, scheme *runtime.Scheme) error {
	if ns != op.Namespace {
		return nil
	}
	return controllerutil.SetOwnerReference(op, obj, scheme)
}

func createMaterializedJob(ctx context.Context, c client.Client, op *opsv1alpha1.Operation, job *batchv1.Job, ns string) error {
	name := materializedName(op)
	var existing batchv1.Job
	err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, &existing)
	if err == nil {
		if err := verifyOperationOwnership(&existing.ObjectMeta, op); err != nil {
			return err
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get job: %w", err)
	}
	job.SetName(name)
	if err := setOwnerReferenceIfSameNamespace(op, job, ns, c.Scheme()); err != nil {
		return err
	}
	return c.Create(ctx, job)
}
