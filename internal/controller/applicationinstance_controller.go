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
	"fmt"
	"time"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/conditions"
	"github.com/vworkspace-io/vworkspace-operator/internal/helmengine"
	"github.com/vworkspace-io/vworkspace-operator/internal/labels"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// ApplicationInstanceReconciler reconciles ApplicationInstance resources.
type ApplicationInstanceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Engine helmengine.Engine
}

// +kubebuilder:rbac:groups=apps.vworkspace.io,resources=applicationinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.vworkspace.io,resources=applicationinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.vworkspace.io,resources=applicationinstances/finalizers,verbs=update
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmrepositories;ocirepositories,verbs=get;list;watch;create;update;patch;delete

func (r *ApplicationInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues(
		"cluster_id", req.Namespace,
		"applicationinstance", req.Name,
		"namespace", req.Namespace,
	)

	app := &appsv1alpha1.ApplicationInstance{}
	if err := r.Get(ctx, req.NamespacedName, app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !controllerutil.ContainsFinalizer(app, appsv1alpha1.ApplicationInstanceFinalizer) {
		controllerutil.AddFinalizer(app, appsv1alpha1.ApplicationInstanceFinalizer)
		if err := r.Update(ctx, app); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if !app.DeletionTimestamp.IsZero() {
		return r.finalize(ctx, app)
	}

	if err := ValidateApplicationInstanceSpec(app); err != nil {
		return r.setBlocked(ctx, app, "ValidationFailed", err.Error())
	}

	if r.Engine == nil {
		return r.setBlocked(ctx, app, "MissingDependencies", "helm engine is not configured")
	}

	app.Status.ObservedGeneration = app.Generation
	now := metav1.Now()
	app.Status.LastReconcileTime = &now
	app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReconciling, metav1.ConditionTrue, "HelmReleaseInstalling", "Ensuring HelmRelease")

	if err := r.Engine.EnsureRelease(ctx, app); err != nil {
		log.Error(err, "ensure helm release failed")
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReady, metav1.ConditionFalse, "HelmReleaseFailed", err.Error())
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReconciling, metav1.ConditionFalse, "Stable", "Reconciliation failed")
		if statusErr := r.Status().Update(ctx, app); statusErr != nil {
			return ctrl.Result{}, fmt.Errorf("update status after ensure failure: %w", statusErr)
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	app.Status.HelmReleaseRef = &appsv1alpha1.HelmReleaseRef{
		Name:      app.Spec.Release.Name,
		Namespace: app.Namespace,
	}
	app.Status.LastAppliedChart = &appsv1alpha1.ChartSnapshot{
		SourceType: app.Spec.Chart.SourceType,
		URL:        app.Spec.Chart.URL,
		Name:       app.Spec.Chart.Name,
		Version:    app.Spec.Chart.Version,
	}

	snapshot, err := r.Engine.SyncStatus(ctx, app)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("sync helm status: %w", err)
	}
	r.applyStatusSnapshot(app, snapshot)

	if err := r.Status().Update(ctx, app); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}

	if snapshot != nil && !snapshot.Ready {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func (r *ApplicationInstanceReconciler) finalize(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionDeleting, metav1.ConditionTrue, "Uninstalling", "Removing HelmRelease")
	if err := r.Status().Update(ctx, app); err != nil {
		return ctrl.Result{}, fmt.Errorf("update deleting status: %w", err)
	}
	if r.Engine != nil {
		if err := r.Engine.DeleteRelease(ctx, app); err != nil {
			log.Error(err, "delete helm release failed")
			return ctrl.Result{RequeueAfter: 15 * time.Second}, err
		}
	}
	controllerutil.RemoveFinalizer(app, appsv1alpha1.ApplicationInstanceFinalizer)
	if err := r.Update(ctx, app); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *ApplicationInstanceReconciler) setBlocked(ctx context.Context, app *appsv1alpha1.ApplicationInstance, reason, message string) (ctrl.Result, error) {
	app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionBlocked, metav1.ConditionTrue, reason, message)
	app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReady, metav1.ConditionFalse, reason, message)
	if err := r.Status().Update(ctx, app); err != nil {
		return ctrl.Result{}, fmt.Errorf("update blocked status: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *ApplicationInstanceReconciler) applyStatusSnapshot(app *appsv1alpha1.ApplicationInstance, snapshot *helmengine.StatusSnapshot) {
	if snapshot == nil {
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReady, metav1.ConditionUnknown, "Reconciling", "Waiting for HelmRelease status")
		return
	}
	if snapshot.Reconciling {
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReconciling, metav1.ConditionTrue, snapshot.Reason, snapshot.Message)
	} else {
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReconciling, metav1.ConditionFalse, "Stable", "No reconciliation in flight")
	}
	if snapshot.Degraded {
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionDegraded, metav1.ConditionTrue, snapshot.Reason, snapshot.Message)
	} else {
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionDegraded, metav1.ConditionFalse, "Recovered", "Release is healthy")
	}
	if snapshot.Ready {
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReady, metav1.ConditionTrue, "HelmReleaseReady", snapshot.Message)
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionBlocked, metav1.ConditionFalse, "Unblocked", "Reconciliation can proceed")
	} else {
		reason := snapshot.Reason
		if reason == "" {
			reason = "Reconciling"
		}
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReady, metav1.ConditionUnknown, reason, snapshot.Message)
	}
	_ = labels.ManagedByOperator
}

func (r *ApplicationInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1alpha1.ApplicationInstance{}).
		Named("applicationinstance").
		Complete(r)
}
