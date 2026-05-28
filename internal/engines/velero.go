package engines

import (
	"context"
	"encoding/json"
	"fmt"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type veleroParameters struct {
	Retention         string            `json:"retention"`
	SnapshotClassName string            `json:"snapshotClassName"`
	StorageLocation   string            `json:"storageLocation"`
	BackupName        string            `json:"backupName"`
	NamespaceMapping  map[string]string `json:"namespaceMapping"`
	RestorePVs        *bool             `json:"restorePVs"`
}

// VeleroEngine materializes velero Backup and Restore resources.
type VeleroEngine struct {
	Client client.Client
}

func NewVeleroEngine(c client.Client) *VeleroEngine {
	return &VeleroEngine{Client: c}
}

func (e *VeleroEngine) Name() opsv1alpha1.OperationEngine {
	return opsv1alpha1.EngineVelero
}

func (e *VeleroEngine) Materialize(ctx context.Context, op *opsv1alpha1.Operation, target *appsv1alpha1.ApplicationInstance) error {
	params, err := parseVeleroParameters(op)
	if err != nil {
		return err
	}
	switch op.Spec.Type {
	case opsv1alpha1.OperationTypeBackup:
		return e.materializeBackup(ctx, op, target, params)
	case opsv1alpha1.OperationTypeRestore:
		return e.materializeRestore(ctx, op, target, params)
	default:
		return fmt.Errorf("velero engine does not support operation type %q", op.Spec.Type)
	}
}

func (e *VeleroEngine) materializeBackup(ctx context.Context, op *opsv1alpha1.Operation, target *appsv1alpha1.ApplicationInstance, params veleroParameters) error {
	backup := &unstructured.Unstructured{}
	backup.SetGroupVersionKind(schema.GroupVersionKind{Group: "velero.io", Version: "v1", Kind: "Backup"})
	backup.SetName(op.Name)
	backup.SetNamespace(target.Namespace)
	_, err := controllerutil.CreateOrUpdate(ctx, e.Client, backup, func() error {
		if err := controllerutil.SetOwnerReference(op, backup, e.Client.Scheme()); err != nil {
			return err
		}
		if err := unstructured.SetNestedStringSlice(backup.Object, []string{target.Namespace}, "spec", "includedNamespaces"); err != nil {
			return err
		}
		if params.StorageLocation != "" {
			if err := unstructured.SetNestedField(backup.Object, params.StorageLocation, "spec", "storageLocation"); err != nil {
				return err
			}
		}
		if params.SnapshotClassName != "" {
			if err := unstructured.SetNestedField(backup.Object, params.SnapshotClassName, "spec", "snapshotVolumes", "snapshotClassName"); err != nil {
				return err
			}
		}
		if params.Retention != "" {
			if err := unstructured.SetNestedField(backup.Object, params.Retention, "spec", "ttl"); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func (e *VeleroEngine) materializeRestore(ctx context.Context, op *opsv1alpha1.Operation, target *appsv1alpha1.ApplicationInstance, params veleroParameters) error {
	if params.BackupName == "" {
		return fmt.Errorf("parameters.backupName is required for restore operations")
	}
	restore := &unstructured.Unstructured{}
	restore.SetGroupVersionKind(schema.GroupVersionKind{Group: "velero.io", Version: "v1", Kind: "Restore"})
	restore.SetName(op.Name)
	restore.SetNamespace(target.Namespace)
	_, err := controllerutil.CreateOrUpdate(ctx, e.Client, restore, func() error {
		if err := controllerutil.SetOwnerReference(op, restore, e.Client.Scheme()); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(restore.Object, params.BackupName, "spec", "backupName"); err != nil {
			return err
		}
		if params.NamespaceMapping != nil {
			if err := unstructured.SetNestedMap(restore.Object, toStringMap(params.NamespaceMapping), "spec", "namespaceMapping"); err != nil {
				return err
			}
		}
		if params.RestorePVs != nil {
			if err := unstructured.SetNestedField(restore.Object, *params.RestorePVs, "spec", "restorePVs"); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func (e *VeleroEngine) Status(ctx context.Context, op *opsv1alpha1.Operation) (Status, error) {
	gvk := schema.GroupVersionKind{Group: "velero.io", Version: "v1", Kind: "Backup"}
	if op.Spec.Type == opsv1alpha1.OperationTypeRestore {
		gvk.Kind = "Restore"
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	if err := e.Client.Get(ctx, client.ObjectKey{Namespace: op.Namespace, Name: op.Name}, obj); err != nil {
		return Status{}, fmt.Errorf("get velero resource: %w", err)
	}
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	switch phase {
	case "Completed":
		return Status{
			Phase:   opsv1alpha1.PhaseSucceeded,
			Reason:  "EngineCompleted",
			Done:    true,
			Outputs: map[string]string{outputKey(op.Spec.Type): op.Name},
		}, nil
	case "Failed", "PartiallyFailed":
		message, _, _ := unstructured.NestedString(obj.Object, "status", "failureReason")
		return Status{
			Phase:   opsv1alpha1.PhaseFailed,
			Reason:  "EngineFailed",
			Message: message,
			Done:    true,
			Failed:  true,
		}, nil
	default:
		return Status{
			Phase:  opsv1alpha1.PhaseRunning,
			Reason: "EngineStarted",
		}, nil
	}
}

func (e *VeleroEngine) Cancel(ctx context.Context, op *opsv1alpha1.Operation) error {
	return nil
}

func parseVeleroParameters(op *opsv1alpha1.Operation) (veleroParameters, error) {
	params := veleroParameters{}
	if op.Spec.Parameters == nil {
		return params, nil
	}
	if err := json.Unmarshal(op.Spec.Parameters.Raw, &params); err != nil {
		return params, fmt.Errorf("decode velero parameters: %w", err)
	}
	return params, nil
}

func outputKey(opType opsv1alpha1.OperationType) string {
	if opType == opsv1alpha1.OperationTypeRestore {
		return "restoreName"
	}
	return "backupName"
}

func toStringMap(in map[string]string) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

var _ Engine = (*VeleroEngine)(nil)

var _ = metav1.Now
