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
	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
	"github.com/vworkspace-io/vworkspace-operator/internal/conditions"
	"github.com/vworkspace-io/vworkspace-operator/internal/helmengine"
	"github.com/vworkspace-io/vworkspace-operator/internal/seaweedengine"
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
	Scheme        *runtime.Scheme
	Engine        helmengine.Engine
	SeaweedEngine seaweedengine.Engine
	Reporter      agent.StatusReporter
}

// +kubebuilder:rbac:groups=apps.vworkspace.io,resources=applicationinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.vworkspace.io,resources=applicationinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.vworkspace.io,resources=applicationinstances/finalizers,verbs=update
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmrepositories;ocirepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=seaweed.seaweedfs.com,resources=seaweeds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=seaweed.seaweedfs.com,resources=seaweeds/status,verbs=get

func (r *ApplicationInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues(
		"cluster_id", req.Namespace,
		"applicationinstance", req.Name,
		"namespace", req.Namespace,
	)
	ctx = logf.IntoContext(ctx, log)

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

	if app.Spec.IsPlaceholder() {
		return r.reconcilePlaceholder(ctx, app)
	}

	if seaweedengine.IsSeaweedWorkload(app) {
		return r.reconcileSeaweed(ctx, app)
	}

	return r.reconcileHelm(ctx, app)
}

func (r *ApplicationInstanceReconciler) reconcileHelm(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	prevConditions := append([]metav1.Condition(nil), app.Status.Conditions...)

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
		r.reportConditions(app, prevConditions)
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
	r.applyHelmStatusSnapshot(app, snapshot)

	if err := r.Status().Update(ctx, app); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}
	r.reportConditions(app, prevConditions)

	if snapshot != nil && !snapshot.Ready {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func (r *ApplicationInstanceReconciler) reconcileSeaweed(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	prevConditions := append([]metav1.Condition(nil), app.Status.Conditions...)

	if r.SeaweedEngine == nil {
		return r.setBlocked(ctx, app, "MissingDependencies", "seaweed engine is not configured")
	}

	if r.Engine != nil {
		exists, err := r.Engine.ReleaseExists(ctx, app)
		if err != nil {
			return r.setBlocked(ctx, app, "HelmMigrationFailed", fmt.Sprintf("check legacy Helm release: %v", err))
		}
		if exists {
			if err := r.Engine.DeleteRelease(ctx, app); err != nil {
				return r.setBlocked(ctx, app, "HelmMigrationFailed", fmt.Sprintf("remove legacy Helm release: %v", err))
			}
			app.Status.HelmReleaseRef = nil
			app.Status.Endpoints = nil
			app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReconciling, metav1.ConditionTrue, "SeaweedMigrating", "Removing legacy Helm release")
			app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReady, metav1.ConditionUnknown, "SeaweedMigrating", "Migrating from Helm to native Seaweed CR")
			app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionDegraded, metav1.ConditionFalse, "SeaweedMigrating", "Legacy Helm release is being removed")
			if err := r.Status().Update(ctx, app); err != nil {
				return ctrl.Result{}, fmt.Errorf("update status after helm migration: %w", err)
			}
			return ctrl.Result{Requeue: true}, nil
		}
	}

	app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionBlocked, metav1.ConditionFalse, "Unblocked", "Reconciliation can proceed")
	app.Status.HelmReleaseRef = nil
	app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReconciling, metav1.ConditionTrue, "SeaweedReconciling", "Ensuring Seaweed CR")

	if err := r.SeaweedEngine.EnsureSeaweed(ctx, app); err != nil {
		log.Error(err, "ensure Seaweed CR failed")
		app.Status.Endpoints = nil
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReady, metav1.ConditionFalse, "SeaweedFailed", err.Error())
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReconciling, metav1.ConditionFalse, "SeaweedFailed", err.Error())
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionDegraded, metav1.ConditionTrue, "SeaweedFailed", err.Error())
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionBlocked, metav1.ConditionFalse, "Unblocked", "Reconciliation can proceed")
		if statusErr := r.Status().Update(ctx, app); statusErr != nil {
			return ctrl.Result{}, fmt.Errorf("update status after ensure failure: %w", statusErr)
		}
		r.reportConditions(app, prevConditions)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	app.Status.ObservedGeneration = app.Generation
	now := metav1.Now()
	app.Status.LastReconcileTime = &now
	if app.Spec.Chart != nil {
		app.Status.LastAppliedChart = &appsv1alpha1.ChartSnapshot{
			SourceType: app.Spec.Chart.SourceType,
			URL:        app.Spec.Chart.URL,
			Name:       app.Spec.Chart.Name,
			Version:    app.Spec.Chart.Version,
		}
	} else {
		app.Status.LastAppliedChart = nil
	}

	snapshot, err := r.SeaweedEngine.SyncStatus(ctx, app)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("sync Seaweed status: %w", err)
	}
	r.applySeaweedStatusSnapshot(app, snapshot)

	if err := r.Status().Update(ctx, app); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}
	r.reportConditions(app, prevConditions)

	if snapshot == nil || !snapshot.Ready {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

// reconcilePlaceholder brings a placeholder (cluster-ops) instance to Ready
// without any Helm interaction. The instance owns no workload; it only needs to
// exist, advertise its capability annotations, and report Ready so cluster-scoped
// Operations can target it.
func (r *ApplicationInstanceReconciler) reconcilePlaceholder(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (ctrl.Result, error) {
	prevConditions := append([]metav1.Condition(nil), app.Status.Conditions...)
	app.Status.ObservedGeneration = app.Generation
	now := metav1.Now()
	app.Status.LastReconcileTime = &now
	// A placeholder owns no Helm release; clear any Helm-derived status left over
	// from a prior managed lifecycle so we never report Ready alongside a stale
	// release reference.
	app.Status.HelmReleaseRef = nil
	app.Status.LastAppliedChart = nil
	app.Status.Endpoints = nil
	app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReconciling, metav1.ConditionFalse, "Stable", "Placeholder instance has no reconciliation in flight")
	app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionDegraded, metav1.ConditionFalse, "Recovered", "Placeholder instance owns no workload")
	app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReady, metav1.ConditionTrue, "Placeholder", "Placeholder instance is ready (no Helm release)")
	app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionBlocked, metav1.ConditionFalse, "Unblocked", "Reconciliation can proceed")
	if err := r.Status().Update(ctx, app); err != nil {
		return ctrl.Result{}, fmt.Errorf("update placeholder status: %w", err)
	}
	r.reportConditions(app, prevConditions)
	return ctrl.Result{}, nil
}

