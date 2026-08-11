package helmengine

import (
	"context"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
)

// Engine materializes and observes Helm releases for ApplicationInstance resources.
type Engine interface {
	EnsureRelease(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error
	DeleteRelease(ctx context.Context, app *appsv1alpha1.ApplicationInstance) error
	ReleaseExists(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (bool, error)
	SyncStatus(ctx context.Context, app *appsv1alpha1.ApplicationInstance) (*StatusSnapshot, error)
}

// StatusSnapshot captures mapped HelmRelease status for ApplicationInstance conditions.
type StatusSnapshot struct {
	Ready       bool
	Reconciling bool
	Degraded    bool
	Reason      string
	Message     string
	ReleaseRef  appsv1alpha1.HelmReleaseRef
}
