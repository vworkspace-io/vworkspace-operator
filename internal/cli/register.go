package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RunRegister applies a Cluster CR carrying a one-time registration token.
func RunRegister(args []string) error {
	fs := flag.NewFlagSet("register", flag.ContinueOnError)
	token := fs.String("token", "", "One-time registration token from the control plane (required)")
	controlPlaneURL := fs.String("control-plane-endpoint", "", "Control plane HTTPS base URL (required unless CONTROL_PLANE_BASE_URL is set)")
	clusterName := fs.String("cluster-name", "", "Cluster resource name (defaults to cluster ID from the control plane after registration)")
	namespace := fs.String("namespace", envOr("POD_NAMESPACE", "vworkspace-system"), "Namespace for the Cluster CR")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(*token) == "" {
		return fmt.Errorf("--token is required")
	}
	if strings.TrimSpace(*controlPlaneURL) == "" {
		*controlPlaneURL = os.Getenv("CONTROL_PLANE_BASE_URL")
	}
	if strings.TrimSpace(*controlPlaneURL) == "" {
		return fmt.Errorf("--control-plane-endpoint or CONTROL_PLANE_BASE_URL is required")
	}
	if strings.TrimSpace(*clusterName) == "" {
		*clusterName = "cluster-local"
	}

	cfg, err := restConfig()
	if err != nil {
		return err
	}
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(opsv1alpha1.AddToScheme(scheme))
	k8s, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("create kubernetes client: %w", err)
	}

	cluster := &opsv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      *clusterName,
			Namespace: *namespace,
		},
		Spec: opsv1alpha1.ClusterSpec{
			ClusterID:           strings.TrimSpace(*clusterName),
			ControlPlaneBaseURL: strings.TrimSpace(*controlPlaneURL),
			RegistrationToken:   strings.TrimSpace(*token),
		},
	}

	fmt.Printf("registering cluster %s with %s ...\n", cluster.Name, cluster.Spec.ControlPlaneBaseURL)
	if err := k8s.Patch(context.Background(), cluster, client.Apply, client.FieldOwner("vworkspace-cli"), client.ForceOwnership); err != nil { //nolint:staticcheck // SSA via Patch is the supported path until ApplyConfiguration is generated.
		return fmt.Errorf("apply Cluster CR: %w", err)
	}

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		latest := &opsv1alpha1.Cluster{}
		if err := k8s.Get(context.Background(), client.ObjectKeyFromObject(cluster), latest); err != nil {
			return fmt.Errorf("get Cluster CR: %w", err)
		}
		if latest.Status.CredentialStatus != nil && latest.Status.CredentialStatus.RegistrationTokenConsumed {
			fmt.Printf("exchanged registration token for bootstrap credential\n")
			fmt.Printf("credential persisted to Secret %s/%s\n",
				latest.Status.CredentialStatus.SecretNamespace,
				latest.Status.CredentialStatus.SecretName)
			if cond, ok := findCondition(latest.Status.Conditions, opsv1alpha1.ConditionConnected); ok && cond.Status == metav1.ConditionTrue {
				fmt.Printf("Cluster %s condition Connected=True (%s)\n", latest.Name, cond.Reason)
			}
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out waiting for registration to complete")
}

func findCondition(conditions []metav1.Condition, t string) (metav1.Condition, bool) {
	for _, c := range conditions {
		if c.Type == t {
			return c, true
		}
	}
	return metav1.Condition{}, false
}

func restConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	return kubeConfig.ClientConfig()
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// Ensure agent package is linked for credential constant reference in docs.
var _ = agent.DefaultCredentialsSecret
