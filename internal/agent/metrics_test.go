package agent

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCredentialAgeSeconds(t *testing.T) {
	SetCredentialUpdatedAt(time.Now().UTC().Add(-2 * time.Minute))
	age := credentialAgeSeconds()
	if age < 110 || age > 130 {
		t.Fatalf("expected age around 120s, got %f", age)
	}

	SetCredentialUpdatedAt(time.Time{})
	if got := credentialAgeSeconds(); got != 0 {
		t.Fatalf("expected zero age when unset, got %f", got)
	}
}

func TestUpdateCredentialAgeFromSecret(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.NewTime(time.Now().UTC().Add(-5 * time.Minute)),
		},
	}
	UpdateCredentialAgeFromSecret(secret)
	age := credentialAgeSeconds()
	if age < 290 || age > 310 {
		t.Fatalf("expected age around 300s, got %f", age)
	}
}
