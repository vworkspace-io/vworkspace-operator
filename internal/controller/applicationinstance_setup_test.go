package controller

import (
	"path/filepath"
	"testing"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/helmengine"
	"github.com/vworkspace-io/vworkspace-operator/internal/seaweedengine"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestApplicationInstanceSetupWithoutSeaweedCRDs(t *testing.T) {
	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	if dir := getFirstFoundEnvTestBinaryDir(); dir != "" {
		env.BinaryAssetsDirectory = dir
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() {
		if err := env.Stop(); err != nil {
			t.Errorf("stop envtest: %v", err)
		}
	})

	if err := appsv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	reconciler := &ApplicationInstanceReconciler{
		Client:                mgr.GetClient(),
		Scheme:                mgr.GetScheme(),
		Engine:                helmengine.NewFluxEngine(mgr.GetClient()),
		SeaweedEngine:         seaweedengine.NewSeaweedEngine(mgr.GetClient()),
		SeaweedWatchesEnabled: false,
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		t.Fatalf("setup with manager: %v", err)
	}
}
