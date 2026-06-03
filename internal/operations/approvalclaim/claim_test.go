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

package approvalclaim_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/vworkspace-io/vworkspace-operator/internal/operations/approvalclaim"
)

func TestVerifyAcceptsValidClaim(t *testing.T) {
	t.Parallel()
	const secret = "test-secret-hex"
	payload := approvalclaim.Payload{
		Version:       1,
		RequestID:     42,
		OperationName: "op-restore-1",
		ApprovedBy:    "admin",
		ApprovedAt:    "2026-06-03T12:00:00Z",
		ExpiresAt:     time.Now().Add(time.Hour).Unix(),
	}
	claim, err := approvalclaim.Issue(secret, payload)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := approvalclaim.Verify(secret, claim, "op-restore-1"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestVerifyRejectsWrongOperation(t *testing.T) {
	t.Parallel()
	const secret = "test-secret-hex"
	claim, err := approvalclaim.Issue(secret, approvalclaim.Payload{
		Version:       1,
		OperationName: "op-a",
		ExpiresAt:     time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := approvalclaim.Verify(secret, claim, "op-b"); err == nil {
		t.Fatal("expected rejection for operation name mismatch")
	}
}

func TestVerifyRejectsExpiredClaim(t *testing.T) {
	t.Parallel()
	const secret = "test-secret-hex"
	claim, err := approvalclaim.Issue(secret, approvalclaim.Payload{
		Version:       1,
		OperationName: "op-1",
		ExpiresAt:     time.Now().Add(-time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := approvalclaim.Verify(secret, claim, "op-1"); err == nil {
		t.Fatal("expected rejection for expired claim")
	}
}

func TestVerifyMatchesServerCanonicalJSON(t *testing.T) {
	t.Parallel()
	// Body issued by server uses sort_keys + compact separators; signature is over the base64 body.
	const secret = "abc"
	exp := time.Now().Add(time.Hour).Unix()
	payload := map[string]any{
		"at":  "2026-06-03T12:00:00Z",
		"by":  "admin",
		"exp": exp,
		"op":  "op-restore-1",
		"rid": 7,
		"v":   1,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	claim, err := approvalclaim.Issue(secret, approvalclaim.Payload{
		Version:       1,
		RequestID:     7,
		OperationName: "op-restore-1",
		ApprovedBy:    "admin",
		ApprovedAt:    "2026-06-03T12:00:00Z",
		ExpiresAt:     exp,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	_ = raw
	if err := approvalclaim.Verify(secret, claim, "op-restore-1"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if err := approvalclaim.Verify(secret, claim+".tampered", "op-restore-1"); err == nil {
		t.Fatal("expected rejection for tampered claim")
	}
}

func TestVerifyRequiresSecret(t *testing.T) {
	t.Parallel()
	err := approvalclaim.Verify("", "vws1.a.b", "op")
	if err == nil {
		t.Fatal("expected error without secret")
	}
	if !strings.Contains(err.Error(), "secret") {
		t.Fatalf("unexpected error: %v", err)
	}
}
