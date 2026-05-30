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
	"os"
	"strings"
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
	Scheme             *runtime.Scheme
	AgentClient        agent.Client
	RegistrationClient agent.RegistrationClient
	CredentialsSecret  string
	OperatorNamespace  string
	Reporter           agent.StatusReporter
	EventBatcher       *agent.EventBatcher
}

// +kubebuilder:rbac:groups=ops.vworkspace.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ops.vworkspace.io,resources=clusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch

func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("cluster", req.Name)

	cluster := &opsv1alpha1.Cluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	prevConditions := append([]metav1.Condition(nil), cluster.Status.Conditions...)
	secretName := r.credentialsSecretName()
	namespace := r.operatorNamespace()

	if token := strings.TrimSpace(cluster.Spec.RegistrationToken); token != "" {
		if err := r.registerCluster(ctx, cluster, token, secretName, namespace); err != nil {
			log.Error(err, "cluster registration failed")
			cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionAuthenticated, metav1.ConditionFalse, "RegistrationFailed", err.Error())
			cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionConnected, metav1.ConditionFalse, "RegistrationPending", "registration token exchange failed")
			r.syncBufferOverflowCondition(cluster)
			if statusErr := r.Status().Update(ctx, cluster); statusErr != nil {
				return ctrl.Result{}, fmt.Errorf("update registration failure status: %w", statusErr)
			}
			r.reportConditions(cluster, prevConditions)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("reload cluster after registration: %w", err)
		}
		prevConditions = append([]metav1.Condition(nil), cluster.Status.Conditions...)
	}

	if cluster.Spec.RotateCredentials {
		if err := r.rotateCredentials(ctx, cluster, secretName, namespace); err != nil {
			log.Error(err, "credential rotation failed")
			cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionAuthenticated, metav1.ConditionFalse, "RotationFailed", err.Error())
			r.syncBufferOverflowCondition(cluster)
			if statusErr := r.Status().Update(ctx, cluster); statusErr != nil {
				return ctrl.Result{}, fmt.Errorf("update rotation failure status: %w", statusErr)
			}
			r.reportConditions(cluster, prevConditions)
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("reload cluster after rotation: %w", err)
		}
		prevConditions = append([]metav1.Condition(nil), cluster.Status.Conditions...)
	}

	clientForHeartbeat := r.AgentClient
	if clientForHeartbeat == nil {
		if creds, err := r.loadStoredCredentials(ctx, namespace, secretName); err == nil {
			clientForHeartbeat, err = agent.NewHTTPClient(agent.Config{
				BaseURL:   creds.BaseURL,
				ClusterID: creds.ClusterID,
				Token:     creds.Token,
			})
			if err != nil {
				log.Error(err, "configure agent client from stored credentials")
			}
		}
	}

	if clientForHeartbeat == nil {
		agent.SetConnectivityState("pull", -1)
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionConnected, metav1.ConditionFalse, "CredentialMissing", "bootstrap credential is not available")
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionAuthenticated, metav1.ConditionFalse, "CredentialMissing", "bootstrap credential is not available")
		r.syncBufferOverflowCondition(cluster)
		if err := r.Status().Update(ctx, cluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("update disconnected status: %w", err)
		}
		r.reportConditions(cluster, prevConditions)
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	if err := clientForHeartbeat.Heartbeat(ctx); err != nil {
		log.Error(err, "heartbeat failed")
		agent.SetConnectivityState("pull", -1)
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionConnected, metav1.ConditionFalse, "OdooUnreachable", err.Error())
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionDisconnected, metav1.ConditionTrue, "NoRecentRoundTrip", err.Error())
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionAuthenticated, metav1.ConditionFalse, "OdooAuthenticationFailed", err.Error())
	} else {
		now := metav1.Now()
		cluster.Status.LastHeartbeat = &now
		agent.SetConnectivityState("pull", 1)
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionConnected, metav1.ConditionTrue, "RoundTripOK", "Recent round-trip to the control plane succeeded")
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionDisconnected, metav1.ConditionFalse, "Connected", "Connection is alive")
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionAuthenticated, metav1.ConditionTrue, "CredentialAccepted", "Bearer token accepted")
	}

	r.syncBufferOverflowCondition(cluster)

	if err := r.Status().Update(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("update cluster status: %w", err)
	}
	r.reportConditions(cluster, prevConditions)
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *ClusterReconciler) syncBufferOverflowCondition(cluster *opsv1alpha1.Cluster) {
	if r.EventBatcher == nil {
		return
	}
	active, dropped := r.EventBatcher.OverflowState()
	if active && dropped > 0 {
		cluster.Status.Conditions = conditions.Set(
			cluster.Status.Conditions,
			opsv1alpha1.ConditionBufferOverflow,
			metav1.ConditionTrue,
			"EventBufferFull",
			agent.BufferOverflowMessage(dropped),
		)
		return
	}
	cluster.Status.Conditions = conditions.Set(
		cluster.Status.Conditions,
		opsv1alpha1.ConditionBufferOverflow,
		metav1.ConditionFalse,
		"BufferDrained",
		"Outbound event buffer has drained successfully",
	)
}

