package engines

import (
	"context"
	"fmt"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
)

// Status summarizes engine execution state.
type Status struct {
	Phase   opsv1alpha1.OperationPhase
	Reason  string
	Message string
	Outputs map[string]string
	Done    bool
	Failed  bool
}

// Engine executes an Operation against a target ApplicationInstance.
type Engine interface {
	Name() opsv1alpha1.OperationEngine
	Materialize(ctx context.Context, op *opsv1alpha1.Operation, target *appsv1alpha1.ApplicationInstance) error
	Status(ctx context.Context, op *opsv1alpha1.Operation) (Status, error)
	Cancel(ctx context.Context, op *opsv1alpha1.Operation) error
}

// Registry resolves engines by name.
type Registry struct {
	engines map[opsv1alpha1.OperationEngine]Engine
}

func NewRegistry(items ...Engine) *Registry {
	r := &Registry{engines: map[opsv1alpha1.OperationEngine]Engine{}}
	for _, item := range items {
		r.engines[item.Name()] = item
	}
	return r
}

func (r *Registry) Get(name opsv1alpha1.OperationEngine) (Engine, error) {
	engine, ok := r.engines[name]
	if !ok {
		return nil, fmt.Errorf("engine %q is not registered", name)
	}
	return engine, nil
}

func (r *Registry) Has(name opsv1alpha1.OperationEngine) bool {
	_, ok := r.engines[name]
	return ok
}