func (r *ApplicationInstanceReconciler) finalize(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	prevConditions := append([]metav1.Condition(nil), app.Status.Conditions...)

	// Placeholder instances own no Helm release; finalize is a clean no-op
	// beyond removing the finalizer.
	if app.Spec.IsPlaceholder() {
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionDeleting, metav1.ConditionTrue, "Deleting", "Removing placeholder instance (nothing to uninstall)")
		if err := r.Status().Update(ctx, app); err != nil {
			return ctrl.Result{}, fmt.Errorf("update deleting status: %w", err)
		}
		r.reportConditions(app, prevConditions)
		controllerutil.RemoveFinalizer(app, appsv1alpha1.ApplicationInstanceFinalizer)
		if err := r.Update(ctx, app); err != nil {
			return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
		}
		return ctrl.Result{}, nil
	}

	if seaweedengine.IsSeaweedWorkload(app) {
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionDeleting, metav1.ConditionTrue, "Uninstalling", "Removing Seaweed CR")
		if err := r.Status().Update(ctx, app); err != nil {
			return ctrl.Result{}, fmt.Errorf("update deleting status: %w", err)
		}
		r.reportConditions(app, prevConditions)
		if r.SeaweedEngine == nil {
			return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("seaweed engine is not configured")
		}
		if r.Engine != nil {
			exists, err := r.Engine.ReleaseExists(ctx, app)
			if err != nil {
				return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("check legacy Helm release: %w", err)
			}
			if exists {
				if err := r.Engine.DeleteRelease(ctx, app); err != nil {
					log.Error(err, "delete legacy Helm release during Seaweed finalize")
					return ctrl.Result{RequeueAfter: 15 * time.Second}, err
				}
			}
		}
		if err := r.SeaweedEngine.DeleteSeaweed(ctx, app); err != nil {
			log.Error(err, "delete Seaweed CR failed")
			return ctrl.Result{RequeueAfter: 15 * time.Second}, err
		}
		exists, err := r.SeaweedEngine.SeaweedExists(ctx, app)
		if err != nil {
			return ctrl.Result{RequeueAfter: 15 * time.Second}, err
		}
		if exists {
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
		controllerutil.RemoveFinalizer(app, appsv1alpha1.ApplicationInstanceFinalizer)
		if err := r.Update(ctx, app); err != nil {
			return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
		}
		return ctrl.Result{}, nil
	}

	app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionDeleting, metav1.ConditionTrue, "Uninstalling", "Removing HelmRelease")
	if err := r.Status().Update(ctx, app); err != nil {
		return ctrl.Result{}, fmt.Errorf("update deleting status: %w", err)
	}
	r.reportConditions(app, prevConditions)
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
	prevConditions := append([]metav1.Condition(nil), app.Status.Conditions...)
	app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionBlocked, metav1.ConditionTrue, reason, message)
	app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReady, metav1.ConditionFalse, reason, message)
	if err := r.Status().Update(ctx, app); err != nil {
		return ctrl.Result{}, fmt.Errorf("update blocked status: %w", err)
	}
	r.reportConditions(app, prevConditions)
	return ctrl.Result{}, nil
}

