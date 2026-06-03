package engines

import (
	"context"
	"encoding/json"
	"fmt"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const helmHookNameLabel = "ops.vworkspace.io/hook-name"

type helmHookJobParameters struct {
	HookName                string   `json:"hookName"`
	Image                   string   `json:"image"`
	Command                 []string `json:"command"`
	Args                    []string `json:"args"`
	ServiceAccountName      string   `json:"serviceAccountName"`
	BackoffLimit            *int32   `json:"backoffLimit"`
	ActiveDeadlineSeconds   *int64   `json:"activeDeadlineSeconds"`
	TTLSecondsAfterFinished *int32   `json:"ttlSecondsAfterFinished"`
}

// HelmHookJobEngine materializes chart hook Jobs on demand.
// Full hook-template resolution from HelmRelease manifests is deferred; callers
// must supply image/command until manifest discovery lands in a follow-up.
type HelmHookJobEngine struct {
	Client client.Client
}

func NewHelmHookJobEngine(c client.Client) *HelmHookJobEngine {
	return &HelmHookJobEngine{Client: c}
}

func (e *HelmHookJobEngine) Name() opsv1alpha1.OperationEngine {
	return opsv1alpha1.EngineHelmHookJob
}

func (e *HelmHookJobEngine) Materialize(ctx context.Context, op *opsv1alpha1.Operation, target *appsv1alpha1.ApplicationInstance) error {
	params, err := parseHelmHookJobParameters(op)
	if err != nil {
		return err
	}
	if params.HookName == "" {
		return fmt.Errorf("parameters.hookName is required")
	}
	if params.Image == "" {
		return fmt.Errorf("parameters.image is required until hook template resolution is implemented")
	}

	ns := targetNamespace(target)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      op.Name,
			Namespace: ns,
			Labels:    map[string]string{helmHookNameLabel: params.HookName},
			Annotations: map[string]string{
				"helm.sh/hook": params.HookName,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            params.BackoffLimit,
			ActiveDeadlineSeconds:   params.ActiveDeadlineSeconds,
			TTLSecondsAfterFinished: params.ttlSecondsAfterFinished(),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{helmHookNameLabel: params.HookName},
				},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: params.serviceAccountName(),
					Containers: []corev1.Container{{
						Name:    "hook",
						Image:   params.Image,
						Command: params.Command,
						Args:    params.Args,
					}},
				},
			},
		},
	}
	applyOperationLabels(&job.ObjectMeta, op)
	applyOperationLabels(&job.Spec.Template.ObjectMeta, op)

	_, err = controllerutil.CreateOrUpdate(ctx, e.Client, job, func() error {
		if err := setOwnerReferenceIfSameNamespace(op, job, ns, e.Client.Scheme()); err != nil {
			return err
		}
		if job.Labels == nil {
			job.Labels = map[string]string{}
		}
		job.Labels[helmHookNameLabel] = params.HookName
		if job.Annotations == nil {
			job.Annotations = map[string]string{}
		}
		job.Annotations["helm.sh/hook"] = params.HookName
		if job.Spec.Template.Labels == nil {
			job.Spec.Template.Labels = map[string]string{}
		}
		job.Spec.Template.Labels[helmHookNameLabel] = params.HookName
		applyOperationLabels(&job.Spec.Template.ObjectMeta, op)
		job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
		job.Spec.Template.Spec.ServiceAccountName = params.serviceAccountName()
		job.Spec.Template.Spec.Containers = []corev1.Container{{
			Name:    "hook",
			Image:   params.Image,
			Command: params.Command,
			Args:    params.Args,
		}}
		return nil
	})
	return err
}

func (e *HelmHookJobEngine) Status(ctx context.Context, op *opsv1alpha1.Operation) (Status, error) {
	ns, err := materializedNamespace(ctx, e.Client, op)
	if err != nil {
		return Status{}, err
	}
	job := &batchv1.Job{}
	if err := e.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: op.Name}, job); err != nil {
		return Status{}, fmt.Errorf("get hook job: %w", err)
	}
	status := statusFromJob(job)
	if status.Outputs == nil {
		status.Outputs = map[string]string{}
	}
	status.Outputs["jobName"] = job.Name
	return status, nil
}

func (e *HelmHookJobEngine) Cancel(ctx context.Context, op *opsv1alpha1.Operation) error {
	ns, err := materializedNamespace(ctx, e.Client, op)
	if err != nil {
		return err
	}
	job := &batchv1.Job{}
	if err := e.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: op.Name}, job); err != nil {
		return client.IgnoreNotFound(err)
	}
	return e.Client.Delete(ctx, job)
}

func parseHelmHookJobParameters(op *opsv1alpha1.Operation) (helmHookJobParameters, error) {
	params := helmHookJobParameters{}
	if op.Spec.Parameters == nil {
		return params, nil
	}
	if err := json.Unmarshal(op.Spec.Parameters.Raw, &params); err != nil {
		return params, fmt.Errorf("decode helmHookJob parameters: %w", err)
	}
	return params, nil
}

func (p helmHookJobParameters) serviceAccountName() string {
	if p.ServiceAccountName != "" {
		return p.ServiceAccountName
	}
	return defaultJobServiceAccount
}

func (p helmHookJobParameters) ttlSecondsAfterFinished() *int32 {
	if p.TTLSecondsAfterFinished != nil {
		return p.TTLSecondsAfterFinished
	}
	ttl := int32(86400)
	return &ttl
}

var _ Engine = (*HelmHookJobEngine)(nil)
