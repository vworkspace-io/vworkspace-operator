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
	"errors"
	"fmt"
	"time"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
	"github.com/vworkspace-io/vworkspace-operator/internal/conditions"
	"github.com/vworkspace-io/vworkspace-operator/internal/engines"
	"github.com/vworkspace-io/vworkspace-operator/internal/operations/approvals"
	"github.com/vworkspace-io/vworkspace-operator/internal/operations/concurrency"
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
	Scheme              *runtime.Scheme
	Registry            *engines.Registry
	Reporter            agent.StatusReporter
	ApprovalClaimSecret string
}

// +kubebuilder:rbac:groups=ops.vworkspace.io,resources=operations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ops.vworkspace.io,resources=operations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ops.vworkspace.io,resources=operations/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps.vworkspace.io,resources=applicationinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups=velero.io,resources=backups;restores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=argoproj.io,resources=workflows,verbs=get;list;watch;create;update;patch;delete

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

	if !op.DeletionTimestamp.IsZero() {
		return r.finalizeOperation(ctx, op)
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

	if approvals.NeedsApprovalCheck(op) {
		if blocked, reason := approvals.BlockReason(op, r.ApprovalClaimSecret); blocked {
			return r.blockOperation(ctx, op, "AwaitingApproval", reason)
		}
	}

	if conflict, reason, err := r.hasConflictingOperation(ctx, op); err != nil {
		return ctrl.Result{}, err
	} else if conflict {
		return r.blockOperation(ctx, op, "ConflictingOperation", reason)
	}

	if r.Registry == nil || !r.Registry.Has(op.Spec.Engine) {
		return r.blockOperation(ctx, op, "DependencyMissing", fmt.Sprintf("engine %q is not registered", op.Spec.Engine))
	}

	prevConditions := append([]metav1.Condition(nil), op.Status.Conditions...)

	engine, err := r.Registry.Get(op.Spec.Engine)
	if err != nil {
		return ctrl.Result{}, err
	}

	if op.Status.Phase == "" || op.Status.Phase == opsv1alpha1.PhasePending {
		if err := engine.Materialize(ctx, op, target); err != nil {
			log.Error(err, "materialize operation failed")
			return r.failOperation(ctx, op, err.Error())
		}
		ref := engines.NewEngineResourceRef(op, target, engines.EngineResourceKind(op.Spec.Engine))
		op.Status.EngineRef = &ref
		now := metav1.Now()
		op.Status.Phase = opsv1alpha1.PhaseRunning
		op.Status.StartedAt = &now
		op.Status.Conditions = conditions.Set(op.Status.Conditions, opsv1alpha1.ConditionAccepted, metav1.ConditionTrue, "AdmissionPassed", "Operation accepted")
		op.Status.Conditions = conditions.Set(op.Status.Conditions, opsv1alpha1.ConditionRunning, metav1.ConditionTrue, "EngineStarted", "Engine is running")
		op.Status.Conditions = conditions.Set(op.Status.Conditions, opsv1alpha1.ConditionBlocked, metav1.ConditionFalse, "Unblocked", "Operation started")
		if err := r.Status().Update(ctx, op); err != nil {
			return ctrl.Result{}, fmt.Errorf("update running status: %w", err)
		}
		r.reportConditions(op, prevConditions)
		prevConditions = append([]metav1.Condition(nil), op.Status.Conditions...)
	}

	if err := r.backfillEngineRefIfNeeded(ctx, op, target); err != nil {
		return ctrl.Result{}, err
	}

	status, err := engine.Status(ctx, op)
	if err != nil {
		return r.handleEngineStatusError(ctx, op, err)
	}

	if status.Done {
		if status.Failed {
			r.cancelEngineWorkloads(ctx, op)
		}
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
		r.reportConditions(op, prevConditions)
		return ctrl.Result{}, nil
	}

	op.Status.Phase = status.Phase
	if err := r.Status().Update(ctx, op); err != nil {
		return ctrl.Result{}, fmt.Errorf("update in-flight status: %w", err)
	}
	r.reportConditions(op, prevConditions)
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *OperationReconciler) backfillEngineRefIfNeeded(ctx context.Context, op *opsv1alpha1.Operation, target *appsv1alpha1.ApplicationInstance) error {
	if op.Status.Phase != opsv1alpha1.PhaseRunning {
		return nil
	}
	patch := false
	if op.Status.StartedAt == nil {
		now := metav1.Now()
		op.Status.StartedAt = &now
		patch = true
	}
	if op.Status.EngineRef == nil && engines.SupportsEngineRef(op.Spec.Engine) {
		ref := engines.NewEngineResourceRef(op, target, engines.EngineResourceKind(op.Spec.Engine))
		op.Status.EngineRef = &ref
		patch = true
	}
	if !patch {
		return nil
	}
	if err := r.Status().Update(ctx, op); err != nil {
		return fmt.Errorf("persist running status backfill: %w", err)
	}
	return nil
}

