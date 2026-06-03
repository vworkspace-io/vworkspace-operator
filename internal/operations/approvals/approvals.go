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

// Package approvals implements Operation approval gating aligned with built-in templates.
package approvals

import (
	"fmt"
	"strings"

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/operations/approvalclaim"
	"github.com/vworkspace-io/vworkspace-operator/internal/operations/templates"
)

// Required reports whether the operation needs an approval claim before running.
func Required(op *opsv1alpha1.Operation) bool {
	if op == nil {
		return false
	}
	if op.Spec.Approvals != nil && op.Spec.Approvals.Required {
		return true
	}
	tpl, ok := templates.Lookup(op.Spec.Type, op.Spec.Engine)
	return ok && tpl.RequiresApproval
}

// Claim returns the trimmed approval claim, if any.
func Claim(op *opsv1alpha1.Operation) string {
	if op == nil || op.Spec.Approvals == nil {
		return ""
	}
	return strings.TrimSpace(op.Spec.Approvals.Claim)
}

// ValidateClaim verifies a non-empty claim when secret is configured.
func ValidateClaim(secret, claim, operationName string) error {
	if strings.TrimSpace(claim) == "" {
		return nil
	}
	return approvalclaim.Verify(secret, claim, operationName)
}

// NeedsApprovalCheck reports whether approval should still be verified before starting.
// Once an operation has left Pending, ExpiresAt is treated as a pre-start deadline only.
func NeedsApprovalCheck(op *opsv1alpha1.Operation) bool {
	if op == nil {
		return false
	}
	switch op.Status.Phase {
	case "", opsv1alpha1.PhasePending:
		return true
	default:
		return false
	}
}

// BlockReason returns whether the reconciler should block and a message for AwaitingApproval.
func BlockReason(op *opsv1alpha1.Operation, secret string) (bool, string) {
	if !Required(op) {
		return false, ""
	}
	claim := Claim(op)
	if claim == "" {
		return true, "approval claim required"
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return true, "approval claim present but operator approval-claim-secret is not configured"
	}
	if err := approvalclaim.Verify(secret, claim, op.Name); err != nil {
		return true, fmt.Sprintf("invalid approval claim: %v", err)
	}
	return false, ""
}
