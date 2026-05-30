package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DefaultCredentialsSecret is the Secret name used when --agent-credentials-secret is set without a namespace.
	DefaultCredentialsSecret = "vworkspace-agent-credentials"

	// SecretKeyControlPlaneBaseURL is the Secret data key for the control plane HTTPS base URL.
	SecretKeyControlPlaneBaseURL = "control-plane-base-url"
	SecretKeyClusterID           = "cluster-id"
	SecretKeyToken               = "token"
)

// Credentials holds Pull-mode connectivity material.
type Credentials struct {
	BaseURL   string
	ClusterID string
	Token     string
}

// CredentialsConfig supplies flag/env overrides and optional Secret lookup.
type CredentialsConfig struct {
	BaseURL   string
	ClusterID string
	Token     string

	SecretNamespace string
	SecretName      string

	// K8s reads credentials; use a direct API reader before the manager cache starts.
	K8s client.Reader
}

// Load resolves credentials: explicit flag/env values win; missing fields are filled from the Secret.
func (c CredentialsConfig) Load(ctx context.Context) (Credentials, error) {
	cred := Credentials{
		BaseURL:   strings.TrimSpace(c.BaseURL),
		ClusterID: strings.TrimSpace(c.ClusterID),
		Token:     strings.TrimSpace(c.Token),
	}

	if c.SecretName != "" && c.K8s != nil {
		ns := c.SecretNamespace
		if ns == "" {
			ns = os.Getenv("POD_NAMESPACE")
		}
		if ns == "" {
			return Credentials{}, fmt.Errorf("agent credentials secret %q requires a namespace (set --agent-credentials-namespace or POD_NAMESPACE)", c.SecretName)
		}
		secret := &corev1.Secret{}
		if err := c.K8s.Get(ctx, client.ObjectKey{Namespace: ns, Name: c.SecretName}, secret); err != nil {
			return Credentials{}, fmt.Errorf("get agent credentials secret %s/%s: %w", ns, c.SecretName, err)
		}
		if cred.BaseURL == "" {
			cred.BaseURL = baseURLFromSecretData(secret.Data)
		}
		if cred.ClusterID == "" {
			cred.ClusterID = string(secret.Data[SecretKeyClusterID])
		}
		if cred.Token == "" {
			cred.Token = string(secret.Data[SecretKeyToken])
		}
	}

	cred.BaseURL = strings.TrimSpace(cred.BaseURL)
	cred.ClusterID = strings.TrimSpace(cred.ClusterID)
	cred.Token = strings.TrimSpace(cred.Token)

	if cred.BaseURL == "" || cred.ClusterID == "" || cred.Token == "" {
		return Credentials{}, fmt.Errorf("incomplete agent credentials: control plane base URL, cluster ID, and token are required")
	}
	return cred, nil
}

func baseURLFromSecretData(data map[string][]byte) string {
	if data == nil {
		return ""
	}
	return strings.TrimSpace(string(data[SecretKeyControlPlaneBaseURL]))
}
