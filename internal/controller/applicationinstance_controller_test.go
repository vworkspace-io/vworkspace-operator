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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/helmengine"
)

type stubHelmEngine struct{}

func (stubHelmEngine) EnsureRelease(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error {
	return nil
}
func (stubHelmEngine) DeleteRelease(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error {
	return nil
}
func (stubHelmEngine) SyncStatus(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (*helmengine.StatusSnapshot, error) {
	return &helmengine.StatusSnapshot{Ready: true, Reason: "HelmReleaseReady"}, nil
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
				Chart:   appsv1alpha1.ChartSpec{SourceType: appsv1alpha1.ChartSourceHelm, URL: "https://charts.example.com", Name: "nextcloud", Version: "1.0.0"},
				Release: appsv1alpha1.ReleaseSpec{Name: "nextcloud", Namespace: "other"},
				Values:  appsv1alpha1.ValuesSpec{Source: appsv1alpha1.ValuesSourceInline, Inline: &runtime.RawExtension{Raw: []byte(`{}`)}},
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
})