func (r *ApplicationInstanceReconciler) reportConditions(app *appsv1alpha1.ApplicationInstance, prev []metav1.Condition) {
	ref := agent.ResourceRefFromMeta(appsv1alpha1.GroupVersion.WithKind("ApplicationInstance"), app.ObjectMeta)
	r.Reporter.ReportConditionTransitions(ref, prev, app.Status.Conditions)
}

func (r *ApplicationInstanceReconciler) applyHelmStatusSnapshot(app *appsv1alpha1.ApplicationInstance, snapshot *helmengine.StatusSnapshot) {
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
}

func (r *ApplicationInstanceReconciler) applySeaweedStatusSnapshot(app *appsv1alpha1.ApplicationInstance, snapshot *seaweedengine.StatusSnapshot) {
	if snapshot == nil {
		app.Status.Endpoints = nil
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReconciling, metav1.ConditionTrue, "Reconciling", "Waiting for Seaweed status")
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionDegraded, metav1.ConditionFalse, "Reconciling", "Seaweed status not yet available")
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReady, metav1.ConditionUnknown, "Reconciling", "Waiting for Seaweed status")
		return
	}
	if snapshot.S3Endpoint != "" {
		app.Status.Endpoints = []appsv1alpha1.EndpointStatus{{
			Name:  "s3",
			URL:   snapshot.S3Endpoint,
			Type:  "s3",
			Notes: "In-cluster SeaweedFS S3 gateway (port 8333)",
		}}
	} else {
		app.Status.Endpoints = nil
	}
	if snapshot.Reconciling {
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReconciling, metav1.ConditionTrue, snapshot.Reason, snapshot.Message)
	} else {
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReconciling, metav1.ConditionFalse, "Stable", "No reconciliation in flight")
	}
	if snapshot.Degraded {
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionDegraded, metav1.ConditionTrue, snapshot.Reason, snapshot.Message)
	} else {
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionDegraded, metav1.ConditionFalse, "Recovered", "Seaweed cluster is healthy")
	}
	if snapshot.Ready {
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReady, metav1.ConditionTrue, "SeaweedReady", snapshot.Message)
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionBlocked, metav1.ConditionFalse, "Unblocked", "Reconciliation can proceed")
	} else {
		reason := snapshot.Reason
		if reason == "" {
			reason = "Reconciling"
		}
		readyStatus := metav1.ConditionUnknown
		if snapshot.Degraded || reason == "SeaweedFailed" {
			readyStatus = metav1.ConditionFalse
		}
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReady, readyStatus, reason, snapshot.Message)
		app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionBlocked, metav1.ConditionFalse, "Unblocked", "Reconciliation can proceed")
	}
}

func (r *ApplicationInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1alpha1.ApplicationInstance{}).
		Owns(seaweedengine.SeaweedObject()).
		Named("applicationinstance").
		Complete(r)
}
