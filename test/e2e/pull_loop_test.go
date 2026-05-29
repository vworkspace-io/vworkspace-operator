//go:build e2e
// +build e2e

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

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
)

// Pull-mode e2e against mock Odoo inside kind is deferred: the operator pod cannot
// reach the test process's httptest server without a sidecar or in-cluster mock deployment.
// Full loop coverage lives in test/integration/pull_loop_test.go (runs under make test).
var _ = Describe("Pull-mode job loop", func() {
	It("applies jobs from mock Odoo through the deployed operator", func() {
		Skip("Phase 1f: deploy mock Odoo sidecar or in-cluster mock service; see test/integration/pull_loop_test.go")
	})
})
