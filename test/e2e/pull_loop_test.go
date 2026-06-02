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
	"encoding/json"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
	"github.com/vworkspace-io/vworkspace-operator/test/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var _ = Describe("Pull-mode job loop", Ordered, func() {
	BeforeAll(func() {
		skipUnlessKindAvailable()
		deployMockControlPlane()
		createPullLoopNamespaces()
		deployOperatorWithAgent()
		startMockControlPlanePortForward()
	})

	AfterAll(func() {
		stopMockControlPlanePortForward()
		teardownPullLoopNamespaces()
		teardownMockControlPlane()
		teardownOperatorWithAgent()
	})

	SetDefaultEventuallyTimeout(3 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)

	It("registers the cluster via Cluster CR", func() {
		applyClusterRegistrationCR()
	})

	It("applies jobs from mock control plane through the deployed operator", func() {
		const (
			appName        = "pull-loop-e2e-app"
			jobID          = "job-e2e-pull-1"
			idempotencyKey = "pull-loop-e2e-key-1"
		)

		By("verifying target namespace is absent before apply job")
		cmd := exec.Command("kubectl", "get", "namespace", appTestNamespace)
		_, err := utils.Run(cmd)
		Expect(err).To(HaveOccurred(), "apps namespace should not exist before the applier runs")

		app := sampleE2EApplicationInstance(appName)
		enqueueApplyJob(jobID, idempotencyKey, app)

		waitForApplicationInstance(appName)

		By("verifying applier created the gated target namespace")
		cmd = exec.Command("kubectl", "get", "namespace", appTestNamespace,
			"-o", "jsonpath={.metadata.labels.managed-by}")
		label, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(label).To(Equal("vworkspace"))
		waitForHelmRelease(appName)

		result := waitForMockJobSucceeded(jobID)
		Expect(result.AppliedRef).NotTo(BeNil())
		Expect(result.AppliedRef.Kind).To(Equal("ApplicationInstance"))
		Expect(result.AppliedRef.Name).To(Equal(appName))
		Expect(result.AppliedRef.Namespace).To(Equal(appTestNamespace))

		waitForConditionTransitionEvents(appName)
	})

	It("creates a Velero Backup CR from a backup operation job", func() {
		if !utils.IsVeleroCRDsInstalled() {
			Skip("Velero Backup CRD is not installed; install Velero CRDs in e2e BeforeSuite or set E2E_INSTALL_VELERO=true")
		}

		const (
			opName  = "backup-e2e-1"
			appName = "pull-loop-e2e-app"
			jobID   = "job-e2e-backup-1"
		)

		op := &opsv1alpha1.Operation{
			TypeMeta: metav1.TypeMeta{
				APIVersion: opsv1alpha1.GroupVersion.String(),
				Kind:       "Operation",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      opName,
				Namespace: appTestNamespace,
			},
			Spec: opsv1alpha1.OperationSpec{
				Type: opsv1alpha1.OperationTypeBackup,
				TargetRef: opsv1alpha1.TargetRef{
					Kind: "ApplicationInstance",
					Name: appName,
				},
				Engine: opsv1alpha1.EngineVelero,
				Parameters: &runtime.RawExtension{
					Raw: []byte(`{"storageLocation":"default"}`),
				},
			},
		}
		payload, err := json.Marshal(op)
		Expect(err).NotTo(HaveOccurred())

		By("enqueueing backup operation apply job on mock control plane")
		Expect(mockOdooAdminClient.EnqueueJob(pullLoopClusterID, agent.Job{
			ID:        jobID,
			Kind:      "apply",
			Payload:   payload,
			ExpiresAt: time.Now().Add(time.Hour),
		})).To(Succeed())

		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "backup", opName, "-n", appTestNamespace)
			_, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred(), "Velero Backup CR should be created")
		}, 2*time.Minute, 2*time.Second).Should(Succeed())

		result := waitForMockJobSucceeded(jobID)
		Expect(result.Outcome).To(Equal(agent.OutcomeSucceeded))
		Expect(result.AppliedRef).NotTo(BeNil())
		Expect(result.AppliedRef.Kind).To(Equal("Operation"))
	})
})