func (r *ClusterReconciler) rotateCredentials(ctx context.Context, cluster *opsv1alpha1.Cluster, secretName, namespace string) error {
	creds, err := r.loadStoredCredentials(ctx, namespace, secretName)
	if err != nil {
		return fmt.Errorf("load stored credentials: %w", err)
	}

	rotateClient := r.AgentClient
	if rotateClient == nil {
		rotateClient, err = agent.NewHTTPClient(agent.Config{
			BaseURL:   creds.BaseURL,
			ClusterID: creds.ClusterID,
			Token:     creds.Token,
		})
		if err != nil {
			return fmt.Errorf("configure rotate client: %w", err)
		}
	}

	resp, err := rotateClient.RotateCredentials(ctx)
	if err != nil {
		return err
	}

	newCreds := agent.Credentials{
		BaseURL:   creds.BaseURL,
		ClusterID: creds.ClusterID,
		Token:     resp.Token,
	}
	if err := agent.PersistCredentials(ctx, r.Client, namespace, secretName, newCreds, cluster); err != nil {
		return err
	}

	cluster.Spec.RotateCredentials = false
	if err := r.Update(ctx, cluster); err != nil {
		return fmt.Errorf("clear rotate credentials flag: %w", err)
	}

	ref := agent.ResourceRefFromMeta(opsv1alpha1.GroupVersion.WithKind("Cluster"), cluster.ObjectMeta)
	r.Reporter.ReportAudit(ref, "CredentialRotated", []metav1.Condition{{
		Type:    opsv1alpha1.ConditionAuthenticated,
		Status:  metav1.ConditionTrue,
		Reason:  "CredentialRotated",
		Message: "Bootstrap credential rotated successfully",
	}})
	return nil
}

func (r *ClusterReconciler) reportConditions(cluster *opsv1alpha1.Cluster, prev []metav1.Condition) {
	ref := agent.ResourceRefFromMeta(opsv1alpha1.GroupVersion.WithKind("Cluster"), cluster.ObjectMeta)
	r.Reporter.ReportConditionTransitions(ref, prev, cluster.Status.Conditions)
}

func (r *ClusterReconciler) registerCluster(ctx context.Context, cluster *opsv1alpha1.Cluster, token, secretName, namespace string) error {
	baseURL := strings.TrimSpace(cluster.Spec.OdooBaseURL)
	if baseURL == "" {
		return fmt.Errorf("spec.odooBaseUrl (control plane base URL) is required for registration")
	}

	regClient := r.RegistrationClient
	if regClient == nil {
		regClient = &agent.HTTPRegistrationClient{}
	}

	resp, err := regClient.Register(ctx, baseURL, token, cluster.Spec.ClusterID)
	if err != nil {
		return err
	}

	creds := agent.Credentials{
		BaseURL:   baseURL,
		ClusterID: resp.ClusterID,
		Token:     resp.Token,
	}
	if err := agent.PersistCredentials(ctx, r.Client, namespace, secretName, creds, cluster); err != nil {
		return err
	}

	now := metav1.Now()
	if strings.TrimSpace(cluster.Spec.ClusterID) == "" {
		cluster.Spec.ClusterID = resp.ClusterID
	}
	cluster.Spec.RegistrationToken = ""
	if err := r.Update(ctx, cluster); err != nil {
		return fmt.Errorf("clear registration token: %w", err)
	}

	fresh := &opsv1alpha1.Cluster{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), fresh); err != nil {
		return fmt.Errorf("reload cluster after registration: %w", err)
	}
	fresh.Status.CredentialStatus = &opsv1alpha1.ClusterCredentialStatus{
		SecretName:                secretName,
		SecretNamespace:           namespace,
		RegisteredAt:              &now,
		RegistrationTokenConsumed: true,
	}
	fresh.Status.Conditions = conditions.Set(fresh.Status.Conditions, opsv1alpha1.ConditionAuthenticated, metav1.ConditionTrue, "RegistrationComplete", "registration token exchanged for bootstrap credential")
	fresh.Status.Conditions = conditions.Set(fresh.Status.Conditions, opsv1alpha1.ConditionConnected, metav1.ConditionTrue, "OdooReachable", "registration exchange succeeded")
	return r.Status().Update(ctx, fresh)
}

func (r *ClusterReconciler) loadStoredCredentials(ctx context.Context, namespace, secretName string) (agent.Credentials, error) {
	secret, found, err := agent.GetCredentialsSecret(ctx, r.Client, namespace, secretName)
	if err != nil {
		return agent.Credentials{}, err
	}
	if !found {
		return agent.Credentials{}, fmt.Errorf("credentials secret not found")
	}
	return agent.CredentialsFromSecret(secret)
}

func (r *ClusterReconciler) credentialsSecretName() string {
	if strings.TrimSpace(r.CredentialsSecret) != "" {
		return r.CredentialsSecret
	}
	return agent.DefaultCredentialsSecret
}

func (r *ClusterReconciler) operatorNamespace() string {
	if strings.TrimSpace(r.OperatorNamespace) != "" {
		return r.OperatorNamespace
	}
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		return ns
	}
	return "vworkspace-system"
}

func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opsv1alpha1.Cluster{}).
		Named("cluster").
		Complete(r)
}
