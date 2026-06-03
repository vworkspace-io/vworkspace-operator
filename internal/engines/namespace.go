package engines

import (
	"context"
	"fmt"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
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
