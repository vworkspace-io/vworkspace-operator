package seaweedengine

import (
	"context"
	"fmt"
	"sort"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var s3CredentialsGVK = schema.GroupVersionKind{
	Group: "seaweed.seaweedfs.com", Version: "v1", Kind: "S3Credentials",
}

// S3CredentialsObject returns an empty S3Credentials CR placeholder for controller watches.
func S3CredentialsObject() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(s3CredentialsGVK)
	return obj
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
	snapshot, _, err := e.ResolveManagedStorageState(ctx, app)
	return snapshot, err
}

// ResolveManagedStorageState resolves inline credentials when available. pending is true
// when matching S3Credentials CRs exist but are not yet Ready with usable keys.
func (e *SeaweedEngine) ResolveManagedStorageState(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (*ManagedStorageSnapshot, bool, error) {
	if err := validateReleaseRef(app); err != nil {
		return nil, false, err
	}
	ns := releaseNamespace(app)
	releaseName := app.Spec.Release.Name

	candidates, err := e.listMatchingS3Credentials(ctx, ns, releaseName)
	if err != nil {
		return nil, false, err
	}
	if len(candidates) == 0 {
		return nil, false, nil
	}

	for _, item := range candidates {
		snapshot, err := e.snapshotFromS3Credentials(ctx, ns, releaseName, item)
		if err != nil {
			return nil, true, err
		}
		if snapshot != nil {
			return snapshot, false, nil
		}
	}
	return nil, true, nil
}

func (e *SeaweedEngine) listMatchingS3Credentials(ctx context.Context, ns, releaseName string) ([]unstructured.Unstructured, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group: s3CredentialsGVK.Group, Version: s3CredentialsGVK.Version, Kind: s3CredentialsGVK.Kind + "List",
	})
	if err := e.Client.List(ctx, list, client.InNamespace(ns)); err != nil {
		return nil, fmt.Errorf("list S3Credentials: %w", err)
	}

	var candidates []unstructured.Unstructured
	for _, item := range list.Items {
		refName, _, _ := unstructured.NestedString(item.Object, "spec", "seaweedRef", "name")
		if refName != releaseName {
			continue
		}
		candidates = append(candidates, item)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].GetName() < candidates[j].GetName()
	})
	return candidates, nil
}

func (e *SeaweedEngine) snapshotFromS3Credentials(ctx context.Context, ns, releaseName string, item unstructured.Unstructured) (*ManagedStorageSnapshot, error) {
	phase, _, _ := unstructured.NestedString(item.Object, "status", "phase")
	if phase != "" && phase != "Ready" {
		return nil, nil
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
		return nil, nil
	}
	secret := &corev1.Secret{}
	if err := e.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: secretName}, secret); err != nil {
		return nil, fmt.Errorf("get S3Credentials secret %s/%s: %w", ns, secretName, err)
	}
	secretKey := string(secret.Data[secretKeyField])
	if accessKey == "" {
		accessKey = string(secret.Data[accessKeyField])
	}
	if accessKey == "" || secretKey == "" {
		return nil, nil
	}
	return &ManagedStorageSnapshot{
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
		BucketName:      releaseName,
	}, nil
}
