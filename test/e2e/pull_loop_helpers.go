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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
	"github.com/vworkspace-io/vworkspace-operator/test/mockcontrolplane"
	"github.com/vworkspace-io/vworkspace-operator/test/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	mockOdooNamespace       = "mock-control-plane"
	mockOdooServiceName     = "mock-control-plane"
	mockOdooAdminToken      = "e2e-mock-control-plane-admin"
	pullLoopClusterID       = "cluster-e2e-1"
	pullLoopBootstrapToken  = "e2e-bootstrap-token"
	pullLoopRegistrationTok = "e2e-registration-token"
	vworkspaceSystemNS      = "vworkspace-system"
	agentCredentialsSecret  = "vworkspace-agent-credentials"
	appTestNamespace        = "team-a"
)

var (
	mockOdooBaseURL     string
	mockOdooPortForward *exec.Cmd
	mockOdooAdminClient *mockcontrolplane.AdminClient
)

func mockOdooServiceURL() string {
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:8080", mockOdooServiceName, mockOdooNamespace)
}

func deployMockControlPlane() {
	By("creating mock control plane namespace")
	Expect(utils.EnsureNamespace(mockOdooNamespace)).To(Succeed(), "Failed to create mock control plane namespace")

	manifest := fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: mock-control-plane
  namespace: %s
  labels:
    app: mock-control-plane
spec:
  replicas: 1
  selector:
    matchLabels:
      app: mock-control-plane
  template:
    metadata:
      labels:
        app: mock-control-plane
    spec:
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
      containers:
      - name: mock-control-plane
        image: %s
        imagePullPolicy: IfNotPresent
        args:
          - -addr=:8080
          - -cluster-id=%s
          - -registration-token=%s
          - -bootstrap-token=%s
          - -admin-token=%s
        ports:
        - name: http
          containerPort: 8080
          protocol: TCP
        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          capabilities:
            drop: ["ALL"]
        livenessProbe:
          httpGet:
            path: /healthz
            port: http
          initialDelaySeconds: 3
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /healthz
            port: http
          initialDelaySeconds: 2
          periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
  labels:
    app: mock-control-plane
spec:
  selector:
    app: mock-control-plane
  ports:
  - name: http
    port: 8080
    targetPort: http
    protocol: TCP
`, mockOdooNamespace, mockOdooImage, pullLoopClusterID, pullLoopRegistrationTok,
		pullLoopBootstrapToken, mockOdooAdminToken, mockOdooServiceName, mockOdooNamespace)

	By("deploying in-cluster mock control plane")
	Expect(utils.KubectlApplyYAML(manifest)).To(Succeed(), "Failed to deploy mock control plane")

	By("waiting for mock control plane deployment")
	Eventually(func(g Gomega) {
		cmd := exec.Command("kubectl", "rollout", "status", "deployment/mock-control-plane", "-n", mockOdooNamespace, "--timeout=5s")
		_, err := utils.Run(cmd)
		g.Expect(err).NotTo(HaveOccurred())
	}, 2*time.Minute, 2*time.Second).Should(Succeed())
}

func teardownMockControlPlane() {
	stopMockControlPlanePortForward()
	utils.KubectlDeleteYAML(fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, mockOdooNamespace))
}

func startMockControlPlanePortForward() {
	By("port-forwarding mock control plane admin API")
	mockOdooPortForward = exec.Command("kubectl", "port-forward", "-n", mockOdooNamespace,
		fmt.Sprintf("svc/%s", mockOdooServiceName), "18080:8080")
	Expect(mockOdooPortForward.Start()).To(Succeed(), "Failed to start mock control plane port-forward")
	mockOdooBaseURL = "http://127.0.0.1:18080"
	mockOdooAdminClient = mockcontrolplane.NewAdminClient(mockOdooBaseURL, mockOdooAdminToken, http.DefaultClient)

	Eventually(func(g Gomega) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, mockOdooBaseURL+"/api/admin/jobs/health-check/result", nil)
		g.Expect(err).NotTo(HaveOccurred())
		req.Header.Set("X-Mock-Control-Plane-Admin-Token", mockOdooAdminToken)
		resp, err := http.DefaultClient.Do(req)
		g.Expect(err).NotTo(HaveOccurred())
		_ = resp.Body.Close()
		g.Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	}, 30*time.Second, time.Second).Should(Succeed())
}

func stopMockControlPlanePortForward() {
	if mockOdooPortForward != nil && mockOdooPortForward.Process != nil {
		_ = mockOdooPortForward.Process.Kill()
		mockOdooPortForward = nil
	}
}

func createPullLoopNamespaces() {
	By("creating vworkspace-system namespace")
	Expect(utils.EnsureNamespace(vworkspaceSystemNS)).To(Succeed())

	By("creating application namespace")
	Expect(utils.EnsureNamespace(appTestNamespace)).To(Succeed())
}

func teardownPullLoopNamespaces() {
	for _, ns := range []string{appTestNamespace, vworkspaceSystemNS} {
		_ = utils.DeleteNamespace(ns)
	}
}

func seedAgentCredentialsSecret() {
	By("pre-seeding agent credentials secret")
	manifest := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
type: Opaque
stringData:
  control-plane-base-url: %s
  odoo-base-url: %s
  cluster-id: %s
  token: %s
`, agentCredentialsSecret, namespace, mockOdooServiceURL(), mockOdooServiceURL(), pullLoopClusterID, pullLoopBootstrapToken)
	Expect(utils.KubectlApplyYAML(manifest)).To(Succeed())
}

