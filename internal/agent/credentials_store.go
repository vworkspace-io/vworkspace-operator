package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// PersistCredentials writes or updates the bootstrap credential Secret.
func PersistCredentials(ctx context.Context, c client.Client, namespace, secretName string, cred Credentials, owner client.Object) error {
	namespace = strings.TrimSpace(namespace)
	secretName = strings.TrimSpace(secretName)
	if namespace == "" {
		return fmt.Errorf("credentials secret namespace is required")
	}
	if secretName == "" {
		secretName = DefaultCredentialsSecret
	}
	if strings.TrimSpace(cred.BaseURL) == "" || strings.TrimSpace(cred.ClusterID) == "" || strings.TrimSpace(cred.Token) == "" {
		return fmt.Errorf("incomplete credentials for persistence")
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, c, secret, func() error {
		if secret.Data == nil {
			secret.Data = map[string][]byte{}
		}
		secret.Data[SecretKeyControlPlaneBaseURL] = []byte(strings.TrimSpace(cred.BaseURL))
		secret.Data[SecretKeyOdooBaseURL] = []byte(strings.TrimSpace(cred.BaseURL))
		secret.Data[SecretKeyClusterID] = []byte(strings.TrimSpace(cred.ClusterID))
		secret.Data[SecretKeyToken] = []byte(strings.TrimSpace(cred.Token))
		secret.Type = corev1.SecretTypeOpaque
		if owner != nil {
			if err := controllerutil.SetControllerReference(owner, secret, c.Scheme()); err != nil {
				return fmt.Errorf("set owner reference: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("persist credentials secret %s/%s: %w", namespace, secretName, err)
	}
	SetCredentialUpdatedAt(time.Now().UTC())
	return nil
}

// UpdateCredentialAgeFromSecret sets the credential age metric from Secret metadata.
func UpdateCredentialAgeFromSecret(secret *corev1.Secret) {
	if secret == nil {
		return
	}
	t := secret.CreationTimestamp.Time
	if t.IsZero() {
		t = time.Now().UTC()
	}
	SetCredentialUpdatedAt(t)
}

// CredentialsFromSecret reads bootstrap credentials from an existing Secret.
func CredentialsFromSecret(secret *corev1.Secret) (Credentials, error) {
	if secret == nil {
		return Credentials{}, fmt.Errorf("secret is nil")
	}
	cred := Credentials{
		BaseURL:   baseURLFromSecretData(secret.Data),
		ClusterID: strings.TrimSpace(string(secret.Data[SecretKeyClusterID])),
		Token:     strings.TrimSpace(string(secret.Data[SecretKeyToken])),
	}
	if cred.BaseURL == "" || cred.ClusterID == "" || cred.Token == "" {
		return Credentials{}, fmt.Errorf("secret %s/%s is missing control-plane-base-url (or odoo-base-url), cluster-id, or token", secret.Namespace, secret.Name)
	}
	return cred, nil
}

// GetCredentialsSecret fetches the credentials Secret if it exists.
func GetCredentialsSecret(ctx context.Context, c client.Reader, namespace, secretName string) (*corev1.Secret, bool, error) {
	if secretName == "" {
		secretName = DefaultCredentialsSecret
	}
	secret := &corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, secret)
	if apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get credentials secret %s/%s: %w", namespace, secretName, err)
	}
	return secret, true, nil
}
