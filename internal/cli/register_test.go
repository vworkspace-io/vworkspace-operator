package cli

import (
	"testing"

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClusterRegistrationObjectIsClusterScoped(t *testing.T) {
	cluster := &opsv1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: opsv1alpha1.GroupVersion.String(),
			Kind:       "Cluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-dev-1",
		},
	}
	if cluster.Namespace != "" {
		t.Fatalf("Cluster CR must be cluster-scoped (no namespace); got %q", cluster.Namespace)
	}
	if cluster.APIVersion == "" || cluster.Kind == "" {
		t.Fatalf("Cluster CR must set TypeMeta for server-side apply; got apiVersion=%q kind=%q", cluster.APIVersion, cluster.Kind)
	}
}