func (r *OperationReconciler) handleEngineStatusError(ctx context.Context, op *opsv1alpha1.Operation, err error) (ctrl.Result, error) {
	if errors.Is(err, engines.ErrWorkloadOwnership) || errors.Is(err, engines.ErrWorkloadAmbiguous) {
		return r.failOperation(ctx, op, err.Error())
	}
	if errors.Is(err, engines.ErrWorkloadMissing) {
		if op.Status.Phase == opsv1alpha1.PhaseRunning {
			if op.Status.StartedAt == nil {
				now := metav1.Now()
				op.Status.StartedAt = &now
				if err := r.Status().Update(ctx, op); err != nil {
					return ctrl.Result{}, fmt.Errorf("record startedAt for workload-missing grace: %w", err)
				}
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			if time.Since(op.Status.StartedAt.Time) < 60*time.Second {
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
		}
		return r.failOperation(ctx, op, err.Error())
	}
	return ctrl.Result{}, fmt.Errorf("engine status: %w", err)
}

func (r *OperationReconciler) cancelEngineWorkloads(ctx context.Context, op *opsv1alpha1.Operation) {
	log := logf.FromContext(ctx)
	if r.Registry == nil || !r.Registry.Has(op.Spec.Engine) {
		return
	}
	engine, err := r.Registry.Get(op.Spec.Engine)
	if err != nil {
		return
	}
	if err := engine.Cancel(ctx, op); err != nil {
		log.Error(err, "cancel engine workloads on failure")
	}
}

func (r *OperationReconciler) finalizeOperation(ctx context.Context, op *opsv1alpha1.Operation) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	if r.Registry != nil && r.Registry.Has(op.Spec.Engine) {
		engine, err := r.Registry.Get(op.Spec.Engine)
		if err != nil {
			return ctrl.Result{}, err
		}
		if err := engine.Cancel(ctx, op); err != nil {
			log.Error(err, "cancel engine resources failed")
			return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("cancel engine resources: %w", err)
		}
	}
	controllerutil.RemoveFinalizer(op, opsv1alpha1.OperationFinalizer)
	if err := r.Update(ctx, op); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *OperationReconciler) hasConflictingOperation(ctx context.Context, op *opsv1alpha1.Operation) (bool, string, error) {
	conflict, err := concurrency.FindConflict(ctx, r.Client, op)
	if err != nil {
		return false, "", err
	}
	if conflict == nil {
		return false, "", nil
	}
	return true, concurrency.FormatConflict(conflict), nil
}

func (r *OperationReconciler) blockOperation(ctx context.Context, op *opsv1alpha1.Operation, reason, message string) (ctrl.Result, error) {
	prevConditions := append([]metav1.Condition(nil), op.Status.Conditions...)
	op.Status.Phase = opsv1alpha1.PhasePending
	op.Status.Conditions = conditions.Set(op.Status.Conditions, opsv1alpha1.ConditionBlocked, metav1.ConditionTrue, reason, message)
	op.Status.Conditions = conditions.Set(op.Status.Conditions, opsv1alpha1.ConditionAccepted, metav1.ConditionFalse, reason, message)
	if err := r.Status().Update(ctx, op); err != nil {
		return ctrl.Result{}, fmt.Errorf("update blocked status: %w", err)
	}
	r.reportConditions(op, prevConditions)
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *OperationReconciler) failOperation(ctx context.Context, op *opsv1alpha1.Operation, message string) (ctrl.Result, error) {
	r.cancelEngineWorkloads(ctx, op)
	prevConditions := append([]metav1.Condition(nil), op.Status.Conditions...)
	now := metav1.Now()
	op.Status.Phase = opsv1alpha1.PhaseFailed
	op.Status.FinishedAt = &now
	op.Status.Conditions = conditions.Set(op.Status.Conditions, opsv1alpha1.ConditionFailed, metav1.ConditionTrue, "EngineFailed", message)
	if err := r.Status().Update(ctx, op); err != nil {
		return ctrl.Result{}, fmt.Errorf("update failed status: %w", err)
	}
	r.reportConditions(op, prevConditions)
	return ctrl.Result{}, nil
}

func (r *OperationReconciler) reportConditions(op *opsv1alpha1.Operation, prev []metav1.Condition) {
	ref := agent.ResourceRefFromMeta(opsv1alpha1.GroupVersion.WithKind("Operation"), op.ObjectMeta)
	r.Reporter.ReportConditionTransitions(ref, prev, op.Status.Conditions)
}

func (r *OperationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opsv1alpha1.Operation{}).
		Named("operation").
		Complete(r)
}
