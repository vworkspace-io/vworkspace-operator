package seaweedengine

import (
	"context"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
)

const (
	// CatalogIDSeaweedFS is the stable catalog identifier for managed SeaweedFS instances.
	CatalogIDSeaweedFS = "seaweedfs"
)

// IsSeaweedWorkload reports whether the ApplicationInstance should be reconciled
// via the native Seaweed CR engine instead of Flux Helm.
func IsSeaweedWorkload(app *appsv1alpha1.ApplicationInstance) bool {
	return app != nil && app.Spec.AppRef.CatalogID == CatalogIDSeaweedFS
}

// Engine materializes and observes Seaweed CRs for ApplicationInstance resources.
type Engine interface {
	EnsureSeaweed(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error
	DeleteSeaweed(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error
	SyncStatus(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (*StatusSnapshot, error)
}

// StatusSnapshot captures mapped Seaweed status for ApplicationInstance conditions.
type StatusSnapshot struct {
	Ready       bool
	Reconciling bool
	Degraded    bool
	Reason      string
	Message     string
	S3Endpoint  string
}
