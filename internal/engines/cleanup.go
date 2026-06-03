package engines

import (
	"context"
	"fmt"

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func deleteJobsLabeledByOperation(ctx context.Context, c client.Client, op *opsv1alpha1.Operation) error {
	list := &batchv1.JobList{}
	if err := c.List(ctx, list, client.MatchingLabels{OperationLabelKey: string(op.UID)}); err != nil {
		return fmt.Errorf("list jobs for operation %s: %w", op.UID, err)
	}
	for i := range list.Items {
		job := &list.Items[i]
		if err := c.Delete(ctx, job); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete job %s/%s: %w", job.Namespace, job.Name, err)
		}
	}
	return nil
}

func deleteWorkflowsLabeledByOperation(ctx context.Context, c client.Client, op *opsv1alpha1.Operation) error {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "WorkflowList"})
	if err := c.List(ctx, list, client.MatchingLabels{OperationLabelKey: string(op.UID)}); err != nil {
		return fmt.Errorf("list workflows for operation %s: %w", op.UID, err)
	}
	for i := range list.Items {
		wf := &list.Items[i]
		if err := c.Delete(ctx, wf); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete workflow %s/%s: %w", wf.GetNamespace(), wf.GetName(), err)
		}
	}
	return nil
}
