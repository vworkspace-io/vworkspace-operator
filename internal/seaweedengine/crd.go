package seaweedengine

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// watchCRDKinds lists Seaweed API kinds the ApplicationInstance controller watches when CRDs are installed.
var watchCRDKinds = []schema.GroupVersionKind{seaweedGVK, s3CredentialsGVK}

// CRDsAvailable reports whether every Seaweed API kind required for controller watches is registered.
func CRDsAvailable(mapper meta.RESTMapper) bool {
	if mapper == nil {
		return false
	}
	for _, gvk := range watchCRDKinds {
		if _, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
			return false
		}
	}
	return true
}
