package conditions

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetPreservesLastTransitionTimeWhenUnchanged(t *testing.T) {
	existing := []metav1.Condition{{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "HelmReleaseReady",
		Message:            "ok",
		LastTransitionTime: metav1.Now(),
	}}
	updated := Set(existing, "Ready", metav1.ConditionTrue, "HelmReleaseReady", "ok")
	if len(updated) != 1 {
		t.Fatalf("expected one condition, got %d", len(updated))
	}
	if !updated[0].LastTransitionTime.Equal(&existing[0].LastTransitionTime) {
		t.Fatal("lastTransitionTime should be preserved when unchanged")
	}
}
