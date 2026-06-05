package engines

import (
	"context"
	"encoding/json"
	"fmt"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type veleroParameters struct {
	Retention              string            `json:"retention"`
	TTL                    string            `json:"ttl"`
	SnapshotClassName      string            `json:"snapshotClassName"`
	CSISnapshotClassName   string            `json:"csiSnapshotClassName"`
	SnapshotVolumes        *bool             `json:"snapshotVolumes"`
	StorageLocation        string            `json:"storageLocation"`
	IncludedResources      []string          `json:"includedResources"`
	ExcludedResources      []string          `json:"excludedResources"`
	BackupName             string            `json:"backupName"`
	NamespaceMapping       map[string]string `json:"namespaceMapping"`
	RestorePVs             *bool             `json:"restorePVs"`
	ExistingResourcePolicy string            `json:"existingResourcePolicy"`
}

func (p veleroParameters) backupTTL() string {
	if p.Retention != "" {
		return p.Retention
	}
	return p.TTL
}

func (p veleroParameters) csiSnapshotClassName() string {
	if p.CSISnapshotClassName != "" {
		return p.CSISnapshotClassName
	}
	return p.SnapshotClassName
}

const DefaultVeleroNamespace = "velero"

// VeleroEngine materializes velero Backup and Restore resources.
type VeleroEngine struct {
	Client    client.Client
	Namespace string
}

func NewVeleroEngine(c client.Client) *VeleroEngine {
	return NewVeleroEngineWithNamespace(c, DefaultVeleroNamespace)
}

func NewVeleroEngineWithNamespace(c client.Client, namespace string) *VeleroEngine {
	if namespace == "" {
		namespace = DefaultVeleroNamespace
	}
	return &VeleroEngine{Client: c, Namespace: namespace}
}

func (e *VeleroEngine) veleroNamespace() string {
	if e.Namespace == "" {
		return DefaultVeleroNamespace
	}
	return e.Namespace
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
	includedNS := targetNamespace(target)
	backup := &unstructured.Unstructured{}
	backup.SetGroupVersionKind(schema.GroupVersionKind{Group: "velero.io", Version: "v1", Kind: "Backup"})
	backup.SetName(op.Name)
	backup.SetNamespace(e.veleroNamespace())
	_, err := controllerutil.CreateOrUpdate(ctx, e.Client, backup, func() error {
		objectMeta := metav1.ObjectMeta{Labels: backup.GetLabels()}
		applyOperationLabels(&objectMeta, op)
		backup.SetLabels(objectMeta.Labels)
		if op.Namespace == backup.GetNamespace() {
			if err := controllerutil.SetOwnerReference(op, backup, e.Client.Scheme()); err != nil {
				return err
			}
		}
		if err := unstructured.SetNestedStringSlice(backup.Object, []string{includedNS}, "spec", "includedNamespaces"); err != nil {
			return err
		}
		storageLocation := params.StorageLocation
		if storageLocation == "" {
			storageLocation = "default"
		}
		if err := unstructured.SetNestedField(backup.Object, storageLocation, "spec", "storageLocation"); err != nil {
			return err
		}
		if params.SnapshotVolumes != nil {
			if err := unstructured.SetNestedField(backup.Object, *params.SnapshotVolumes, "spec", "snapshotVolumes"); err != nil {
				return err
			}
		}
		if csiClass := params.csiSnapshotClassName(); csiClass != "" {
			if err := unstructured.SetNestedField(backup.Object, csiClass, "spec", "csiSnapshotClassName"); err != nil {
				return err
			}
		}
		if len(params.IncludedResources) > 0 {
			if err := unstructured.SetNestedStringSlice(backup.Object, params.IncludedResources, "spec", "includedResources"); err != nil {
				return err
			}
		}
		if len(params.ExcludedResources) > 0 {
			if err := unstructured.SetNestedStringSlice(backup.Object, params.ExcludedResources, "spec", "excludedResources"); err != nil {
				return err
			}
		}
		if ttl := params.backupTTL(); ttl != "" {
			if err := unstructured.SetNestedField(backup.Object, ttl, "spec", "ttl"); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func (e *VeleroEngine) materializeRestore(ctx context.Context, op *opsv1alpha1.Operation, target *appsv1alpha1.ApplicationInstance, params veleroParameters) error {
	_ = target // reserved for future namespaceMapping defaults from the app release namespace
	if params.BackupName == "" {
		return fmt.Errorf("parameters.backupName is required for restore operations")
	}
	restore := &unstructured.Unstructured{}
	restore.SetGroupVersionKind(schema.GroupVersionKind{Group: "velero.io", Version: "v1", Kind: "Restore"})
	restore.SetName(op.Name)
	restore.SetNamespace(e.veleroNamespace())
	_, err := controllerutil.CreateOrUpdate(ctx, e.Client, restore, func() error {
		objectMeta := metav1.ObjectMeta{Labels: restore.GetLabels()}
		applyOperationLabels(&objectMeta, op)
		restore.SetLabels(objectMeta.Labels)
		if op.Namespace == restore.GetNamespace() {
			if err := controllerutil.SetOwnerReference(op, restore, e.Client.Scheme()); err != nil {
				return err
			}
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
		if params.ExistingResourcePolicy != "" {
			if err := unstructured.SetNestedField(restore.Object, params.ExistingResourcePolicy, "spec", "existingResourcePolicy"); err != nil {
				return err
			}
		}
		if len(params.IncludedResources) > 0 {
			if err := unstructured.SetNestedStringSlice(restore.Object, params.IncludedResources, "spec", "includedResources"); err != nil {
				return err
			}
		}
		if len(params.ExcludedResources) > 0 {
			if err := unstructured.SetNestedStringSlice(restore.Object, params.ExcludedResources, "spec", "excludedResources"); err != nil {
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
	if err := e.Client.Get(ctx, client.ObjectKey{Namespace: e.veleroNamespace(), Name: op.Name}, obj); err != nil {
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
	kind := "Backup"
	if op.Spec.Type == opsv1alpha1.OperationTypeRestore {
		kind = "Restore"
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "velero.io", Version: "v1", Kind: kind})
	obj.SetName(op.Name)
	obj.SetNamespace(e.veleroNamespace())
	if err := e.Client.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete velero %s %s/%s: %w", kind, e.veleroNamespace(), op.Name, err)
	}
	if err := deleteVeleroCRsLabeledByOperation(ctx, e.Client, op, kind); err != nil {
		return err
	}
	// Legacy backups created in the Operation namespace before the velero-namespace fix.
	legacy := obj.DeepCopy()
	legacy.SetNamespace(op.Namespace)
	if err := e.Client.Delete(ctx, legacy); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete legacy velero %s %s/%s: %w", kind, op.Namespace, op.Name, err)
	}
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

func toStringMap(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func deleteVeleroCRsLabeledByOperation(ctx context.Context, c client.Client, op *opsv1alpha1.Operation, kind string) error {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{Group: "velero.io", Version: "v1", Kind: kind + "List"})
	if err := c.List(ctx, list, client.MatchingLabels{OperationLabelKey: string(op.UID)}); err != nil {
		return fmt.Errorf("list velero %ss for operation %s: %w", kind, op.UID, err)
	}
	for i := range list.Items {
		item := &list.Items[i]
		if err := c.Delete(ctx, item); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete velero %s %s/%s: %w", kind, item.GetNamespace(), item.GetName(), err)
		}
	}
	return nil
}

var _ Engine = (*VeleroEngine)(nil)

var _ = metav1.Now