func deployOperatorWithAgent() {
	By("installing CRDs")
	cmd := exec.Command("make", "install")
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

	By("creating operator namespace")
	Expect(utils.EnsureNamespace(namespace)).To(Succeed())

	seedAgentCredentialsSecret()

	By("labeling operator namespace with restricted pod security")
	cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
		"pod-security.kubernetes.io/enforce=restricted")
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())

	By("deploying operator with Pull-mode agent enabled")
	cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", managerImage))
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to deploy operator")

	Eventually(func(g Gomega) {
		cmd := exec.Command("kubectl", "rollout", "status",
			"deployment/vworkspace-operator-controller-manager", "-n", namespace, "--timeout=60s")
		_, err := utils.Run(cmd)
		g.Expect(err).NotTo(HaveOccurred())
	}, 3*time.Minute, 5*time.Second).Should(Succeed())

	patch := fmt.Sprintf(`[
  {"op": "replace", "path": "/spec/strategy", "value": {"type": "Recreate"}},
  {"op": "replace", "path": "/spec/template/spec/containers/0/args/3", "value": "--agent-enabled=true"},
  {"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--control-plane-base-url=%s"},
  {"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--cluster-id=%s"},
  {"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--agent-token=%s"},
  {"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--agent-poll-interval=5s"},
  {"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--agent-credentials-secret=%s"},
  {"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--agent-credentials-namespace=%s"}
]`, mockOdooServiceURL(), pullLoopClusterID, pullLoopBootstrapToken, agentCredentialsSecret, namespace)

	cmd = exec.Command("kubectl", "patch", "deployment", "vworkspace-operator-controller-manager",
		"-n", namespace, "--type=json", "-p", patch)
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to patch operator for agent mode")

	Eventually(func(g Gomega) {
		cmd := exec.Command("kubectl", "rollout", "status",
			"deployment/vworkspace-operator-controller-manager", "-n", namespace, "--timeout=60s")
		_, err := utils.Run(cmd)
		g.Expect(err).NotTo(HaveOccurred())
	}, 5*time.Minute, 5*time.Second).Should(Succeed())
}

func teardownOperatorWithAgent() {
	// Kind cleanup follows; do not block on CRD removal while test CRs may still exist.
	cmd := exec.Command("make", "undeploy", "KUBECTL_WAIT=false")
	_, _ = utils.Run(cmd)
	cmd = exec.Command("make", "uninstall", "KUBECTL_WAIT=false")
	_, _ = utils.Run(cmd)
	_ = utils.DeleteNamespace(namespace)
}

func applyClusterRegistrationCR() {
	By("applying Cluster CR to verify registration flow")
	manifest := fmt.Sprintf(`apiVersion: ops.vworkspace.io/v1alpha1
kind: Cluster
metadata:
  name: %s
  namespace: %s
spec:
  clusterId: %s
  odooBaseUrl: %s
  registrationToken: %s
`, pullLoopClusterID, vworkspaceSystemNS, pullLoopClusterID, mockOdooServiceURL(), pullLoopRegistrationTok)
	Expect(utils.KubectlApplyYAML(manifest)).To(Succeed())

	Eventually(func(g Gomega) {
		cmd := exec.Command("kubectl", "get", "secret", agentCredentialsSecret, "-n", namespace,
			"-o", "jsonpath={.data.token}")
		out, err := utils.Run(cmd)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(strings.TrimSpace(out)).NotTo(BeEmpty())
	}, 2*time.Minute, 2*time.Second).Should(Succeed())
}

