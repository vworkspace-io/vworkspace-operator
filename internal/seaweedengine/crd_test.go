package seaweedengine

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type fakeRESTMapper struct {
	available map[schema.GroupVersionKind]bool
}

func (f fakeRESTMapper) KindFor(resource schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, &meta.NoResourceMatchError{PartialResource: resource}
}

func (f fakeRESTMapper) KindsFor(resource schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	return nil, &meta.NoResourceMatchError{PartialResource: resource}
}

func (f fakeRESTMapper) ResourceFor(input schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	return schema.GroupVersionResource{}, &meta.NoResourceMatchError{PartialResource: input}
}

func (f fakeRESTMapper) ResourcesFor(input schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	return nil, &meta.NoResourceMatchError{PartialResource: input}
}

func (f fakeRESTMapper) ResourceSingularizer(resource string) (string, error) {
	return "", &meta.NoResourceMatchError{PartialResource: schema.GroupVersionResource{Resource: resource}}
}

func (f fakeRESTMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	for _, version := range versions {
		gvk := schema.GroupVersionKind{Group: gk.Group, Version: version, Kind: gk.Kind}
		if f.available[gvk] {
			return &meta.RESTMapping{}, nil
		}
	}
	return nil, &meta.NoKindMatchError{GroupKind: gk}
}

func (f fakeRESTMapper) RESTMappings(gk schema.GroupKind, versions ...string) ([]*meta.RESTMapping, error) {
	mapping, err := f.RESTMapping(gk, versions...)
	if err != nil {
		return nil, err
	}
	return []*meta.RESTMapping{mapping}, nil
}

func TestCRDsAvailable(t *testing.T) {
	t.Run("nil mapper", func(t *testing.T) {
		if CRDsAvailable(nil) {
			t.Fatal("expected false for nil mapper")
		}
	})

	t.Run("missing Seaweed kind", func(t *testing.T) {
		mapper := fakeRESTMapper{available: map[schema.GroupVersionKind]bool{
			s3CredentialsGVK: true,
		}}
		if CRDsAvailable(mapper) {
			t.Fatal("expected false when Seaweed CRD is missing")
		}
	})

	t.Run("missing S3Credentials kind", func(t *testing.T) {
		mapper := fakeRESTMapper{available: map[schema.GroupVersionKind]bool{
			seaweedGVK: true,
		}}
		if CRDsAvailable(mapper) {
			t.Fatal("expected false when S3Credentials CRD is missing")
		}
	})

	t.Run("both kinds present", func(t *testing.T) {
		mapper := fakeRESTMapper{available: map[schema.GroupVersionKind]bool{
			seaweedGVK:       true,
			s3CredentialsGVK: true,
		}}
		if !CRDsAvailable(mapper) {
			t.Fatal("expected true when both CRDs are registered")
		}
	})
}
