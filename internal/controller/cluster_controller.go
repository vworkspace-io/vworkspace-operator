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

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
	"github.com/vworkspace-io/vworkspace-operator/internal/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// ClusterReconciler reports Pull-mode connectivity for a Cluster resource.
type ClusterReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	AgentClient agent.Client
}

// +kubebuilder:rbac:groups=ops.vworkspace.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ops.vworkspace.io,resources=clusters/status,verbs=get;update;patch

func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("cluster", req.Name)

	cluster := &opsv1alpha1.Cluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if r.AgentClient == nil {
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionConnected, metav1.ConditionFalse, "OdooUnreachable", "agent client is not configured")
		if err := r.Status().Update(ctx, cluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("update disconnected status: %w", err)
		}
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	if err := r.AgentClient.Heartbeat(ctx); err != nil {
		log.Error(err, "heartbeat failed")
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionConnected, metav1.ConditionFalse, "OdooUnreachable", err.Error())
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionDisconnected, metav1.ConditionTrue, "NoRecentRoundTrip", err.Error())
	} else {
		now := metav1.Now()
		cluster.Status.LastHeartbeat = &now
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionConnected, metav1.ConditionTrue, "RoundTripOK", "Recent round-trip to Odoo succeeded")
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionDisconnected, metav1.ConditionFalse, "Connected", "Connection is alive")
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionAuthenticated, metav1.ConditionTrue, "CredentialAccepted", "Bearer token accepted")
	}

	if err := r.Status().Update(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("update cluster status: %w", err)
	}
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opsv1alpha1.Cluster{}).
		Named("cluster").
		Complete(r)
}
