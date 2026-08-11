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

const (
	testSeaweedNamespace = CatalogIDSeaweedFS
	testSeaweedRelease   = "seaweedfs-smoke"
)

func TestResolveManagedStorageFromS3Credentials(t *testing.T) {
	t.Parallel()

	ns := testSeaweedNamespace
	release := testSeaweedRelease
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

func TestResolveManagedStorageStateQuietWhenAllCredentialsFailed(t *testing.T) {
	t.Parallel()

	ns := testSeaweedNamespace
	release := testSeaweedRelease
	cred := &unstructured.Unstructured{}
	cred.SetGroupVersionKind(s3CredentialsGVK)
	cred.SetName("failed-creds")
	cred.SetNamespace(ns)
	_ = unstructured.SetNestedField(cred.Object, release, "spec", "seaweedRef", "name")
	_ = unstructured.SetNestedField(cred.Object, "Failed", "status", "phase")

	app := sampleSeaweedApp()
	app.Spec.Release.Name = release
	app.SetNamespace(ns)
	app.Spec.Release.Namespace = ns

	engine := NewSeaweedEngine(fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(cred).Build())
	got, pending, failed, err := engine.ResolveManagedStorageState(context.Background(), app)
	if err != nil {
		t.Fatalf("ResolveManagedStorageState: %v", err)
	}
	if got != nil || pending || !failed {
		t.Fatalf("expected terminal Failed state, got snapshot=%+v pending=%v failed=%v", got, pending, failed)
	}
}

func TestResolveManagedStorageSelectsSmallestReadyCredential(t *testing.T) {
	t.Parallel()

	ns := testSeaweedNamespace
	release := testSeaweedRelease

	makeCred := func(name, phase string) *unstructured.Unstructured {
		cred := &unstructured.Unstructured{}
		cred.SetGroupVersionKind(s3CredentialsGVK)
		cred.SetName(name)
		cred.SetNamespace(ns)
		_ = unstructured.SetNestedField(cred.Object, release, "spec", "seaweedRef", "name")
		_ = unstructured.SetNestedField(cred.Object, name+"-admin", "status", "accessKey")
		_ = unstructured.SetNestedField(cred.Object, phase, "status", "phase")
		_ = unstructured.SetNestedField(cred.Object, name+"-secret", "status", "secretName")
		return cred
	}

	secretFor := func(name string) *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-secret", Namespace: ns},
			Data: map[string][]byte{
				"accessKey": []byte(name + "-admin"),
				"secretKey": []byte(name + "-secret-key"),
			},
		}
	}

	app := sampleSeaweedApp()
	app.Spec.Release.Name = release
	app.SetNamespace(ns)
	app.Spec.Release.Namespace = ns

	engine := NewSeaweedEngine(fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(
		makeCred("a-creds", "Pending"),
		makeCred("z-creds", "Ready"),
		secretFor("z-creds"),
	).Build())
	got, pending, failed, err := engine.ResolveManagedStorageState(context.Background(), app)
	if err != nil {
		t.Fatalf("ResolveManagedStorageState: %v", err)
	}
	if pending || failed || got == nil || got.AccessKeyID != "z-creds-admin" {
		t.Fatalf("expected smallest Ready credential z-creds, got snapshot=%+v pending=%v failed=%v", got, pending, failed)
	}
}

func TestResolveManagedStorageSelectsDeterministicCredential(t *testing.T) {
	t.Parallel()

	ns := testSeaweedNamespace
	release := testSeaweedRelease

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

	ns := testSeaweedNamespace
	release := testSeaweedRelease
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
	got, pending, failed, err := engine.ResolveManagedStorageState(context.Background(), app)
	if err != nil {
		t.Fatalf("ResolveManagedStorageState: %v", err)
	}
	if got != nil || !pending || failed {
		t.Fatalf("expected pending state, got snapshot=%+v pending=%v failed=%v", got, pending, failed)
	}
}

