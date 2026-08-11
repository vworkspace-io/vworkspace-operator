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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/helmengine"
	"github.com/vworkspace-io/vworkspace-operator/internal/seaweedengine"
)

type stubHelmEngine struct{}

func (stubHelmEngine) EnsureRelease(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error {
	return nil
}
func (stubHelmEngine) DeleteRelease(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error {
	return nil
}
func (stubHelmEngine) ReleaseExists(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (bool, error) {
	return false, nil
}
func (stubHelmEngine) SyncStatus(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (*helmengine.StatusSnapshot, error) {
	return &helmengine.StatusSnapshot{Ready: true, Reason: "HelmReleaseReady"}, nil
}

// recordingHelmEngine counts engine calls so tests can assert that placeholder
// reconciliation performs no Helm interaction.
type recordingHelmEngine struct {
	ensureCalls            int
	deleteCalls            int
	syncCalls              int
	releaseExistsRemaining int
}

func (e *recordingHelmEngine) EnsureRelease(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error {
	e.ensureCalls++
	return nil
}
func (e *recordingHelmEngine) DeleteRelease(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error {
	e.deleteCalls++
	return nil
}
func (e *recordingHelmEngine) ReleaseExists(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (bool, error) {
	if e.releaseExistsRemaining > 0 {
		e.releaseExistsRemaining--
		return true, nil
	}
	return false, nil
}
func (e *recordingHelmEngine) SyncStatus(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (*helmengine.StatusSnapshot, error) {
	e.syncCalls++
	return &helmengine.StatusSnapshot{Ready: true, Reason: "HelmReleaseReady"}, nil
}

type recordingSeaweedEngine struct {
	ensureCalls  int
	deleteCalls  int
	syncCalls    int
	syncSnapshot *seaweedengine.StatusSnapshot
}

func (e *recordingSeaweedEngine) EnsureSeaweed(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error {
	e.ensureCalls++
	return nil
}
func (e *recordingSeaweedEngine) DeleteSeaweed(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error {
	e.deleteCalls++
	return nil
}
func (e *recordingSeaweedEngine) SeaweedExists(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (bool, error) {
	return false, nil
}
func (e *recordingSeaweedEngine) SyncStatus(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (*seaweedengine.StatusSnapshot, error) {
	e.syncCalls++
	if e.syncSnapshot != nil {
		return e.syncSnapshot, nil
	}
	return &seaweedengine.StatusSnapshot{
		Ready:      true,
		Reason:     "SeaweedReady",
		S3Endpoint: seaweedengine.S3Endpoint(app.Spec.Release.Name, app.Namespace),
	}, nil
}

func (e *recordingSeaweedEngine) ResolveManagedStorage(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (*seaweedengine.ManagedStorageSnapshot, error) {
	return nil, nil
}

func (e *recordingSeaweedEngine) ResolveManagedStorageState(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (*seaweedengine.ManagedStorageSnapshot, bool, error) {
	return nil, false, nil
}

func readyConditionStatus(conds []metav1.Condition) (metav1.ConditionStatus, string) {
	for _, c := range conds {
		if c.Type == appsv1alpha1.ConditionReady {
			return c.Status, c.Reason
		}
	}
	return "", ""
}

var _ = Describe("ApplicationInstance Controller", func() {
	It("sets blocked status when spec is invalid", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "invalid-app", Namespace: "default"}

		app := &appsv1alpha1.ApplicationInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name.Name,
				Namespace:  name.Namespace,
				Finalizers: []string{appsv1alpha1.ApplicationInstanceFinalizer},
			},
			Spec: appsv1alpha1.ApplicationInstanceSpec{
				AppRef:  appsv1alpha1.AppRef{CatalogID: "nextcloud"},
				Chart:   &appsv1alpha1.ChartSpec{SourceType: appsv1alpha1.ChartSourceHelm, URL: "https://charts.example.com", Name: "nextcloud", Version: "1.0.0"},
				Release: &appsv1alpha1.ReleaseSpec{Name: "nextcloud", Namespace: "other"},
				Values:  &appsv1alpha1.ValuesSpec{Source: appsv1alpha1.ValuesSourceInline, Inline: &runtime.RawExtension{Raw: []byte(`{}`)}},
			},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())

		reconciler := &ApplicationInstanceReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			Engine: stubHelmEngine{},
		}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		updated := &appsv1alpha1.ApplicationInstance{}
		Expect(k8sClient.Get(ctx, name, updated)).To(Succeed())
		Expect(updated.Status.Conditions).NotTo(BeEmpty())
	})

	It("brings a placeholder instance to Ready without any Helm interaction", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "cluster-ops", Namespace: "default"}

		app := &appsv1alpha1.ApplicationInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name.Name,
				Namespace:  name.Namespace,
				Finalizers: []string{appsv1alpha1.ApplicationInstanceFinalizer},
				Annotations: map[string]string{
					"ops.vworkspace.io/runcommand": "job",
					"ops.vworkspace.io/runbook":    "workflow",
				},
			},
			Spec: appsv1alpha1.ApplicationInstanceSpec{
				AppRef: appsv1alpha1.AppRef{CatalogID: "cluster-ops"},
				Mode:   appsv1alpha1.InstanceModePlaceholder,
			},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())

		engine := &recordingHelmEngine{}
		reconciler := &ApplicationInstanceReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			Engine: engine,
		}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		updated := &appsv1alpha1.ApplicationInstance{}
		Expect(k8sClient.Get(ctx, name, updated)).To(Succeed())

		status, reason := readyConditionStatus(updated.Status.Conditions)
		Expect(status).To(Equal(metav1.ConditionTrue))
		Expect(reason).To(Equal("Placeholder"))

		// No Helm interaction for a placeholder instance.
		Expect(engine.ensureCalls).To(Equal(0))
		Expect(engine.syncCalls).To(Equal(0))
		Expect(updated.Status.HelmReleaseRef).To(BeNil())

		// Capability annotations are preserved untouched.
		Expect(updated.Annotations).To(HaveKeyWithValue("ops.vworkspace.io/runcommand", "job"))
		Expect(updated.Annotations).To(HaveKeyWithValue("ops.vworkspace.io/runbook", "workflow"))
	})

	It("clears stale Helm status when reconciling a placeholder", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "cluster-ops-stale", Namespace: "default"}

		app := &appsv1alpha1.ApplicationInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name.Name,
				Namespace:  name.Namespace,
				Finalizers: []string{appsv1alpha1.ApplicationInstanceFinalizer},
			},
			Spec: appsv1alpha1.ApplicationInstanceSpec{
				AppRef: appsv1alpha1.AppRef{CatalogID: "cluster-ops"},
				Mode:   appsv1alpha1.InstanceModePlaceholder,
			},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())

		app.Status.HelmReleaseRef = &appsv1alpha1.HelmReleaseRef{Name: "leftover", Namespace: "default"}
		app.Status.LastAppliedChart = &appsv1alpha1.ChartSnapshot{
			SourceType: appsv1alpha1.ChartSourceHelm,
			URL:        "https://charts.example.com",
			Name:       "leftover",
			Version:    "1.0.0",
		}
		Expect(k8sClient.Status().Update(ctx, app)).To(Succeed())

		engine := &recordingHelmEngine{}
		reconciler := &ApplicationInstanceReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			Engine: engine,
		}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		updated := &appsv1alpha1.ApplicationInstance{}
		Expect(k8sClient.Get(ctx, name, updated)).To(Succeed())

		status, reason := readyConditionStatus(updated.Status.Conditions)
		Expect(status).To(Equal(metav1.ConditionTrue))
		Expect(reason).To(Equal("Placeholder"))
		Expect(engine.deleteCalls).To(Equal(0))
		Expect(updated.Status.HelmReleaseRef).To(BeNil())
		Expect(updated.Status.LastAppliedChart).To(BeNil())
	})

	It("finalizes a placeholder instance cleanly without uninstalling a release", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "cluster-ops-del", Namespace: "default"}

		app := &appsv1alpha1.ApplicationInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name.Name,
				Namespace:  name.Namespace,
				Finalizers: []string{appsv1alpha1.ApplicationInstanceFinalizer},
			},
			Spec: appsv1alpha1.ApplicationInstanceSpec{
				AppRef: appsv1alpha1.AppRef{CatalogID: "cluster-ops"},
				Mode:   appsv1alpha1.InstanceModePlaceholder,
			},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		Expect(k8sClient.Delete(ctx, app)).To(Succeed())

		engine := &recordingHelmEngine{}
		reconciler := &ApplicationInstanceReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			Engine: engine,
		}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		// No Helm uninstall for a placeholder; finalizer removed so object is gone.
		Expect(engine.deleteCalls).To(Equal(0))
		gone := &appsv1alpha1.ApplicationInstance{}
		err = k8sClient.Get(ctx, name, gone)
		Expect(err).To(HaveOccurred())
	})

	It("keeps managed-mode reconciliation on the Helm path", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "managed-app", Namespace: "default"}

		app := &appsv1alpha1.ApplicationInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name.Name,
				Namespace:  name.Namespace,
				Finalizers: []string{appsv1alpha1.ApplicationInstanceFinalizer},
			},
			Spec: appsv1alpha1.ApplicationInstanceSpec{
				AppRef:  appsv1alpha1.AppRef{CatalogID: "nextcloud"},
				Chart:   &appsv1alpha1.ChartSpec{SourceType: appsv1alpha1.ChartSourceHelm, URL: "https://charts.example.com", Name: "nextcloud", Version: "1.0.0"},
				Release: &appsv1alpha1.ReleaseSpec{Name: "nextcloud", Namespace: "default"},
				Values:  &appsv1alpha1.ValuesSpec{Source: appsv1alpha1.ValuesSourceInline, Inline: &runtime.RawExtension{Raw: []byte(`{}`)}},
			},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())

		engine := &recordingHelmEngine{}
		reconciler := &ApplicationInstanceReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			Engine: engine,
		}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		// Managed mode still drives the Helm engine.
		Expect(engine.ensureCalls).To(Equal(1))

		updated := &appsv1alpha1.ApplicationInstance{}
		Expect(k8sClient.Get(ctx, name, updated)).To(Succeed())
		status, reason := readyConditionStatus(updated.Status.Conditions)
		Expect(status).To(Equal(metav1.ConditionTrue))
		Expect(reason).To(Equal("HelmReleaseReady"))
	})

	It("reconciles seaweedfs catalog instances via the Seaweed CR engine", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "seaweedfs-dev", Namespace: "default"}

		app := &appsv1alpha1.ApplicationInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name.Name,
				Namespace:  name.Namespace,
				Finalizers: []string{appsv1alpha1.ApplicationInstanceFinalizer},
			},
			Spec: appsv1alpha1.ApplicationInstanceSpec{
				AppRef: appsv1alpha1.AppRef{CatalogID: seaweedengine.CatalogIDSeaweedFS},
				Chart: &appsv1alpha1.ChartSpec{
					SourceType: appsv1alpha1.ChartSourceHelm,
					URL:        "https://vworkspace-io.github.io/vworkspace-server/charts/",
					Name:       "seaweedfs",
					Version:    "0.1.0",
				},
				Release: &appsv1alpha1.ReleaseSpec{Name: "seaweedfs-dev", Namespace: "default"},
				Values: &appsv1alpha1.ValuesSpec{
					Source: appsv1alpha1.ValuesSourceInline,
					Inline: &runtime.RawExtension{Raw: []byte(`{"master":{"replicas":1},"volume":{"replicas":1,"requests":{"storage":"10Gi"}},"filer":{"replicas":1},"s3":{"replicas":1}}`)},
				},
			},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())

		helmEngine := &recordingHelmEngine{}
		seaweedEngine := &recordingSeaweedEngine{}
		reconciler := &ApplicationInstanceReconciler{
			Client:        k8sClient,
			Scheme:        k8sClient.Scheme(),
			Engine:        helmEngine,
			SeaweedEngine: seaweedEngine,
		}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(helmEngine.ensureCalls).To(Equal(0))
		Expect(seaweedEngine.ensureCalls).To(Equal(1))
		Expect(seaweedEngine.syncCalls).To(Equal(1))

		updated := &appsv1alpha1.ApplicationInstance{}
		Expect(k8sClient.Get(ctx, name, updated)).To(Succeed())
		status, reason := readyConditionStatus(updated.Status.Conditions)
		Expect(status).To(Equal(metav1.ConditionTrue))
		Expect(reason).To(Equal("SeaweedReady"))
		Expect(updated.Status.HelmReleaseRef).To(BeNil())
		Expect(updated.Status.Endpoints).To(HaveLen(1))
		Expect(updated.Status.Endpoints[0].Name).To(Equal("s3"))
		Expect(updated.Status.Endpoints[0].URL).To(Equal("http://seaweedfs-dev-s3.default.svc:8333"))
	})

	It("materializes a Seaweed CR for seaweedfs instances", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "seaweedfs-cr", Namespace: "default"}

		app := &appsv1alpha1.ApplicationInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name.Name,
				Namespace:  name.Namespace,
				Finalizers: []string{appsv1alpha1.ApplicationInstanceFinalizer},
			},
			Spec: appsv1alpha1.ApplicationInstanceSpec{
				AppRef: appsv1alpha1.AppRef{CatalogID: seaweedengine.CatalogIDSeaweedFS},
				Chart: &appsv1alpha1.ChartSpec{
					SourceType: appsv1alpha1.ChartSourceHelm,
					URL:        "https://vworkspace-io.github.io/vworkspace-server/charts/",
					Name:       "seaweedfs",
					Version:    "0.1.0",
				},
				Release: &appsv1alpha1.ReleaseSpec{Name: "seaweedfs-cr", Namespace: "default"},
				Values: &appsv1alpha1.ValuesSpec{
					Source: appsv1alpha1.ValuesSourceInline,
					Inline: &runtime.RawExtension{Raw: []byte(`{"master":{"replicas":1},"volume":{"replicas":1,"requests":{"storage":"10Gi"}},"filer":{"replicas":1},"s3":{"replicas":1}}`)},
				},
			},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())

		reconciler := &ApplicationInstanceReconciler{
			Client:        k8sClient,
			Scheme:        k8sClient.Scheme(),
			Engine:        stubHelmEngine{},
			SeaweedEngine: seaweedengine.NewSeaweedEngine(k8sClient),
		}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		sw := seaweedengine.MaterializeSeaweedForTest(app, nil)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(sw), sw)).To(Succeed())
		replicas, found, err := unstructured.NestedInt64(sw.Object, "spec", "s3", "replicas")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(replicas).To(Equal(int64(1)))
	})

	It("finalizes a seaweedfs instance by deleting the Seaweed CR and removing the finalizer", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "seaweedfs-del", Namespace: "default"}

		app := &appsv1alpha1.ApplicationInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name.Name,
				Namespace:  name.Namespace,
				Finalizers: []string{appsv1alpha1.ApplicationInstanceFinalizer},
			},
			Spec: appsv1alpha1.ApplicationInstanceSpec{
				AppRef: appsv1alpha1.AppRef{CatalogID: seaweedengine.CatalogIDSeaweedFS},
				Chart: &appsv1alpha1.ChartSpec{
					SourceType: appsv1alpha1.ChartSourceHelm,
					URL:        "https://vworkspace-io.github.io/vworkspace-server/charts/",
					Name:       "seaweedfs",
					Version:    "0.1.0",
				},
				Release: &appsv1alpha1.ReleaseSpec{Name: "seaweedfs-del", Namespace: "default"},
				Values: &appsv1alpha1.ValuesSpec{
					Source: appsv1alpha1.ValuesSourceInline,
					Inline: &runtime.RawExtension{Raw: []byte(`{"master":{"replicas":1},"volume":{"replicas":1,"requests":{"storage":"10Gi"}},"filer":{"replicas":1},"s3":{"replicas":1}}`)},
				},
			},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())

		seaweedEngine := seaweedengine.NewSeaweedEngine(k8sClient)
		reconciler := &ApplicationInstanceReconciler{
			Client:        k8sClient,
			Scheme:        k8sClient.Scheme(),
			Engine:        stubHelmEngine{},
			SeaweedEngine: seaweedEngine,
		}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		sw := seaweedengine.MaterializeSeaweedForTest(app, nil)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(sw), sw)).To(Succeed())

		Expect(k8sClient.Delete(ctx, app)).To(Succeed())
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		gone := &appsv1alpha1.ApplicationInstance{}
		err = k8sClient.Get(ctx, name, gone)
		Expect(err).To(HaveOccurred())

		err = k8sClient.Get(ctx, client.ObjectKeyFromObject(sw), sw)
		Expect(err).To(HaveOccurred())
	})

	It("blocks seaweed reconcile when a legacy Helm release still exists", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "seaweedfs-migrate", Namespace: "default"}

		app := &appsv1alpha1.ApplicationInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name.Name,
				Namespace:  name.Namespace,
				Finalizers: []string{appsv1alpha1.ApplicationInstanceFinalizer},
			},
			Spec: appsv1alpha1.ApplicationInstanceSpec{
				AppRef: appsv1alpha1.AppRef{CatalogID: seaweedengine.CatalogIDSeaweedFS},
				Chart: &appsv1alpha1.ChartSpec{
					SourceType: appsv1alpha1.ChartSourceHelm,
					URL:        "https://vworkspace-io.github.io/vworkspace-server/charts/",
					Name:       "seaweedfs",
					Version:    "0.1.0",
				},
				Release: &appsv1alpha1.ReleaseSpec{Name: "seaweedfs-migrate", Namespace: "default"},
				Values: &appsv1alpha1.ValuesSpec{
					Source: appsv1alpha1.ValuesSourceInline,
					Inline: &runtime.RawExtension{Raw: []byte(`{"master":{"replicas":1},"volume":{"replicas":1,"requests":{"storage":"10Gi"}},"filer":{"replicas":1},"s3":{"replicas":1}}`)},
				},
			},
		}
		app.Status.HelmReleaseRef = &appsv1alpha1.HelmReleaseRef{Name: "seaweedfs-migrate", Namespace: "default"}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())

		helmEngine := &recordingHelmEngine{releaseExistsRemaining: 1}
		seaweedEngine := &recordingSeaweedEngine{}
		reconciler := &ApplicationInstanceReconciler{
			Client:        k8sClient,
			Scheme:        k8sClient.Scheme(),
			Engine:        helmEngine,
			SeaweedEngine: seaweedEngine,
		}

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		Expect(helmEngine.deleteCalls).To(Equal(0))
		Expect(seaweedEngine.ensureCalls).To(Equal(0))

		updated := &appsv1alpha1.ApplicationInstance{}
		Expect(k8sClient.Get(ctx, name, updated)).To(Succeed())
		status, reason := readyConditionStatus(updated.Status.Conditions)
		Expect(status).To(Equal(metav1.ConditionFalse))
		Expect(reason).To(Equal("HelmMigrationRequired"))
	})

	It("finalizes a blocked seaweedfs instance by removing legacy Helm and Seaweed CR", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "seaweedfs-migrate-del", Namespace: "default"}

		app := &appsv1alpha1.ApplicationInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name.Name,
				Namespace:  name.Namespace,
				Finalizers: []string{appsv1alpha1.ApplicationInstanceFinalizer},
			},
			Spec: appsv1alpha1.ApplicationInstanceSpec{
				AppRef: appsv1alpha1.AppRef{CatalogID: seaweedengine.CatalogIDSeaweedFS},
				Chart: &appsv1alpha1.ChartSpec{
					SourceType: appsv1alpha1.ChartSourceHelm,
					URL:        "https://vworkspace-io.github.io/vworkspace-server/charts/",
					Name:       "seaweedfs",
					Version:    "0.1.0",
				},
				Release: &appsv1alpha1.ReleaseSpec{Name: "seaweedfs-migrate-del", Namespace: "default"},
				Values: &appsv1alpha1.ValuesSpec{
					Source: appsv1alpha1.ValuesSourceInline,
					Inline: &runtime.RawExtension{Raw: []byte(`{"master":{"replicas":1},"volume":{"replicas":1,"requests":{"storage":"10Gi"}},"filer":{"replicas":1},"s3":{"replicas":1}}`)},
				},
			},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())

		helmEngine := &recordingHelmEngine{releaseExistsRemaining: 1}
		seaweedEngine := &recordingSeaweedEngine{}
		reconciler := &ApplicationInstanceReconciler{
			Client:        k8sClient,
			Scheme:        k8sClient.Scheme(),
			Engine:        helmEngine,
			SeaweedEngine: seaweedEngine,
		}

		Expect(k8sClient.Delete(ctx, app)).To(Succeed())
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		Expect(seaweedEngine.deleteCalls).To(Equal(1))
		Expect(helmEngine.deleteCalls).To(Equal(1))

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		gone := &appsv1alpha1.ApplicationInstance{}
		err = k8sClient.Get(ctx, name, gone)
		Expect(err).To(HaveOccurred())
	})

	It("does not report Ready when Seaweed is ready but S3 endpoint is missing", func() {
		ctx := context.Background()
		name := types.NamespacedName{Name: "seaweedfs-s3-wait", Namespace: "default"}

		app := &appsv1alpha1.ApplicationInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name.Name,
				Namespace:  name.Namespace,
				Finalizers: []string{appsv1alpha1.ApplicationInstanceFinalizer},
			},
			Spec: appsv1alpha1.ApplicationInstanceSpec{
				AppRef: appsv1alpha1.AppRef{CatalogID: seaweedengine.CatalogIDSeaweedFS},
				Chart: &appsv1alpha1.ChartSpec{
					SourceType: appsv1alpha1.ChartSourceHelm,
					URL:        "https://vworkspace-io.github.io/vworkspace-server/charts/",
					Name:       "seaweedfs",
					Version:    "0.1.0",
				},
				Release: &appsv1alpha1.ReleaseSpec{Name: "seaweedfs-s3-wait", Namespace: "default"},
				Values: &appsv1alpha1.ValuesSpec{
					Source: appsv1alpha1.ValuesSourceInline,
					Inline: &runtime.RawExtension{Raw: []byte(`{"master":{"replicas":1},"volume":{"replicas":1,"requests":{"storage":"10Gi"}},"filer":{"replicas":1},"s3":{"replicas":1}}`)},
				},
			},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())

		seaweedEngine := &recordingSeaweedEngine{
			syncSnapshot: &seaweedengine.StatusSnapshot{
				Ready:  true,
				HasS3:  true,
				Reason: "SeaweedReady",
			},
		}
		reconciler := &ApplicationInstanceReconciler{
			Client:        k8sClient,
			Scheme:        k8sClient.Scheme(),
			Engine:        stubHelmEngine{},
			SeaweedEngine: seaweedEngine,
		}

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		updated := &appsv1alpha1.ApplicationInstance{}
		Expect(k8sClient.Get(ctx, name, updated)).To(Succeed())
		status, reason := readyConditionStatus(updated.Status.Conditions)
		Expect(status).To(Equal(metav1.ConditionUnknown))
		Expect(reason).To(Equal("WaitingForS3Endpoint"))
		Expect(updated.Status.Endpoints).To(BeEmpty())
	})
})
