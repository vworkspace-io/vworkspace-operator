package seaweedengine

import (
	"context"
	"testing"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolveManagedStorageFromS3Credentials(t *testing.T) {
	t.Parallel()

	ns := "seaweedfs"
	release := "seaweedfs-smoke"
	cred := &unstructured.Unstructured{}
	cred.SetGroupVersionKind(s3CredentialsGVK)
	cred.SetName("smoke-creds")
	cred.SetNamespace(ns)
	_ = unstructured.SetNestedField(cred.Object, release, "spec", "seaweedRef", "name")
	_ = unstructured.SetNestedField(cred.Object, "smoke-admin", "status", "accessKey")
	_ = unstructured.SetNestedField(cred.Object, "Ready", "status", "phase")
	_ = unstructured.SetNestedField(cred.Object, "smoke-s3-secret", "status", "secretName")
	_ = unstructured.SetNestedField(cred.Object, "smoke-s3-secret", "spec", "secretRef", "name")

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "smoke-s3-secret", Namespace: ns},
		Data: map[string][]byte{
			"accessKey": []byte("smoke-admin"),
			"secretKey": []byte("smoke-secret"),
		},
	}

	app := sampleSeaweedApp()
	app.Spec.Release.Name = release
	app.SetNamespace(ns)
	app.Spec.Release.Namespace = ns

	engine := NewSeaweedEngine(fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(cred, secret).Build())
	got, err := engine.ResolveManagedStorage(context.Background(), app)
	if err != nil {
		t.Fatalf("ResolveManagedStorage: %v", err)
	}
	if got == nil {
		t.Fatal("expected managed storage snapshot")
	}
	if got.AccessKeyID != "smoke-admin" || got.SecretAccessKey != "smoke-secret" {
		t.Fatalf("unexpected credentials: %+v", got)
	}
	if got.BucketName != release {
		t.Fatalf("expected bucket %q, got %q", release, got.BucketName)
	}
}

func TestResolveManagedStorageSelectsDeterministicCredential(t *testing.T) {
	t.Parallel()

	ns := "seaweedfs"
	release := "seaweedfs-smoke"

	makeCred := func(name, accessKey string) *unstructured.Unstructured {
		cred := &unstructured.Unstructured{}
		cred.SetGroupVersionKind(s3CredentialsGVK)
		cred.SetName(name)
		cred.SetNamespace(ns)
		_ = unstructured.SetNestedField(cred.Object, release, "spec", "seaweedRef", "name")
		_ = unstructured.SetNestedField(cred.Object, accessKey, "status", "accessKey")
		_ = unstructured.SetNestedField(cred.Object, "Ready", "status", "phase")
		_ = unstructured.SetNestedField(cred.Object, name+"-secret", "status", "secretName")
		return cred
	}

	secretFor := func(name, accessKey string) *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-secret", Namespace: ns},
			Data: map[string][]byte{
				"accessKey": []byte(accessKey),
				"secretKey": []byte(name + "-secret-key"),
			},
		}
	}

	app := sampleSeaweedApp()
	app.Spec.Release.Name = release
	app.SetNamespace(ns)
	app.Spec.Release.Namespace = ns

	engine := NewSeaweedEngine(fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(
		makeCred("z-creds", "z-admin"),
		makeCred("a-creds", "a-admin"),
		secretFor("z-creds", "z-admin"),
		secretFor("a-creds", "a-admin"),
	).Build())
	got, err := engine.ResolveManagedStorage(context.Background(), app)
	if err != nil {
		t.Fatalf("ResolveManagedStorage: %v", err)
	}
	if got == nil || got.AccessKeyID != "a-admin" {
		t.Fatalf("expected deterministic a-creds selection, got %+v", got)
	}
}

func TestResolveManagedStorageStatePendingWhenCredentialsNotReady(t *testing.T) {
	t.Parallel()

	ns := "seaweedfs"
	release := "seaweedfs-smoke"
	cred := &unstructured.Unstructured{}
	cred.SetGroupVersionKind(s3CredentialsGVK)
	cred.SetName("pending-creds")
	cred.SetNamespace(ns)
	_ = unstructured.SetNestedField(cred.Object, release, "spec", "seaweedRef", "name")
	_ = unstructured.SetNestedField(cred.Object, "Pending", "status", "phase")

	app := sampleSeaweedApp()
	app.Spec.Release.Name = release
	app.SetNamespace(ns)
	app.Spec.Release.Namespace = ns

	engine := NewSeaweedEngine(fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(cred).Build())
	got, pending, err := engine.ResolveManagedStorageState(context.Background(), app)
	if err != nil {
		t.Fatalf("ResolveManagedStorageState: %v", err)
	}
	if got != nil || !pending {
		t.Fatalf("expected pending state, got snapshot=%+v pending=%v", got, pending)
	}
}

func TestResolveManagedStorageIgnoresOtherSeaweedRefs(t *testing.T) {
	t.Parallel()

	cred := &unstructured.Unstructured{}
	cred.SetGroupVersionKind(s3CredentialsGVK)
	cred.SetName("other-creds")
	cred.SetNamespace("seaweedfs")
	_ = unstructured.SetNestedField(cred.Object, "other-cluster", "spec", "seaweedRef", "name")
	_ = unstructured.SetNestedField(cred.Object, "Ready", "status", "phase")

	app := sampleSeaweedApp()
	engine := NewSeaweedEngine(fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(cred).Build())
	got, err := engine.ResolveManagedStorage(context.Background(), app)
	if err != nil {
		t.Fatalf("ResolveManagedStorage: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil snapshot, got %+v", got)
	}
}

var _ = appsv1alpha1.ApplicationInstance{}
