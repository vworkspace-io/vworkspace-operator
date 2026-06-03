package engines

import (
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/labels"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const OperationLabelKey = "ops.vworkspace.io/operation"

func applyOperationLabels(meta *metav1.ObjectMeta, op *opsv1alpha1.Operation) {
	if meta.Labels == nil {
		meta.Labels = map[string]string{}
	}
	meta.Labels[labels.ManagedByKey] = labels.ManagedByOperator
	meta.Labels[OperationLabelKey] = string(op.UID)
}
