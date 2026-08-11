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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
	"github.com/vworkspace-io/vworkspace-operator/internal/conditions"
	"github.com/vworkspace-io/vworkspace-operator/internal/helmengine"
	"github.com/vworkspace-io/vworkspace-operator/internal/seaweedengine"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const reportedManagedStorageAccessKeyAnnotation = "vworkspace.io/reported-managed-storage-key"

// ApplicationInstanceReconciler reconciles ApplicationInstance resources.
type ApplicationInstanceReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Engine        helmengine.Engine
	SeaweedEngine seaweedengine.Engine
	Reporter      agent.StatusReporter

	storageDeliveryMu sync.Mutex
	storageInFlight   map[string]string // namespace/name -> reportKey enqueued, not yet accepted by control plane
	storageAckPending map[string]string // namespace/name -> reportKey delivered, annotation patch pending
}

// +kubebuilder:rbac:groups=apps.vworkspace.io,resources=applicationinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.vworkspace.io,resources=applicationinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.vworkspace.io,resources=applicationinstances/finalizers,verbs=update
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmrepositories;ocirepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=seaweed.seaweedfs.com,resources=seaweeds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=seaweed.seaweedfs.com,resources=seaweeds/status,verbs=get
// +kubebuilder:rbac:groups=seaweed.seaweedfs.com,resources=s3credentials,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

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
		r.reportConditions(ctx, app, prevConditions)
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
	r.reportConditions(ctx, app, prevConditions)

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
	if r.Engine == nil {
		return r.setBlocked(ctx, app, "MissingDependencies", "helm engine is not configured; required to detect legacy Helm releases during Seaweed migration")
	}

	exists, err := r.Engine.ReleaseExists(ctx, app)
	if err != nil {
		return r.setBlocked(ctx, app, "HelmMigrationFailed", fmt.Sprintf("check legacy Helm release: %v", err))
	}
	if exists {
		return r.setBlocked(ctx, app, "HelmMigrationRequired",
			"legacy Helm release detected; remove the HelmRelease manually or recreate the ApplicationInstance before native Seaweed reconciliation")
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
		r.reportConditions(ctx, app, prevConditions)
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
	r.reportConditions(ctx, app, prevConditions)

	if snapshot == nil || !snapshot.Ready || (snapshot.Ready && snapshot.S3Endpoint == "" && snapshot.HasS3) {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	return r.reconcileSeaweedManagedStorage(ctx, app)
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
	r.reportConditions(ctx, app, prevConditions)
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
		r.reportConditions(ctx, app, prevConditions)
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
		r.reportConditions(ctx, app, prevConditions)
		if r.SeaweedEngine == nil {
			app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionBlocked, metav1.ConditionTrue, "MissingDependencies", "seaweed engine is not configured")
			app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionDeleting, metav1.ConditionTrue, "Blocked", "Cannot uninstall Seaweed CR until seaweed engine is configured")
			if err := r.Status().Update(ctx, app); err != nil {
				return ctrl.Result{}, fmt.Errorf("update blocked status during finalize: %w", err)
			}
			r.reportConditions(ctx, app, prevConditions)
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
		if err := r.SeaweedEngine.DeleteSeaweed(ctx, app); err != nil {
			log.Error(err, "delete Seaweed CR failed")
			return ctrl.Result{RequeueAfter: 15 * time.Second}, err
		}
		// Deletion is the supported cleanup path for legacy Helm releases blocked during live reconcile.
		if r.Engine != nil {
			helmExists, err := r.Engine.ReleaseExists(ctx, app)
			if err != nil {
				return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("check legacy Helm release: %w", err)
			}
			if helmExists {
				if err := r.Engine.DeleteRelease(ctx, app); err != nil {
					log.Error(err, "delete legacy Helm release during Seaweed finalize")
					return ctrl.Result{RequeueAfter: 15 * time.Second}, err
				}
			}
		}
		exists, err := r.SeaweedEngine.SeaweedExists(ctx, app)
		if err != nil {
			return ctrl.Result{RequeueAfter: 15 * time.Second}, err
		}
		if exists {
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
		if r.Engine != nil {
			helmExists, err := r.Engine.ReleaseExists(ctx, app)
			if err != nil {
				return ctrl.Result{RequeueAfter: 15 * time.Second}, fmt.Errorf("check legacy Helm release: %w", err)
			}
			if helmExists {
				return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
			}
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
	r.reportConditions(ctx, app, prevConditions)
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
	r.reportConditions(ctx, app, prevConditions)
	return ctrl.Result{}, nil
}

func (r *ApplicationInstanceReconciler) reportConditions(_ context.Context, app *appsv1alpha1.ApplicationInstance, prev []metav1.Condition) {
	ref := agent.ResourceRefFromMeta(appsv1alpha1.GroupVersion.WithKind("ApplicationInstance"), app.ObjectMeta)
	var enrich agent.ConditionEventEnricher
	if seaweedengine.IsSeaweedWorkload(app) {
		enrich = func(condition metav1.Condition) agent.EventExtras {
			if condition.Type != appsv1alpha1.ConditionReady || condition.Status != metav1.ConditionTrue {
				return agent.EventExtras{}
			}
			extras := agent.EventExtras{Endpoints: agentEndpointsFromStatus(app.Status.Endpoints)}
			return extras
		}
	}
	r.Reporter.ReportConditionTransitions(ref, prev, app.Status.Conditions, enrich)
}

func (r *ApplicationInstanceReconciler) reconcileSeaweedManagedStorage(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	if !conditions.IsTrue(app.Status.Conditions, appsv1alpha1.ConditionReady) {
		return ctrl.Result{}, nil
	}

	ms, pending, err := r.SeaweedEngine.ResolveManagedStorageState(ctx, app)
	if err != nil {
		log.Error(err, "ResolveManagedStorage failed")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if ms == nil {
		if pending {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{}, nil
	}

	ready, ok := conditions.Get(app.Status.Conditions, appsv1alpha1.ConditionReady)
	if !ok {
		return ctrl.Result{}, nil
	}
	ready.LastTransitionTime = metav1.Now()
	ready.Reason = "ManagedStorageReady"
	ready.Message = "Inline S3 credentials available for control-plane registry sync"

	reportKey := managedStorageReportKey(ms)
	if app.Annotations[reportedManagedStorageAccessKeyAnnotation] == reportKey {
		r.clearStorageDeliveryState(app.Namespace, app.Name)
		return ctrl.Result{}, nil
	}

	ref := agent.ResourceRefFromMeta(appsv1alpha1.GroupVersion.WithKind("ApplicationInstance"), app.ObjectMeta)
	eventKey := agent.ManagedStorageEventKey(ref, ready, reportKey)

	if inFlightKey, ok := r.inFlightStorageReportKey(app.Namespace, app.Name); ok {
		if inFlightKey != reportKey {
			r.clearStorageInFlight(app.Namespace, app.Name)
		} else {
			return ctrl.Result{}, nil
		}
	}

	if pendingKey, ok := r.pendingStorageAckReportKey(app.Namespace, app.Name); ok {
		if pendingKey != reportKey {
			r.clearStorageAckPending(app.Namespace, app.Name)
		} else {
			if err := r.patchManagedStorageAck(ctx, app.Namespace, app.Name, reportKey); err != nil {
				log.Error(err, "retry managed storage delivery ack")
				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}
			return ctrl.Result{}, nil
		}
	}

	if r.Reporter.HasPendingEvent(eventKey) {
		return ctrl.Result{}, nil
	}
	if !r.Reporter.ReportManagedStorageReady(ref, ready, agent.EventExtras{
		Endpoints: agentEndpointsFromStatus(app.Status.Endpoints),
		ManagedStorage: &agent.ManagedStoragePayload{
			AccessKeyID:     ms.AccessKeyID,
			SecretAccessKey: ms.SecretAccessKey,
			BucketName:      ms.BucketName,
		},
	}, reportKey) {
		return ctrl.Result{}, nil
	}
	r.markStorageInFlight(app.Namespace, app.Name, reportKey)
	return ctrl.Result{}, nil
}

// AckManagedStorageDelivered patches the dedup annotation after PostEvents succeeds.
func (r *ApplicationInstanceReconciler) AckManagedStorageDelivered(ctx context.Context, events []agent.Event) {
	for _, event := range events {
		reportKey, ok := agent.ManagedStorageReportKeyFromEventKey(event.EventKey)
		if !ok {
			continue
		}
		ref := event.ResourceRef
		r.clearStorageInFlight(ref.Namespace, ref.Name)
		if err := r.patchManagedStorageAck(ctx, ref.Namespace, ref.Name, reportKey); err != nil {
			r.markStorageAckPending(ref.Namespace, ref.Name, reportKey)
			logf.FromContext(ctx).Error(err, "ack managed storage delivery",
				"namespace", ref.Namespace, "name", ref.Name)
		}
	}
}

func (r *ApplicationInstanceReconciler) patchManagedStorageAck(ctx context.Context, namespace, name, reportKey string) error {
	app := &appsv1alpha1.ApplicationInstance{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, app); err != nil {
		return fmt.Errorf("get ApplicationInstance: %w", err)
	}
	if app.Annotations[reportedManagedStorageAccessKeyAnnotation] == reportKey {
		r.clearStorageDeliveryState(namespace, name)
		return nil
	}
	patchBase := app.DeepCopy()
	if app.Annotations == nil {
		app.Annotations = map[string]string{}
	}
	app.Annotations[reportedManagedStorageAccessKeyAnnotation] = reportKey
	var patchErr error
	for attempt := range 3 {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
		}
		if err := r.Patch(ctx, app, client.MergeFrom(patchBase)); err != nil {
			patchErr = err
			continue
		}
		r.clearStorageDeliveryState(namespace, name)
		return nil
	}
	return fmt.Errorf("patch ApplicationInstance: %w", patchErr)
}

func storageDeliveryKey(namespace, name string) string {
	return namespace + "/" + name
}

func (r *ApplicationInstanceReconciler) markStorageInFlight(namespace, name, reportKey string) {
	r.storageDeliveryMu.Lock()
	defer r.storageDeliveryMu.Unlock()
	if r.storageInFlight == nil {
		r.storageInFlight = make(map[string]string)
	}
	r.storageInFlight[storageDeliveryKey(namespace, name)] = reportKey
}

func (r *ApplicationInstanceReconciler) inFlightStorageReportKey(namespace, name string) (string, bool) {
	r.storageDeliveryMu.Lock()
	defer r.storageDeliveryMu.Unlock()
	reportKey, ok := r.storageInFlight[storageDeliveryKey(namespace, name)]
	return reportKey, ok
}

func (r *ApplicationInstanceReconciler) clearStorageInFlight(namespace, name string) {
	r.storageDeliveryMu.Lock()
	defer r.storageDeliveryMu.Unlock()
	delete(r.storageInFlight, storageDeliveryKey(namespace, name))
}

func (r *ApplicationInstanceReconciler) markStorageAckPending(namespace, name, reportKey string) {
	r.storageDeliveryMu.Lock()
	defer r.storageDeliveryMu.Unlock()
	if r.storageAckPending == nil {
		r.storageAckPending = make(map[string]string)
	}
	r.storageAckPending[storageDeliveryKey(namespace, name)] = reportKey
}

func (r *ApplicationInstanceReconciler) pendingStorageAckReportKey(namespace, name string) (string, bool) {
	r.storageDeliveryMu.Lock()
	defer r.storageDeliveryMu.Unlock()
	reportKey, ok := r.storageAckPending[storageDeliveryKey(namespace, name)]
	return reportKey, ok
}

func (r *ApplicationInstanceReconciler) clearStorageAckPending(namespace, name string) {
	r.storageDeliveryMu.Lock()
	defer r.storageDeliveryMu.Unlock()
	delete(r.storageAckPending, storageDeliveryKey(namespace, name))
}

func (r *ApplicationInstanceReconciler) clearStorageDeliveryState(namespace, name string) {
	r.storageDeliveryMu.Lock()
	defer r.storageDeliveryMu.Unlock()
	delete(r.storageInFlight, storageDeliveryKey(namespace, name))
	delete(r.storageAckPending, storageDeliveryKey(namespace, name))
}

func agentEndpointsFromStatus(endpoints []appsv1alpha1.EndpointStatus) []agent.EndpointPayload {
	if len(endpoints) == 0 {
		return nil
	}
	out := make([]agent.EndpointPayload, 0, len(endpoints))
	for _, ep := range endpoints {
		if ep.URL == "" {
			continue
		}
		out = append(out, agent.EndpointPayload{Name: ep.Name, URL: ep.URL})
	}
	return out
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
		if snapshot.HasS3 && snapshot.S3Endpoint == "" {
			app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReady, metav1.ConditionUnknown, "WaitingForS3Endpoint", "Seaweed is ready but S3 endpoint is not yet available")
		} else {
			app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionReady, metav1.ConditionTrue, "SeaweedReady", snapshot.Message)
			app.Status.Conditions = conditions.Set(app.Status.Conditions, appsv1alpha1.ConditionBlocked, metav1.ConditionFalse, "Unblocked", "Reconciliation can proceed")
		}
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
		Watches(
			seaweedengine.S3CredentialsObject(),
			handler.EnqueueRequestsFromMapFunc(r.mapS3CredentialsToApplicationInstance),
		).
		Named("applicationinstance").
		Complete(r)
}

func (r *ApplicationInstanceReconciler) mapS3CredentialsToApplicationInstance(ctx context.Context, obj client.Object) []reconcile.Request {
	cred, ok := obj.(*unstructured.Unstructured)
	if !ok || cred.GetKind() != "S3Credentials" {
		return nil
	}
	releaseName, found, _ := unstructured.NestedString(cred.Object, "spec", "seaweedRef", "name")
	if !found || releaseName == "" {
		return nil
	}

	list := &appsv1alpha1.ApplicationInstanceList{}
	if err := r.List(ctx, list, client.InNamespace(cred.GetNamespace())); err != nil {
		logf.FromContext(ctx).Error(err, "list ApplicationInstances for S3Credentials watch")
		return nil
	}

	// release.namespace must match metadata.namespace (admission webhook), so a
	// namespace-local list covers every ApplicationInstance that can reference
	// S3Credentials in cred.GetNamespace().
	return r.applicationInstancesForSeaweedRef(list.Items, releaseName, cred.GetNamespace())
}

func (r *ApplicationInstanceReconciler) applicationInstancesForSeaweedRef(items []appsv1alpha1.ApplicationInstance, releaseName, releaseNamespace string) []reconcile.Request {
	var out []reconcile.Request
	for i := range items {
		app := &items[i]
		if !seaweedengine.IsSeaweedWorkload(app) {
			continue
		}
		if app.Spec.Release == nil || app.Spec.Release.Name != releaseName {
			continue
		}
		ns := app.Spec.Release.Namespace
		if ns == "" {
			ns = app.Namespace
		}
		if ns != releaseNamespace {
			continue
		}
		out = append(out, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(app)})
	}
	return out
}

func managedStorageReportKey(ms *seaweedengine.ManagedStorageSnapshot) string {
	sum := sha256.Sum256([]byte(ms.AccessKeyID + "\x00" + ms.SecretAccessKey))
	return hex.EncodeToString(sum[:8])
}
