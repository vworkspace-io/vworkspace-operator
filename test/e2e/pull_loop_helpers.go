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
	"github.com/vworkspace-io/vworkspace-operator/test/mockodoo"
	"github.com/vworkspace-io/vworkspace-operator/test/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	mockOdooNamespace       = "mock-odoo"
	mockOdooServiceName     = "mock-odoo"
	mockOdooAdminToken      = "e2e-mock-odoo-admin"
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
	mockOdooAdminClient *mockodoo.AdminClient
)

func mockOdooServiceURL() string {
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:8080", mockOdooServiceName, mockOdooNamespace)
}

func deployMockOdoo() {
	By("creating mock Odoo namespace")
	cmd := exec.Command("kubectl", "create", "ns", mockOdooNamespace)
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to create mock Odoo namespace")

	manifest := fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: mock-odoo
  namespace: %s
  labels:
    app: mock-odoo
spec:
  replicas: 1
  selector:
    matchLabels:
      app: mock-odoo
  template:
    metadata:
      labels:
        app: mock-odoo
    spec:
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
      containers:
      - name: mock-odoo
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
    app: mock-odoo
spec:
  selector:
    app: mock-odoo
  ports:
  - name: http
    port: 8080
    targetPort: http
    protocol: TCP
`, mockOdooNamespace, mockOdooImage, pullLoopClusterID, pullLoopRegistrationTok,
		pullLoopBootstrapToken, mockOdooAdminToken, mockOdooServiceName, mockOdooNamespace)

	By("deploying in-cluster mock Odoo")
	Expect(utils.KubectlApplyYAML(manifest)).To(Succeed(), "Failed to deploy mock Odoo")

	By("waiting for mock Odoo deployment")
	Eventually(func(g Gomega) {
		cmd := exec.Command("kubectl", "rollout", "status", "deployment/mock-odoo", "-n", mockOdooNamespace, "--timeout=5s")
		_, err := utils.Run(cmd)
		g.Expect(err).NotTo(HaveOccurred())
	}, 2*time.Minute, 2*time.Second).Should(Succeed())
}

func teardownMockOdoo() {
	stopMockOdooPortForward()
	utils.KubectlDeleteYAML(fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, mockOdooNamespace))
}

func startMockOdooPortForward() {
	By("port-forwarding mock Odoo admin API")
	mockOdooPortForward = exec.Command("kubectl", "port-forward", "-n", mockOdooNamespace,
		fmt.Sprintf("svc/%s", mockOdooServiceName), "18080:8080")
	Expect(mockOdooPortForward.Start()).To(Succeed(), "Failed to start mock Odoo port-forward")
	mockOdooBaseURL = "http://127.0.0.1:18080"
	mockOdooAdminClient = mockodoo.NewAdminClient(mockOdooBaseURL, mockOdooAdminToken, http.DefaultClient)

	Eventually(func(g Gomega) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, mockOdooBaseURL+"/api/admin/jobs/health-check/result", nil)
		g.Expect(err).NotTo(HaveOccurred())
		req.Header.Set("X-Mock-Odoo-Admin-Token", mockOdooAdminToken)
		resp, err := http.DefaultClient.Do(req)
		g.Expect(err).NotTo(HaveOccurred())
		_ = resp.Body.Close()
		g.Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	}, 30*time.Second, time.Second).Should(Succeed())
}

func stopMockOdooPortForward() {
	if mockOdooPortForward != nil && mockOdooPortForward.Process != nil {
		_ = mockOdooPortForward.Process.Kill()
		mockOdooPortForward = nil
	}
}

func createPullLoopNamespaces() {
	By("creating vworkspace-system namespace")
	cmd := exec.Command("kubectl", "create", "ns", vworkspaceSystemNS)
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())

	By("creating application namespace")
	cmd = exec.Command("kubectl", "create", "ns", appTestNamespace)
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())
}

func teardownPullLoopNamespaces() {
	for _, ns := range []string{appTestNamespace, vworkspaceSystemNS} {
		cmd := exec.Command("kubectl", "delete", "ns", ns, "--ignore-not-found", "--wait=false")
		_, _ = utils.Run(cmd)
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
  odoo-base-url: %s
  cluster-id: %s
  token: %s
`, agentCredentialsSecret, namespace, mockOdooServiceURL(), pullLoopClusterID, pullLoopBootstrapToken)
	Expect(utils.KubectlApplyYAML(manifest)).To(Succeed())
}

func deployOperatorWithAgent() {
	By("installing CRDs")
	cmd := exec.Command("make", "install")
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

	By("creating operator namespace")
	cmd = exec.Command("kubectl", "create", "ns", namespace)
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())

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
  {"op": "replace", "path": "/spec/template/spec/containers/0/args", "value": [
    "--leader-elect",
    "--health-probe-bind-address=:8081",
    "--agent-enabled=true",
    "--odoo-base-url=%s",
    "--cluster-id=%s",
    "--agent-token=%s",
    "--agent-poll-interval=5s",
    "--agent-credentials-secret=%s",
    "--agent-credentials-namespace=%s"
  ]}
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
	cmd := exec.Command("make", "undeploy")
	_, _ = utils.Run(cmd)
	cmd = exec.Command("make", "uninstall")
	_, _ = utils.Run(cmd)
	cmd = exec.Command("kubectl", "delete", "ns", namespace, "--ignore-not-found", "--wait=false")
	_, _ = utils.Run(cmd)
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

	By("enqueueing apply job on mock Odoo")
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

func skipUnlessKindAvailable() {
	if os.Getenv("SKIP_E2E") == "true" {
		Skip("SKIP_E2E=true")
	}
	cmd := exec.Command("kind", "version")
	if err := cmd.Run(); err != nil {
		Skip("kind is not available; e2e requires a kind cluster (see make test-e2e)")
	}
}
