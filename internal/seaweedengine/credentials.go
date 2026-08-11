package seaweedengine

import (
	"context"
	"fmt"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var s3CredentialsGVK = schema.GroupVersionKind{
	Group: "seaweed.seaweedfs.com", Version: "v1", Kind: "S3Credentials",
}

// ManagedStorageSnapshot carries inline S3 credentials for control-plane registry sync.
type ManagedStorageSnapshot struct {
	AccessKeyID     string
	SecretAccessKey string
	BucketName      string
}

// ResolveManagedStorage reads the first Ready S3Credentials CR for the Seaweed cluster
// and returns inline keys for agent event payloads (P10-T005 / P10-T006).
func (e *SeaweedEngine) ResolveManagedStorage(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (*ManagedStorageSnapshot, error) {
	if err := validateReleaseRef(app); err != nil {
		return nil, err
	}
	ns := releaseNamespace(app)
	releaseName := app.Spec.Release.Name

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group: s3CredentialsGVK.Group, Version: s3CredentialsGVK.Version, Kind: s3CredentialsGVK.Kind + "List",
	})
	if err := e.Client.List(ctx, list, client.InNamespace(ns)); err != nil {
		return nil, fmt.Errorf("list S3Credentials: %w", err)
	}

	for _, item := range list.Items {
		refName, _, _ := unstructured.NestedString(item.Object, "spec", "seaweedRef", "name")
		if refName != releaseName {
			continue
		}
		phase, _, _ := unstructured.NestedString(item.Object, "status", "phase")
		if phase != "" && phase != "Ready" {
			continue
		}
		accessKey, _, _ := unstructured.NestedString(item.Object, "status", "accessKey")
		secretName, _, _ := unstructured.NestedString(item.Object, "status", "secretName")
		specSecretName, _, _ := unstructured.NestedString(item.Object, "spec", "secretRef", "name")
		if secretName == "" {
			secretName = specSecretName
		}
		accessKeyField, _, _ := unstructured.NestedString(item.Object, "spec", "secretRef", "accessKeyField")
		if accessKeyField == "" {
			accessKeyField = "accessKey"
		}
		secretKeyField, _, _ := unstructured.NestedString(item.Object, "spec", "secretRef", "secretKeyField")
		if secretKeyField == "" {
			secretKeyField = "secretKey"
		}
		if secretName == "" {
			continue
		}
		secret := &corev1.Secret{}
		if err := e.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: secretName}, secret); err != nil {
			continue
		}
		secretKey := string(secret.Data[secretKeyField])
		if accessKey == "" {
			accessKey = string(secret.Data[accessKeyField])
		}
		if accessKey == "" || secretKey == "" {
			continue
		}
		return &ManagedStorageSnapshot{
			AccessKeyID:     accessKey,
			SecretAccessKey: secretKey,
			BucketName:      releaseName,
		}, nil
	}
	return nil, nil
}
