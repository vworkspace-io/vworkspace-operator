/*
Copyright 2026 vWorkspace Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/conditions"
	"github.com/vworkspace-io/vworkspace-operator/internal/engines"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// OperationReconciler reconciles Operation resources.
type OperationReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Registry *engines.Registry
}

// +kubebuilder:rbac:groups=ops.vworkspace.io,resources=operations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ops.vworkspace.io,resources=operations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ops.vworkspace.io,resources=operations/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps.vworkspace.io,resources=applicationinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups=velero.io,resources=backups;restores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;update;patch

func (r *OperationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("operation", req.Name, "namespace", req.Namespace)

	op := &opsv1alpha1.Operation{}
	if err := r.Get(ctx, req.NamespacedName, op); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !controllerutil.ContainsFinalizer(op, opsv1alpha1.OperationFinalizer) {
		controllerutil.AddFinalizer(op, opsv1alpha1.OperationFinalizer)
		if err := r.Update(ctx, op); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if err := ValidateOperationSpec(op); err != nil {
		return r.blockOperation(ctx, op, "ValidationFailed", err.Error())
	}

	target := &appsv1alpha1.ApplicationInstance{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: op.Namespace, Name: op.Spec.TargetRef.Name}, target); err != nil {
		if apierrors.IsNotFound(err) {
			return r.blockOperation(ctx, op, "TargetNotReady", "target ApplicationInstance not found")
		}
		return ctrl.Result{}, fmt.Errorf("get target: %w", err)
	}

	if conflict, reason, err := r.hasConflictingOperation(ctx, op); err != nil {
		return ctrl.Result{}, err
	} else if conflict {
		return r.blockOperation(ctx, op, "ConflictingOperation", reason)
	}

	if r.Registry == nil || !r.Registry.Has(op.Spec.Engine) {
		return r.blockOperation(ctx, op, "DependencyMissing", fmt.Sprintf("engine %q is not registered", op.Spec.Engine))
	}

	engine, err := r.Registry.Get(op.Spec.Engine)
	if err != nil {
		return ctrl.Result{}, err
	}

	if op.Status.Phase == "" || op.Status.Phase == opsv1alpha1.PhasePending {
		if err := engine.Materialize(ctx, op, target); err != nil {
			log.Error(err, "materialize operation failed")
			return r.failOperation(ctx, op, err.Error())
		}
		now := metav1.Now()
		op.Status.Phase = opsv1alpha1.PhaseRunning
		op.Status.StartedAt = &now
		op.Status.Conditions = conditions.Set(op.Status.Conditions, opsv1alpha1.ConditionAccepted, metav1.ConditionTrue, "AdmissionPassed", "Operation accepted")
		op.Status.Conditions = conditions.Set(op.Status.Conditions, opsv1alpha1.ConditionRunning, metav1.ConditionTrue, "EngineStarted", "Engine is running")
		op.Status.Conditions = conditions.Set(op.Status.Conditions, opsv1alpha1.ConditionBlocked, metav1.ConditionFalse, "Unblocked", "Operation started")
		if err := r.Status().Update(ctx, op); err != nil {
			return ctrl.Result{}, fmt.Errorf("update running status: %w", err)
		}
	}

	status, err := engine.Status(ctx, op)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("engine status: %w", err)
	}

	if status.Done {
		finished := metav1.Now()
		op.Status.FinishedAt = &finished
		op.Status.Phase = status.Phase
		if status.Failed {
			op.Status.Conditions = conditions.Set(op.Status.Conditions, opsv1alpha1.ConditionFailed, metav1.ConditionTrue, status.Reason, status.Message)
			op.Status.Conditions = conditions.Set(op.Status.Conditions, opsv1alpha1.ConditionRunning, metav1.ConditionFalse, "EngineNotStarted", "Engine finished with failure")
		} else {
			op.Status.Conditions = conditions.Set(op.Status.Conditions, opsv1alpha1.ConditionSucceeded, metav1.ConditionTrue, status.Reason, status.Message)
			op.Status.Conditions = conditions.Set(op.Status.Conditions, opsv1alpha1.ConditionRunning, metav1.ConditionFalse, "EngineNotStarted", "Engine completed")
		}
		if len(status.Outputs) > 0 {
			raw, marshalErr := json.Marshal(status.Outputs)
			if marshalErr == nil {
				op.Status.Outputs.Raw = raw
			}
		}
		if err := r.Status().Update(ctx, op); err != nil {
			return ctrl.Result{}, fmt.Errorf("update terminal status: %w", err)
		}
		return ctrl.Result{}, nil
	}

	op.Status.Phase = status.Phase
	if err := r.Status().Update(ctx, op); err != nil {
		return ctrl.Result{}, fmt.Errorf("update in-flight status: %w", err)
	}
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

func (r *OperationReconciler) hasConflictingOperation(ctx context.Context, op *opsv1alpha1.Operation) (bool, string, error) {
	list := &opsv1alpha1.OperationList{}
	if err := r.List(ctx, list, client.InNamespace(op.Namespace)); err != nil {
		return false, "", fmt.Errorf("list operations: %w", err)
	}
	for _, item := range list.Items {
		if item.Name == op.Name {
			continue
		}
		if item.Spec.TargetRef.Name != op.Spec.TargetRef.Name {
			continue
		}
		if item.Status.Phase != opsv1alpha1.PhaseRunning && item.Status.Phase != opsv1alpha1.PhasePending {
			continue
		}
		if conflicts(item.Spec.Type, op.Spec.Type) {
			return true, fmt.Sprintf("conflicts with operation %q (%s)", item.Name, item.Spec.Type), nil
		}
	}
	return false, "", nil
}

func conflicts(existing, incoming opsv1alpha1.OperationType) bool {
	switch incoming {
	case opsv1alpha1.OperationTypeUpgrade:
		return existing == opsv1alpha1.OperationTypeUpgrade
	case opsv1alpha1.OperationTypeRestore:
		return existing == opsv1alpha1.OperationTypeUpgrade || existing == opsv1alpha1.OperationTypeBackup
	case opsv1alpha1.OperationTypeBackup:
		return existing == opsv1alpha1.OperationTypeRestore
	default:
		return false
	}
}

func (r *OperationReconciler) blockOperation(ctx context.Context, op *opsv1alpha1.Operation, reason, message string) (ctrl.Result, error) {
	op.Status.Phase = opsv1alpha1.PhasePending
	op.Status.Conditions = conditions.Set(op.Status.Conditions, opsv1alpha1.ConditionBlocked, metav1.ConditionTrue, reason, message)
	op.Status.Conditions = conditions.Set(op.Status.Conditions, opsv1alpha1.ConditionAccepted, metav1.ConditionFalse, reason, message)
	if err := r.Status().Update(ctx, op); err != nil {
		return ctrl.Result{}, fmt.Errorf("update blocked status: %w", err)
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *OperationReconciler) failOperation(ctx context.Context, op *opsv1alpha1.Operation, message string) (ctrl.Result, error) {
	now := metav1.Now()
	op.Status.Phase = opsv1alpha1.PhaseFailed
	op.Status.FinishedAt = &now
	op.Status.Conditions = conditions.Set(op.Status.Conditions, opsv1alpha1.ConditionFailed, metav1.ConditionTrue, "EngineFailed", message)
	if err := r.Status().Update(ctx, op); err != nil {
		return ctrl.Result{}, fmt.Errorf("update failed status: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *OperationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opsv1alpha1.Operation{}).
		Named("operation").
		Complete(r)
}
