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

package concurrency_test

import (
	"testing"

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/operations/concurrency"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConflictsMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		existing opsv1alpha1.OperationType
		incoming opsv1alpha1.OperationType
		want     bool
	}{
		{opsv1alpha1.OperationTypeUpgrade, opsv1alpha1.OperationTypeUpgrade, true},
		{opsv1alpha1.OperationTypeUpgrade, opsv1alpha1.OperationTypeBackup, false},
		{opsv1alpha1.OperationTypeUpgrade, opsv1alpha1.OperationTypeRestore, true},
		{opsv1alpha1.OperationTypeBackup, opsv1alpha1.OperationTypeRestore, true},
		{opsv1alpha1.OperationTypeBackup, opsv1alpha1.OperationTypeBackup, false},
		{opsv1alpha1.OperationTypeRestore, opsv1alpha1.OperationTypeBackup, true},
		{opsv1alpha1.OperationTypeUpgrade, opsv1alpha1.OperationTypeRunCommand, false},
	}
	for _, tc := range cases {
		if got := concurrency.Conflicts(tc.existing, tc.incoming); got != tc.want {
			t.Fatalf("Conflicts(%s,%s)=%v want %v", tc.existing, tc.incoming, got, tc.want)
		}
	}
}

func TestInFlight(t *testing.T) {
	t.Parallel()
	running := &opsv1alpha1.Operation{Status: opsv1alpha1.OperationStatus{Phase: opsv1alpha1.PhaseRunning}}
	if !concurrency.InFlight(running) {
		t.Fatal("expected running operation in flight")
	}
	done := &opsv1alpha1.Operation{Status: opsv1alpha1.OperationStatus{Phase: opsv1alpha1.PhaseSucceeded}}
	if concurrency.InFlight(done) {
		t.Fatal("expected succeeded operation not in flight")
	}
	accepted := &opsv1alpha1.Operation{
		Status: opsv1alpha1.OperationStatus{
			Conditions: []metav1.Condition{{
				Type:   opsv1alpha1.ConditionAccepted,
				Status: metav1.ConditionTrue,
			}},
		},
	}
	if !concurrency.InFlight(accepted) {
		t.Fatal("expected accepted operation in flight")
	}
}
