/*
Copyright 2026 vWorkspace Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package concurrency encodes the typed conflict matrix for Operation resources.
package concurrency

import (
	"context"
	"fmt"

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Conflicts reports whether an in-flight existing operation blocks the incoming type.
// RunCommand and Runbook are concurrent-safe with all verbs unless a future catalog
// template marks them exclusive.
func Conflicts(existing, incoming opsv1alpha1.OperationType) bool {
	switch incoming {
	case opsv1alpha1.OperationTypeUpgrade:
		return existing == opsv1alpha1.OperationTypeUpgrade
	case opsv1alpha1.OperationTypeRestore:
		return existing == opsv1alpha1.OperationTypeUpgrade || existing == opsv1alpha1.OperationTypeBackup
	case opsv1alpha1.OperationTypeBackup:
		return existing == opsv1alpha1.OperationTypeRestore
	default:
		return false
	}
}

// InFlight reports whether the operation should be treated as active for concurrency.
func InFlight(op *opsv1alpha1.Operation) bool {
	if op == nil {
		return false
	}
	switch op.Status.Phase {
	case opsv1alpha1.PhaseSucceeded, opsv1alpha1.PhaseFailed, opsv1alpha1.PhaseCancelled:
		return false
	case opsv1alpha1.PhaseRunning, opsv1alpha1.PhasePending:
		return true
	}
	for _, c := range op.Status.Conditions {
		if c.Type == opsv1alpha1.ConditionAccepted && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// Conflict describes an active operation that blocks the candidate.
type Conflict struct {
	Name string
	Type opsv1alpha1.OperationType
}

// FindConflict returns the first in-flight operation on the same target that conflicts.
func FindConflict(ctx context.Context, cl client.Reader, op *opsv1alpha1.Operation) (*Conflict, error) {
	list := &opsv1alpha1.OperationList{}
	if err := cl.List(ctx, list, client.InNamespace(op.Namespace)); err != nil {
		return nil, fmt.Errorf("list operations: %w", err)
	}
	for _, item := range list.Items {
		if item.Name == op.Name {
			continue
		}
		if item.Spec.TargetRef.Name != op.Spec.TargetRef.Name {
			continue
		}
		if !InFlight(&item) {
			continue
		}
		if Conflicts(item.Spec.Type, op.Spec.Type) {
			return &Conflict{Name: item.Name, Type: item.Spec.Type}, nil
		}
	}
	return nil, nil
}

// FormatConflict returns a human-readable conflict reason.
func FormatConflict(c *Conflict) string {
	return fmt.Sprintf("conflicts with operation %q (%s)", c.Name, c.Type)
}
