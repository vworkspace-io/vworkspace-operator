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

// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

package main

import (
	"crypto/tls"
	"flag"
	"os"
	"strings"
	"time"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
	"github.com/vworkspace-io/vworkspace-operator/internal/cli"
	"github.com/vworkspace-io/vworkspace-operator/internal/controller"
	"github.com/vworkspace-io/vworkspace-operator/internal/engines"
	"github.com/vworkspace-io/vworkspace-operator/internal/helmengine"
	"github.com/vworkspace-io/vworkspace-operator/internal/seaweedengine"
	"github.com/vworkspace-io/vworkspace-operator/internal/webhook"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	webhookserver "sigs.k8s.io/controller-runtime/pkg/webhook"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(appsv1alpha1.AddToScheme(scheme))
	utilruntime.Must(opsv1alpha1.AddToScheme(scheme))
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "register" {
		if err := cli.RunRegister(os.Args[2:]); err != nil {
			_, _ = os.Stderr.WriteString(err.Error() + "\n")
			os.Exit(1)
		}
		return
	}

	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var agentPollInterval time.Duration
	var agentCredentialsSecret string
	var agentCredentialsNamespace string
	var enableWebhooks bool
	var approvalClaimSecret string
	var veleroNamespace string
	var tlsOpts []func(*tls.Config)

	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true, "If set, the metrics endpoint is served securely via HTTPS.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false, "If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.DurationVar(&agentPollInterval, "agent-poll-interval", 30*time.Second,
		"Long-poll wait duration when fetching jobs from the control plane.")
	flag.StringVar(&agentCredentialsSecret, "agent-credentials-secret", agent.DefaultCredentialsSecret,
		"Default Kubernetes Secret name for bootstrap credentials when status.credentialsSecretRef is unset.")
	flag.StringVar(&agentCredentialsNamespace, "agent-credentials-namespace", os.Getenv("POD_NAMESPACE"),
		"Namespace of the agent credentials Secret.")
	flag.BoolVar(&enableWebhooks, "webhooks-enabled", false, "Enable validating admission webhooks.")
	flag.StringVar(&approvalClaimSecret, "approval-claim-secret", os.Getenv("VWORKSPACE_APPROVAL_CLAIM_SECRET"),
		"HMAC secret for verifying Operation.spec.approvals.claim (vws1 tokens from the control plane).")
	defaultVeleroNS := engines.DefaultVeleroNamespace
	if v := strings.TrimSpace(os.Getenv("VELERO_NAMESPACE")); v != "" {
		defaultVeleroNS = v
	}
	flag.StringVar(&veleroNamespace, "velero-namespace", defaultVeleroNS,
		"Namespace where Velero Backup and Restore CRs are created (must match Velero server install namespace).")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling HTTP/2")
		c.NextProtos = []string{"http/1.1"}
	}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhookserver.NewServer(webhookserver.Options{
		TLSOpts:  tlsOpts,
		CertDir:  webhookCertPath,
		CertName: webhookCertName,
		KeyName:  webhookCertKey,
	})

	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}
	if secureMetrics {
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}
	if len(metricsCertPath) > 0 {
		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "dad03b25.vworkspace.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	fluxEngine := helmengine.NewFluxEngine(mgr.GetClient())
	seaweedEngine := seaweedengine.NewSeaweedEngine(mgr.GetClient())

	credNamespace := strings.TrimSpace(agentCredentialsNamespace)
	if credNamespace == "" {
		credNamespace = "vworkspace-system"
	}

	// Pull-mode agent is always constructed; it idles until a Cluster CR and
	// bootstrap credentials Secret exist (cluster-bootstrap v2 agent unification).
	eventBatcher := agent.NewEventBatcher(nil)
	eventBatcher.Log = ctrl.Log.WithName("agent-events")
	statusReporter := agent.NewStatusReporter(eventBatcher)

	if err := (&controller.ApplicationInstanceReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		Engine:        fluxEngine,
		SeaweedEngine: seaweedEngine,
		Reporter:      statusReporter,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ApplicationInstance")
		os.Exit(1)
	}

	engineRegistry := engines.NewRegistry(
		engines.NewHelmEngine(mgr.GetClient()),
		engines.NewVeleroEngineWithNamespace(mgr.GetClient(), veleroNamespace),
		engines.NewJobEngine(mgr.GetClient()),
		engines.NewWorkflowEngine(mgr.GetClient()),
		engines.NewHelmHookJobEngine(mgr.GetClient()),
	)
	if err := (&controller.OperationReconciler{
		Client:              mgr.GetClient(),
		Scheme:              mgr.GetScheme(),
		Registry:            engineRegistry,
		Reporter:            statusReporter,
		ApprovalClaimSecret: strings.TrimSpace(approvalClaimSecret),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Operation")
		os.Exit(1)
	}

	if err := (&controller.ClusterReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		CredentialsSecret: agentCredentialsSecret,
		OperatorNamespace: credNamespace,
		Reporter:          statusReporter,
		EventBatcher:      eventBatcher,
		// ClusterReconciler.Recorder is a record.EventRecorder (old events API); keep using
		// GetEventRecorderFor until the reconciler migrates to the new events.EventRecorder API.
		Recorder: mgr.GetEventRecorderFor("cluster-controller"), //nolint:staticcheck // SA1019: old events API still in use
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Cluster")
		os.Exit(1)
	}

	if enableWebhooks {
		opWebhook, err := webhook.NewOperationWebhook(mgr.GetScheme(), mgr.GetClient(), webhook.OperationWebhookOptions{
			ApprovalClaimSecret: strings.TrimSpace(approvalClaimSecret),
		})
		if err != nil {
			setupLog.Error(err, "unable to create operation webhook")
			os.Exit(1)
		}
		if err := opWebhook.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to register operation webhook")
			os.Exit(1)
		}
		appWebhook, err := webhook.NewApplicationInstanceWebhook(mgr.GetScheme())
		if err != nil {
			setupLog.Error(err, "unable to create applicationinstance webhook")
			os.Exit(1)
		}
		if err := appWebhook.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to register applicationinstance webhook")
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()

	idempotencyStore := &agent.IdempotencyStore{
		Client:    mgr.GetClient(),
		Namespace: credNamespace,
		Name:      agent.DefaultIdempotencyConfigMap,
	}

	go eventBatcher.Start(ctx)
	agentRuntime := agent.NewRuntime(agent.RuntimeConfig{
		K8s:             mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		SecretNamespace: credNamespace,
		SecretName:      agentCredentialsSecret,
		PollInterval:    agentPollInterval,
		Idempotency:     idempotencyStore,
		EventBatcher:    eventBatcher,
		Log:             ctrl.Log.WithName("agent-runtime"),
	})
	go agentRuntime.Start(ctx)

	setupLog.Info("starting manager", "pull_mode", "credential-driven")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
