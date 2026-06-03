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

func materializedNamespace(ctx context.Context, c client.Client, op *opsv1alpha1.Operation) (string, error) {
	target := &appsv1alpha1.ApplicationInstance{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: op.Namespace, Name: op.Spec.TargetRef.Name}, target); err != nil {
		return "", fmt.Errorf("get target ApplicationInstance: %w", err)
	}
	return targetNamespace(target), nil
}

func setOwnerReferenceIfSameNamespace(op *opsv1alpha1.Operation, obj client.Object, ns string, scheme *runtime.Scheme) error {
	if ns != op.Namespace {
		return nil
	}
	return controllerutil.SetOwnerReference(op, obj, scheme)
}

func createMaterializedJob(ctx context.Context, c client.Client, op *opsv1alpha1.Operation, job *batchv1.Job, ns string) error {
	var existing batchv1.Job
	err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: op.Name}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get job: %w", err)
	}
	if err := setOwnerReferenceIfSameNamespace(op, job, ns, c.Scheme()); err != nil {
		return err
	}
	return c.Create(ctx, job)
}
