// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch

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

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"os"
	"time"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
	"github.com/vworkspace-io/vworkspace-operator/internal/controller"
	"github.com/vworkspace-io/vworkspace-operator/internal/engines"
	"github.com/vworkspace-io/vworkspace-operator/internal/helmengine"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
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
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var clusterID string
	var odooBaseURL string
	var agentToken string
	var agentEnabled bool
	var agentPollInterval time.Duration
	var agentCredentialsSecret string
	var agentCredentialsNamespace string
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
	flag.StringVar(&clusterID, "cluster-id", os.Getenv("VWORKSPACE_CLUSTER_ID"),
		"Stable cluster identity registered with Odoo.")
	flag.StringVar(&odooBaseURL, "odoo-base-url", os.Getenv("ODOO_BASE_URL"), "Odoo base URL for Pull-mode connectivity.")
	flag.StringVar(&agentToken, "agent-token", os.Getenv("VWORKSPACE_AGENT_TOKEN"),
		"Bearer token for Pull-mode agent API.")
	flag.BoolVar(&agentEnabled, "agent-enabled", false,
		"Enable the Pull-mode agent loop (long-poll jobs from Odoo).")
	flag.DurationVar(&agentPollInterval, "agent-poll-interval", 30*time.Second,
		"Long-poll wait duration when fetching jobs from Odoo.")
	flag.StringVar(&agentCredentialsSecret, "agent-credentials-secret", "",
		"Kubernetes Secret name containing odoo-base-url, cluster-id, and token keys.")
	flag.StringVar(&agentCredentialsNamespace, "agent-credentials-namespace", os.Getenv("POD_NAMESPACE"),
		"Namespace of the agent credentials Secret.")

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

	webhookServer := webhook.NewServer(webhook.Options{
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
	if err := (&controller.ApplicationInstanceReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Engine: fluxEngine,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ApplicationInstance")
		os.Exit(1)
	}

	engineRegistry := engines.NewRegistry(
		engines.NewHelmEngine(mgr.GetClient()),
		engines.NewVeleroEngine(mgr.GetClient()),
	)
	if err := (&controller.OperationReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Registry: engineRegistry,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Operation")
		os.Exit(1)
	}

	var agentClient agent.Client
	if agentEnabled {
		creds, err := agent.CredentialsConfig{
			BaseURL:         odooBaseURL,
			ClusterID:       clusterID,
			Token:           agentToken,
			SecretNamespace: agentCredentialsNamespace,
			SecretName:      agentCredentialsSecret,
			K8s:             mgr.GetClient(),
		}.Load(context.Background())
		if err != nil {
			setupLog.Error(err, "unable to load agent credentials")
			os.Exit(1)
		}
		clusterID = creds.ClusterID
		agentClient, err = agent.NewHTTPClient(agent.Config{
			BaseURL:   creds.BaseURL,
			ClusterID: creds.ClusterID,
			Token:     creds.Token,
		})
		if err != nil {
			setupLog.Error(err, "unable to configure agent client")
			os.Exit(1)
		}
		setupLog.Info("pull-mode agent enabled", "cluster_id", creds.ClusterID)
	}

	if err := (&controller.ClusterReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		AgentClient: agentClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Cluster")
		os.Exit(1)
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

	if agentClient != nil {
		applier := &agent.Applier{
			Client:    mgr.GetClient(),
			Scheme:    mgr.GetScheme(),
			ClusterID: clusterID,
		}
		batcher := agent.NewEventBatcher(agentClient)
		batcher.Log = setupLog.WithName("agent-events")
		poller := &agent.AgentPoller{
			Client:   agentClient,
			Applier:  applier,
			Events:   batcher,
			Log:      ctrl.Log.WithName("agent-poller"),
			WaitSecs: int(agentPollInterval.Seconds()),
		}
		go batcher.Start(ctx)
		go poller.Run(ctx)
	}

	setupLog.Info("starting manager", "cluster_id", clusterID)
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
