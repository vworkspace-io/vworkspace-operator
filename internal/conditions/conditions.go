package conditions

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Set(existing []metav1.Condition, conditionType string, status metav1.ConditionStatus, reason, message string) []metav1.Condition {
	now := metav1.NewTime(time.Now().UTC())
	for i := range existing {
		if existing[i].Type == conditionType {
			if existing[i].Status == status && existing[i].Reason == reason && existing[i].Message == message {
				return existing
			}
			existing[i].Status = status
			existing[i].Reason = reason
			existing[i].Message = message
			existing[i].LastTransitionTime = now
			return existing
		}
	}
	return append(existing, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	})
}

func IsTrue(conditions []metav1.Condition, conditionType string) bool {
	for _, c := range conditions {
		if c.Type == conditionType {
			return c.Status == metav1.ConditionTrue
		}
	}
	return false
}

func Get(conditions []metav1.Condition, conditionType string) (metav1.Condition, bool) {
	for _, c := range conditions {
		if c.Type == conditionType {
			return c, true
		}
	}
	return metav1.Condition{}, false
}