func TestResolveManagedStorageStateFailedWhenReadySecretUnusable(t *testing.T) {
	t.Parallel()

	ns := testSeaweedNamespace
	release := testSeaweedRelease
	cred := &unstructured.Unstructured{}
	cred.SetGroupVersionKind(s3CredentialsGVK)
	cred.SetName("broken-ready")
	cred.SetNamespace(ns)
	_ = unstructured.SetNestedField(cred.Object, release, "spec", "seaweedRef", "name")
	_ = unstructured.SetNestedField(cred.Object, "Ready", "status", "phase")
	_ = unstructured.SetNestedField(cred.Object, "missing-secret", "status", "secretName")

	app := sampleSeaweedApp()
	app.Spec.Release.Name = release
	app.SetNamespace(ns)
	app.Spec.Release.Namespace = ns

	engine := NewSeaweedEngine(fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(cred).Build())
	got, pending, failed, err := engine.ResolveManagedStorageState(context.Background(), app)
	if err != nil {
		t.Fatalf("ResolveManagedStorageState: %v", err)
	}
	if got != nil || pending || !failed {
		t.Fatalf("expected terminal failed when Ready secret is missing, got snapshot=%+v pending=%v failed=%v", got, pending, failed)
	}
}

func TestResolveManagedStorageStatePendingWhenReadyUnusableButOthersPending(t *testing.T) {
	t.Parallel()

	ns := testSeaweedNamespace
	release := testSeaweedRelease

	broken := &unstructured.Unstructured{}
	broken.SetGroupVersionKind(s3CredentialsGVK)
	broken.SetName("broken-ready")
	broken.SetNamespace(ns)
	_ = unstructured.SetNestedField(broken.Object, release, "spec", "seaweedRef", "name")
	_ = unstructured.SetNestedField(broken.Object, "Ready", "status", "phase")
	_ = unstructured.SetNestedField(broken.Object, "missing-secret", "status", "secretName")

	pending := &unstructured.Unstructured{}
	pending.SetGroupVersionKind(s3CredentialsGVK)
	pending.SetName("still-pending")
	pending.SetNamespace(ns)
	_ = unstructured.SetNestedField(pending.Object, release, "spec", "seaweedRef", "name")
	_ = unstructured.SetNestedField(pending.Object, "Pending", "status", "phase")

	app := sampleSeaweedApp()
	app.Spec.Release.Name = release
	app.SetNamespace(ns)
	app.Spec.Release.Namespace = ns

	engine := NewSeaweedEngine(fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(broken, pending).Build())
	got, pendingState, failed, err := engine.ResolveManagedStorageState(context.Background(), app)
	if err != nil {
		t.Fatalf("ResolveManagedStorageState: %v", err)
	}
	if got != nil || !pendingState || failed {
		t.Fatalf("expected pending while other credentials are still Pending, got snapshot=%+v pending=%v failed=%v", got, pendingState, failed)
	}
}

func TestResolveManagedStorageStatePendingWhenSecretMissing(t *testing.T) {
	t.Parallel()

	ns := testSeaweedNamespace
	release := testSeaweedRelease
	cred := &unstructured.Unstructured{}
	cred.SetGroupVersionKind(s3CredentialsGVK)
	cred.SetName("pending-secret")
	cred.SetNamespace(ns)
	_ = unstructured.SetNestedField(cred.Object, release, "spec", "seaweedRef", "name")
	_ = unstructured.SetNestedField(cred.Object, "Ready", "status", "phase")
	_ = unstructured.SetNestedField(cred.Object, "missing-secret", "status", "secretName")

	app := sampleSeaweedApp()
	app.Spec.Release.Name = release
	app.SetNamespace(ns)
	app.Spec.Release.Namespace = ns

	engine := NewSeaweedEngine(fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(cred).Build())
	got, pending, failed, err := engine.ResolveManagedStorageState(context.Background(), app)
	if err != nil {
		t.Fatalf("ResolveManagedStorageState: %v", err)
	}
	if got != nil || pending || !failed {
		t.Fatalf("expected terminal failed when only Ready credential has missing secret, got snapshot=%+v pending=%v failed=%v", got, pending, failed)
	}
}

func TestResolveManagedStorageIgnoresOtherSeaweedRefs(t *testing.T) {
	t.Parallel()

	cred := &unstructured.Unstructured{}
	cred.SetGroupVersionKind(s3CredentialsGVK)
	cred.SetName("other-creds")
	cred.SetNamespace(testSeaweedNamespace)
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
