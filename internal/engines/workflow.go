package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/labels"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
	wfLabels := wf.GetLabels()
	if wfLabels == nil {
		wfLabels = map[string]string{}
	}
	wfLabels[labels.ManagedByKey] = labels.ManagedByOperator
	wfLabels[OperationLabelKey] = string(op.UID)
	wf.SetLabels(wfLabels)

	_, err = controllerutil.CreateOrUpdate(ctx, e.Client, wf, func() error {
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
			names := make([]string, 0, len(params.WorkflowTemplateParameters))
			for name := range params.WorkflowTemplateParameters {
				names = append(names, name)
			}
			sort.Strings(names)
			args := make([]any, 0, len(names))
			for _, name := range names {
				args = append(args, map[string]any{"name": name, "value": params.WorkflowTemplateParameters[name]})
			}
			if err := unstructured.SetNestedSlice(wf.Object, args, "spec", "arguments", "parameters"); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func (e *WorkflowEngine) Status(ctx context.Context, op *opsv1alpha1.Operation) (Status, error) {
	ns, name, err := resolveEngineLocation(ctx, e.Client, op)
	if err != nil {
		return Status{}, err
	}
	wf := &unstructured.Unstructured{}
	wf.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Workflow"})
	if err := e.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, wf); err != nil {
		return Status{}, fmt.Errorf("get workflow: %w", err)
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
	ns, name, err := resolveEngineLocation(ctx, e.Client, op)
	if err != nil {
		return err
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
