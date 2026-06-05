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
	"os"
	"strings"
	"time"

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
	"github.com/vworkspace-io/vworkspace-operator/internal/conditions"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
	// Recorder emits deprecation/audit Events; optional (nil-safe).
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=ops.vworkspace.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ops.vworkspace.io,resources=clusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ops.vworkspace.io,resources=clusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch

func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("cluster", req.Name)

	cluster := &opsv1alpha1.Cluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	secretName := r.credentialsSecretName()
	namespace := r.operatorNamespace()

	if !cluster.DeletionTimestamp.IsZero() {
		return r.finalizeCluster(ctx, cluster)
	}
	if !controllerutil.ContainsFinalizer(cluster, opsv1alpha1.ClusterFinalizer) {
		controllerutil.AddFinalizer(cluster, opsv1alpha1.ClusterFinalizer)
		if err := r.Update(ctx, cluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
	}

	prevConditions := append([]metav1.Condition(nil), cluster.Status.Conditions...)

	token, tokenSource, err := r.resolveRegistrationToken(ctx, cluster, namespace)
	if err != nil {
		log.Error(err, "resolve registration token")
		cluster.Status.Phase = opsv1alpha1.ClusterPhasePending
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionAuthenticated, metav1.ConditionFalse, "TokenSecretMissing", err.Error())
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionConnected, metav1.ConditionFalse, "RegistrationPending", "registration token is not yet resolvable")
		r.syncBufferOverflowCondition(cluster)
		if statusErr := r.Status().Update(ctx, cluster); statusErr != nil {
			return ctrl.Result{}, fmt.Errorf("update pending status: %w", statusErr)
		}
		r.reportConditions(cluster, prevConditions)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	if token != "" && tokenFingerprint(token) != cluster.Status.ObservedToken {
		if tokenSource == tokenSourceInline && r.Recorder != nil {
			r.Recorder.Event(cluster, corev1.EventTypeWarning, "DeprecatedField",
				"spec.registrationToken is deprecated; store the token in a Secret and use spec.registrationTokenSecretRef")
		}
		if err := r.registerCluster(ctx, cluster, token, tokenSource, secretName, namespace); err != nil {
			log.Error(err, "cluster registration failed")
			cluster.Status.Phase = opsv1alpha1.ClusterPhaseError
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
			cluster.Status.Phase = opsv1alpha1.ClusterPhaseError
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
		cluster.Status.Phase = opsv1alpha1.ClusterPhasePending
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
		cluster.Status.Phase = opsv1alpha1.ClusterPhaseDisconnected
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionConnected, metav1.ConditionFalse, "ControlPlaneUnreachable", err.Error())
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionDisconnected, metav1.ConditionTrue, "NoRecentRoundTrip", err.Error())
		cluster.Status.Conditions = conditions.Set(cluster.Status.Conditions, opsv1alpha1.ConditionAuthenticated, metav1.ConditionFalse, "ControlPlaneAuthenticationFailed", err.Error())
	} else {
		now := metav1.Now()
		cluster.Status.LastHeartbeat = &now
		cluster.Status.Phase = opsv1alpha1.ClusterPhaseConnected
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

func (r *ClusterReconciler) registerCluster(ctx context.Context, cluster *opsv1alpha1.Cluster, token, tokenSource, secretName, namespace string) error {
	baseURL := controlPlaneEndpoint(cluster)
	if baseURL == "" {
		return fmt.Errorf("spec.controlPlaneEndpoint is required for registration")
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
	specChanged := false
	if strings.TrimSpace(cluster.Spec.ClusterID) == "" {
		cluster.Spec.ClusterID = resp.ClusterID
		specChanged = true
	}
	// Only the deprecated inline token is cleared from the spec; the Secret-backed
	// token is never mutated by the operator.
	if tokenSource == tokenSourceInline && cluster.Spec.RegistrationToken != "" {
		cluster.Spec.RegistrationToken = ""
		specChanged = true
	}
	if specChanged {
		if err := r.Update(ctx, cluster); err != nil {
			return fmt.Errorf("persist post-registration spec: %w", err)
		}
	}

	fresh := &opsv1alpha1.Cluster{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), fresh); err != nil {
		return fmt.Errorf("reload cluster after registration: %w", err)
	}
	fresh.Status.Phase = opsv1alpha1.ClusterPhaseRegistering
	fresh.Status.ObservedToken = tokenFingerprint(token)
	fresh.Status.CredentialsSecretRef = &opsv1alpha1.SecretReference{Name: secretName, Namespace: namespace}
	fresh.Status.CredentialStatus = &opsv1alpha1.ClusterCredentialStatus{
		SecretName:                secretName,
		SecretNamespace:           namespace,
		RegisteredAt:              &now,
		RegistrationTokenConsumed: true,
	}
	fresh.Status.Conditions = conditions.Set(fresh.Status.Conditions, opsv1alpha1.ConditionAuthenticated, metav1.ConditionTrue, "RegistrationComplete", "registration token exchanged for bootstrap credential")
	fresh.Status.Conditions = conditions.Set(fresh.Status.Conditions, opsv1alpha1.ConditionConnected, metav1.ConditionTrue, "ControlPlaneReachable", "registration exchange succeeded")
	return r.Status().Update(ctx, fresh)
}

const (
	tokenSourceSecret = "secret"
	tokenSourceInline = "inline"
)

// resolveRegistrationToken returns the one-time registration token to exchange.
// A registrationTokenSecretRef (resolved only within the operator namespace) is
// preferred; the deprecated inline spec.registrationToken is the fallback. An empty
// token with a nil error means there is nothing to register (already registered or
// adopting an existing credentials Secret).
func (r *ClusterReconciler) resolveRegistrationToken(ctx context.Context, cluster *opsv1alpha1.Cluster, namespace string) (token, source string, err error) {
	if ref := cluster.Spec.RegistrationTokenSecretRef; ref != nil && strings.TrimSpace(ref.Name) != "" {
		key := strings.TrimSpace(ref.Key)
		if key == "" {
			key = opsv1alpha1.DefaultRegistrationTokenKey
		}
		secret := &corev1.Secret{}
		if getErr := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: strings.TrimSpace(ref.Name)}, secret); getErr != nil {
			if apierrors.IsNotFound(getErr) {
				return "", tokenSourceSecret, fmt.Errorf("registration token secret %s/%s not found", namespace, ref.Name)
			}
			return "", tokenSourceSecret, fmt.Errorf("get registration token secret %s/%s: %w", namespace, ref.Name, getErr)
		}
		tok := strings.TrimSpace(string(secret.Data[key]))
		if tok == "" {
			return "", tokenSourceSecret, fmt.Errorf("registration token secret %s/%s has no value for key %q", namespace, ref.Name, key)
		}
		return tok, tokenSourceSecret, nil
	}
	if tok := strings.TrimSpace(cluster.Spec.RegistrationToken); tok != "" {
		return tok, tokenSourceInline, nil
	}
	return "", "", nil
}

