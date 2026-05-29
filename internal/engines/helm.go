package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type helmParameters struct {
	TargetVersion string         `json:"targetVersion"`
	ValuesPatch   map[string]any `json:"valuesPatch"`
}

// HelmEngine upgrades an ApplicationInstance by patching its HelmRelease.
type HelmEngine struct {
	Client client.Client
}

func NewHelmEngine(c client.Client) *HelmEngine {
	return &HelmEngine{Client: c}
}

func (e *HelmEngine) Name() opsv1alpha1.OperationEngine {
	return opsv1alpha1.EngineHelm
}

func (e *HelmEngine) Materialize(ctx context.Context, op *opsv1alpha1.Operation, target *appsv1alpha1.ApplicationInstance) error {
	params, err := parseHelmParameters(op)
	if err != nil {
		return err
	}
	hr := &unstructured.Unstructured{}
	hr.SetGroupVersionKind(schema.GroupVersionKind{Group: "helm.toolkit.fluxcd.io", Version: "v2", Kind: "HelmRelease"})
	if err := e.Client.Get(ctx, client.ObjectKey{Namespace: target.Namespace, Name: target.Spec.Release.Name}, hr); err != nil {
		return fmt.Errorf("get helmrelease: %w", err)
	}
	_, err = controllerutil.CreateOrUpdate(ctx, e.Client, hr, func() error {
		if params.TargetVersion != "" {
			if err := unstructured.SetNestedField(hr.Object, params.TargetVersion, "spec", "chart", "spec", "version"); err != nil {
				return err
			}
		}
		if len(params.ValuesPatch) > 0 {
			current, _, _ := unstructured.NestedMap(hr.Object, "spec", "values")
			if current == nil {
				current = map[string]any{}
			}
			maps.Copy(current, params.ValuesPatch)
			if err := unstructured.SetNestedMap(hr.Object, current, "spec", "values"); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func (e *HelmEngine) Status(ctx context.Context, op *opsv1alpha1.Operation) (Status, error) {
	return Status{
		Phase:  opsv1alpha1.PhaseRunning,
		Reason: "EngineStarted",
		Done:   false,
	}, nil
}

func (e *HelmEngine) Cancel(ctx context.Context, op *opsv1alpha1.Operation) error {
	return nil
}

func parseHelmParameters(op *opsv1alpha1.Operation) (helmParameters, error) {
	params := helmParameters{}
	if op.Spec.Parameters == nil {
		return params, nil
	}
	if err := json.Unmarshal(op.Spec.Parameters.Raw, &params); err != nil {
		return params, fmt.Errorf("decode helm parameters: %w", err)
	}
	return params, nil
}

var _ Engine = (*HelmEngine)(nil)

// Ensure metav1 import is used for future timestamp handling.
var _ = metav1.Now