func sampleE2EApplicationInstance(name string) *appsv1alpha1.ApplicationInstance {
	return &appsv1alpha1.ApplicationInstance{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1alpha1.GroupVersion.String(),
			Kind:       "ApplicationInstance",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: appTestNamespace,
		},
		Spec: appsv1alpha1.ApplicationInstanceSpec{
			AppRef: appsv1alpha1.AppRef{CatalogID: "nextcloud"},
			Chart: appsv1alpha1.ChartSpec{
				SourceType: appsv1alpha1.ChartSourceHelm,
				URL:        "https://charts.example.com",
				Name:       "nextcloud",
				Version:    "6.6.0",
			},
			Release: appsv1alpha1.ReleaseSpec{Name: name, Namespace: appTestNamespace},
			Values: appsv1alpha1.ValuesSpec{
				Source: appsv1alpha1.ValuesSourceInline,
				Inline: &runtime.RawExtension{Raw: []byte(`{}`)},
			},
		},
	}
}

func enqueueApplyJob(jobID, idempotencyKey string, app *appsv1alpha1.ApplicationInstance) {
	payload, err := json.Marshal(app)
	Expect(err).NotTo(HaveOccurred(), "marshal ApplicationInstance payload")

	By("enqueueing apply job on mock control plane")
	Expect(mockOdooAdminClient.EnqueueJob(pullLoopClusterID, agent.Job{
		ID:             jobID,
		Kind:           "apply",
		Payload:        payload,
		IdempotencyKey: idempotencyKey,
		ExpiresAt:      time.Now().Add(time.Hour),
	})).To(Succeed())
}

func waitForApplicationInstance(name string) {
	Eventually(func(g Gomega) {
		cmd := exec.Command("kubectl", "get", "applicationinstance", name, "-n", appTestNamespace)
		_, err := utils.Run(cmd)
		g.Expect(err).NotTo(HaveOccurred(), "ApplicationInstance should exist")
	}, 2*time.Minute, 2*time.Second).Should(Succeed())
}

func waitForHelmRelease(name string) {
	if !utils.IsFluxCRDsInstalled() {
		Skip("Flux HelmRelease CRD is not installed; install Flux CRDs in e2e BeforeSuite")
	}
	Eventually(func(g Gomega) {
		cmd := exec.Command("kubectl", "get", "helmrelease", name, "-n", appTestNamespace)
		_, err := utils.Run(cmd)
		g.Expect(err).NotTo(HaveOccurred(), "HelmRelease should be materialized by reconciler")
	}, 2*time.Minute, 2*time.Second).Should(Succeed())
}

func waitForMockJobSucceeded(jobID string) agent.JobResult {
	var result agent.JobResult
	Eventually(func(g Gomega) {
		var err error
		result, err = mockOdooAdminClient.WaitForJobResult(jobID, 5*time.Second)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result.Outcome).To(Equal(agent.OutcomeSucceeded))
	}, 3*time.Minute, time.Second).Should(Succeed())
	return result
}

func waitForConditionTransitionEvents(appName string) {
	Eventually(func(g Gomega) {
		events, err := mockOdooAdminClient.ListEvents(pullLoopClusterID, mockcontrolplane.EventFilter{
			Kind:      "ApplicationInstance",
			Namespace: appTestNamespace,
			Name:      appName,
		})
		g.Expect(err).NotTo(HaveOccurred(), "mock control plane admin events API should respond")

		found := false
		for _, ev := range events {
			if ev.Kind != "ConditionTransition" {
				continue
			}
			if ev.EventKey == "" {
				continue
			}
			for _, c := range ev.Conditions {
				if c.Type == "Reconciling" || c.Type == "Ready" || c.Type == "Blocked" {
					found = true
					break
				}
			}
		}
		g.Expect(found).To(BeTrue(), "expected ApplicationInstance condition transition events on mock control plane")
	}, 2*time.Minute, 2*time.Second).Should(Succeed())
}

func skipUnlessKindAvailable() {
	if os.Getenv("SKIP_E2E") == "true" {
		Skip("SKIP_E2E=true")
	}
	cmd := exec.Command("kind", "version")
	if err := cmd.Run(); err != nil {
		Skip("kind is not available; e2e requires a kind cluster (see make test-e2e)")
	}
}