// finalizeCluster runs best-effort cleanup before the Cluster is removed. The
// credentials Secret is garbage-collected through its controller owner reference,
// so the finalizer only guarantees deterministic teardown ordering and removes
// itself. Server-side credential revoke is deferred (no revoke endpoint yet).
func (r *ClusterReconciler) finalizeCluster(ctx context.Context, cluster *opsv1alpha1.Cluster) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(cluster, opsv1alpha1.ClusterFinalizer) {
		return ctrl.Result{}, nil
	}
	logf.FromContext(ctx).Info("finalizing cluster; owned credentials Secret will be garbage-collected", "cluster", cluster.Name)
	agent.SetConnectivityState("pull", 0)
	controllerutil.RemoveFinalizer(cluster, opsv1alpha1.ClusterFinalizer)
	if err := r.Update(ctx, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// controlPlaneEndpoint returns the configured Pull-mode endpoint, preferring the
// v2 controlPlaneEndpoint field and falling back to the deprecated controlPlaneBaseUrl alias.
func controlPlaneEndpoint(cluster *opsv1alpha1.Cluster) string {
	if endpoint := strings.TrimSpace(cluster.Spec.ControlPlaneEndpoint); endpoint != "" {
		return endpoint
	}
	return strings.TrimSpace(cluster.Spec.ControlPlaneBaseURL)
}

// tokenFingerprint returns a short, non-reversible fingerprint of a registration
// token suitable for storing in status to detect rotation/re-registration.
func tokenFingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:16]
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
