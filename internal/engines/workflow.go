package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type workflowParameters struct {
	WorkflowTemplate           string            `json:"workflowTemplate"`
	Template                   string            `json:"template"`
	WorkflowTemplateParameters map[string]string `json:"workflowTemplateParameters"`
	ServiceAccountName         string            `json:"serviceAccountName"`
}

// WorkflowEngine materializes argoproj.io Workflow resources.
type WorkflowEngine struct {
	Client client.Client
}

func NewWorkflowEngine(c client.Client) *WorkflowEngine {
	return &WorkflowEngine{Client: c}
}

func (e *WorkflowEngine) Name() opsv1alpha1.OperationEngine {
	return opsv1alpha1.EngineWorkflow
}

func (e *WorkflowEngine) Materialize(ctx context.Context, op *opsv1alpha1.Operation, target *appsv1alpha1.ApplicationInstance) error {
	params, err := parseWorkflowParameters(op)
	if err != nil {
		return err
	}
	templateName := params.templateName()
	if templateName == "" {
		return fmt.Errorf("parameters.workflowTemplate is required")
	}

	ns := targetNamespace(target)
	name := materializedName(op)
	wf := &unstructured.Unstructured{}
	wf.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Workflow"})
	wf.SetName(name)
	wf.SetNamespace(ns)
	wfMeta := metav1.ObjectMeta{Name: name, Namespace: ns}
	applyOperationLabels(&wfMeta, op)
	wf.SetLabels(wfMeta.Labels)

	var existing unstructured.Unstructured
	existing.SetGroupVersionKind(wf.GroupVersionKind())
	if err := e.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, &existing); err == nil {
		if err := verifyOperationOwnership(&metav1.ObjectMeta{Labels: existing.GetLabels()}, op); err != nil {
			return err
		}
		return nil
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get workflow: %w", err)
	}

	if err := setOwnerReferenceIfSameNamespace(op, wf, ns, e.Client.Scheme()); err != nil {
		return err
	}
	if err := unstructured.SetNestedMap(wf.Object, map[string]any{"name": templateName}, "spec", "workflowTemplateRef"); err != nil {
		return err
	}
	if sa := params.serviceAccountName(); sa != "" {
		if err := unstructured.SetNestedField(wf.Object, sa, "spec", "serviceAccountName"); err != nil {
			return err
		}
	}
	if len(params.WorkflowTemplateParameters) > 0 {
		paramNames := make([]string, 0, len(params.WorkflowTemplateParameters))
		for paramName := range params.WorkflowTemplateParameters {
			paramNames = append(paramNames, paramName)
		}
		sort.Strings(paramNames)
		args := make([]any, 0, len(paramNames))
		for _, paramName := range paramNames {
			args = append(args, map[string]any{"name": paramName, "value": params.WorkflowTemplateParameters[paramName]})
		}
		if err := unstructured.SetNestedSlice(wf.Object, args, "spec", "arguments", "parameters"); err != nil {
			return err
		}
	}
	if err := e.Client.Create(ctx, wf); err != nil {
		return fmt.Errorf("create workflow: %w", err)
	}
	return nil
}

func (e *WorkflowEngine) Status(ctx context.Context, op *opsv1alpha1.Operation) (Status, error) {
	ns, name, err := resolveEngineLocationForStatus(ctx, e.Client, op, findWorkflowLocationByOperationLabel)
	if err != nil {
		return Status{}, err
	}
	wf := &unstructured.Unstructured{}
	wf.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Workflow"})
	if err := e.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, wf); err != nil {
		return Status{}, fmt.Errorf("get workflow: %w", err)
	}
	if err := verifyOperationOwnership(&metav1.ObjectMeta{Labels: wf.GetLabels()}, op); err != nil {
		return Status{}, err
	}
	phase, _, _ := unstructured.NestedString(wf.Object, "status", "phase")
	switch phase {
	case "Succeeded":
		return Status{
			Phase:   opsv1alpha1.PhaseSucceeded,
			Reason:  "EngineCompleted",
			Done:    true,
			Outputs: map[string]string{"workflowName": wf.GetName()},
		}, nil
	case "Failed", "Error", "Terminated", "Skipped":
		message, _, _ := unstructured.NestedString(wf.Object, "status", "message")
		return Status{
			Phase:   opsv1alpha1.PhaseFailed,
			Reason:  "EngineFailed",
			Message: message,
			Done:    true,
			Failed:  true,
			Outputs: map[string]string{"workflowName": wf.GetName()},
		}, nil
	default:
		return Status{
			Phase:   opsv1alpha1.PhaseRunning,
			Reason:  "EngineStarted",
			Outputs: map[string]string{"workflowName": wf.GetName()},
		}, nil
	}
}

func (e *WorkflowEngine) Cancel(ctx context.Context, op *opsv1alpha1.Operation) error {
	ns, name, skip, err := resolveEngineLocationForCancel(ctx, e.Client, op)
	if err != nil {
		return err
	}
	if skip {
		return deleteWorkflowsLabeledByOperation(ctx, e.Client, op)
	}
	wf := &unstructured.Unstructured{}
	wf.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Workflow"})
	if err := e.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, wf); err != nil {
		return client.IgnoreNotFound(err)
	}
	return e.Client.Delete(ctx, wf)
}

func parseWorkflowParameters(op *opsv1alpha1.Operation) (workflowParameters, error) {
	params := workflowParameters{}
	if op.Spec.Parameters == nil {
		return params, nil
	}
	if err := json.Unmarshal(op.Spec.Parameters.Raw, &params); err != nil {
		return params, fmt.Errorf("decode workflow parameters: %w", err)
	}
	return params, nil
}

func (p workflowParameters) templateName() string {
	if p.WorkflowTemplate != "" {
		return p.WorkflowTemplate
	}
	return p.Template
}

func (p workflowParameters) serviceAccountName() string {
	if p.ServiceAccountName != "" {
		return p.ServiceAccountName
	}
	return defaultJobServiceAccount
}

var _ Engine = (*WorkflowEngine)(nil)
