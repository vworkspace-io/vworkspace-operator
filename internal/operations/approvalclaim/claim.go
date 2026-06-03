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

// Package approvalclaim verifies Odoo-issued vws1 approval claims for Operation.spec.approvals.
package approvalclaim

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const Prefix = "vws1"

// Payload is the decoded vws1 claim body (matches server vws_approval_claim.issue_approval_claim).
type Payload struct {
	Version       int    `json:"v"`
	RequestID     int    `json:"rid"`
	OperationName string `json:"op"`
	ApprovedBy    string `json:"by"`
	ApprovedAt    string `json:"at"`
	ExpiresAt     int64  `json:"exp"`
}

// Verify checks claim format, HMAC, expiry, and operation name binding.
// secret must match server ir.config_parameter vws_operations.approval_claim_secret.
func Verify(secret, claim, operationName string) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return fmt.Errorf("approval claim verification requires a configured secret")
	}
	claim = strings.TrimSpace(claim)
	if claim == "" {
		return fmt.Errorf("approval claim is empty")
	}
	prefix, body, digest, err := splitClaim(claim)
	if err != nil {
		return err
	}
	if prefix != Prefix {
		return fmt.Errorf("unsupported approval claim prefix %q", prefix)
	}
	expected := hmacSHA256Hex(secret, body)
	if !hmac.Equal([]byte(expected), []byte(digest)) {
		return fmt.Errorf("approval claim signature mismatch")
	}
	payload, err := decodePayload(body)
	if err != nil {
		return err
	}
	if payload.Version != 1 {
		return fmt.Errorf("unsupported approval claim version %d", payload.Version)
	}
	if strings.TrimSpace(payload.OperationName) != strings.TrimSpace(operationName) {
		return fmt.Errorf("approval claim operation %q does not match %q", payload.OperationName, operationName)
	}
	if time.Now().Unix() > payload.ExpiresAt {
		return fmt.Errorf("approval claim expired")
	}
	return nil
}

func splitClaim(claim string) (prefix, body, digest string, err error) {
	parts := strings.Split(claim, ".")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("malformed approval claim")
	}
	return parts[0], parts[1], parts[2], nil
}

func hmacSHA256Hex(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

func decodePayload(body string) (Payload, error) {
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return Payload{}, fmt.Errorf("decode approval claim payload: %w", err)
	}
	var payload Payload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Payload{}, fmt.Errorf("parse approval claim payload: %w", err)
	}
	return payload, nil
}

// Issue builds a signed claim for tests (mirrors server issue_approval_claim).
func Issue(secret string, payload Payload) (string, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", fmt.Errorf("secret is required")
	}
	raw, err := marshalCanonicalPayload(payload)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	digest := hmacSHA256Hex(secret, body)
	return fmt.Sprintf("%s.%s.%s", Prefix, body, digest), nil
}

// marshalCanonicalPayload matches Odoo json.dumps(..., sort_keys=True, separators=(",", ":")).
func marshalCanonicalPayload(p Payload) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	pairs := []struct {
		key string
		val func() error
	}{
		{"at", func() error {
			b, err := json.Marshal(p.ApprovedAt)
			if err != nil {
				return err
			}
			buf.WriteString(`"at":`)
			buf.Write(b)
			return nil
		}},
		{"by", func() error {
			b, err := json.Marshal(p.ApprovedBy)
			if err != nil {
				return err
			}
			buf.WriteString(`"by":`)
			buf.Write(b)
			return nil
		}},
		{"exp", func() error {
			b, err := json.Marshal(p.ExpiresAt)
			if err != nil {
				return err
			}
			buf.WriteString(`"exp":`)
			buf.Write(b)
			return nil
		}},
		{"op", func() error {
			b, err := json.Marshal(p.OperationName)
			if err != nil {
				return err
			}
			buf.WriteString(`"op":`)
			buf.Write(b)
			return nil
		}},
		{"rid", func() error {
			b, err := json.Marshal(p.RequestID)
			if err != nil {
				return err
			}
			buf.WriteString(`"rid":`)
			buf.Write(b)
			return nil
		}},
		{"v", func() error {
			b, err := json.Marshal(p.Version)
			if err != nil {
				return err
			}
			buf.WriteString(`"v":`)
			buf.Write(b)
			return nil
		}},
	}
	for i, pair := range pairs {
		if i > 0 {
			buf.WriteByte(',')
		}
		_ = pair.key
		if err := pair.val(); err != nil {
			return nil, err
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
