package engines

import opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"

// EngineResourceKind returns the Kubernetes kind name for a materialized engine workload.
func EngineResourceKind(engine opsv1alpha1.OperationEngine) string {
	switch engine {
	case opsv1alpha1.EngineJob, opsv1alpha1.EngineHelmHookJob:
		return "Job"
	case opsv1alpha1.EngineWorkflow:
		return "Workflow"
	default:
		return string(engine)
	}
}
