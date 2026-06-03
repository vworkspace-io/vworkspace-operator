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

package approvals_test

import (
	"testing"
	"time"

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/operations/approvalclaim"
	"github.com/vworkspace-io/vworkspace-operator/internal/operations/approvals"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNeedsApprovalCheck(t *testing.T) {
	t.Parallel()
	cases := []struct {
		phase opsv1alpha1.OperationPhase
		want  bool
	}{
		{"", true},
		{opsv1alpha1.PhasePending, true},
		{opsv1alpha1.PhaseRunning, false},
		{opsv1alpha1.PhaseSucceeded, false},
		{opsv1alpha1.PhaseFailed, false},
	}
	for _, tc := range cases {
		op := &opsv1alpha1.Operation{Status: opsv1alpha1.OperationStatus{Phase: tc.phase}}
		if got := approvals.NeedsApprovalCheck(op); got != tc.want {
			t.Fatalf("NeedsApprovalCheck(phase=%q)=%v want %v", tc.phase, got, tc.want)
		}
	}
}

func TestBlockReasonRestoreWithoutClaim(t *testing.T) {
	t.Parallel()
	op := &opsv1alpha1.Operation{
		Spec: opsv1alpha1.OperationSpec{
			Type:   opsv1alpha1.OperationTypeRestore,
			Engine: opsv1alpha1.EngineVelero,
		},
	}
	blocked, reason := approvals.BlockReason(op, "secret")
	if !blocked {
		t.Fatal("expected restore.velero without claim to require approval")
	}
	if reason != "approval claim required" {
		t.Fatalf("unexpected reason: %q", reason)
	}
}

func TestBlockReasonRejectsExpiredClaim(t *testing.T) {
	t.Parallel()
	const secret = "test-secret"
	claim, err := approvalclaim.Issue(secret, approvalclaim.Payload{
		Version:       1,
		OperationName: "restore-1",
		ExpiresAt:     time.Now().Add(-time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	op := &opsv1alpha1.Operation{
		ObjectMeta: metav1.ObjectMeta{Name: "restore-1"},
		Spec: opsv1alpha1.OperationSpec{
			Type:   opsv1alpha1.OperationTypeRestore,
			Engine: opsv1alpha1.EngineVelero,
			Approvals: &opsv1alpha1.ApprovalsSpec{
				Claim: claim,
			},
		},
	}
	blocked, reason := approvals.BlockReason(op, secret)
	if !blocked {
		t.Fatal("expected expired claim to block")
	}
	if reason == "" {
		t.Fatal("expected block reason message")
	}
}
