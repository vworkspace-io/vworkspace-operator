package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultJobServiceAccount = "vworkspace-operation-runner"

type jobParameters struct {
	Image                   string            `json:"image"`
	Command                 []string          `json:"command"`
	Args                    []string          `json:"args"`
	Env                     map[string]string `json:"env"`
	ServiceAccountName      string            `json:"serviceAccountName"`
	BackoffLimit            *int32            `json:"backoffLimit"`
	ActiveDeadlineSeconds   *int64            `json:"activeDeadlineSeconds"`
	TTLSecondsAfterFinished *int32            `json:"ttlSecondsAfterFinished"`
}

// JobEngine materializes batch/v1 Jobs for RunCommand operations.
type JobEngine struct {
	Client client.Client
}

func NewJobEngine(c client.Client) *JobEngine {
	return &JobEngine{Client: c}
}

func (e *JobEngine) Name() opsv1alpha1.OperationEngine {
	return opsv1alpha1.EngineJob
}

func (e *JobEngine) Materialize(ctx context.Context, op *opsv1alpha1.Operation, target *appsv1alpha1.ApplicationInstance) error {
	params, err := parseJobParameters(op)
	if err != nil {
		return err
	}
	if params.Image == "" {
		return fmt.Errorf("parameters.image is required")
	}

	ns := targetNamespace(target)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns},
		Spec: batchv1.JobSpec{
			BackoffLimit:            params.BackoffLimit,
			ActiveDeadlineSeconds:   params.ActiveDeadlineSeconds,
			TTLSecondsAfterFinished: params.ttlSecondsAfterFinished(),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: params.serviceAccountName(),
					Containers: []corev1.Container{{
						Name:    "runner",
						Image:   params.Image,
						Command: params.Command,
						Args:    params.Args,
						Env:     envFromMap(params.Env),
					}},
				},
			},
		},
	}
	applyOperationLabels(&job.ObjectMeta, op)
	applyOperationLabels(&job.Spec.Template.ObjectMeta, op)

	return createMaterializedJob(ctx, e.Client, op, job, ns)
}

func (e *JobEngine) Status(ctx context.Context, op *opsv1alpha1.Operation) (Status, error) {
	ns, name, err := resolveEngineLocation(ctx, e.Client, op)
	if err != nil {
		return Status{}, err
	}
	job := &batchv1.Job{}
	if err := e.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, job); err != nil {
		return Status{}, fmt.Errorf("get job: %w", err)
	}
	if err := verifyOperationOwnership(&job.ObjectMeta, op); err != nil {
		return Status{}, err
	}
	return statusFromJob(job), nil
}

func (e *JobEngine) Cancel(ctx context.Context, op *opsv1alpha1.Operation) error {
	ns, name, err := resolveEngineLocation(ctx, e.Client, op)
	if err != nil {
		return err
	}
	job := &batchv1.Job{}
	if err := e.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, job); err != nil {
		return client.IgnoreNotFound(err)
	}
	return e.Client.Delete(ctx, job)
}

func parseJobParameters(op *opsv1alpha1.Operation) (jobParameters, error) {
	params := jobParameters{}
	if op.Spec.Parameters == nil {
		return params, nil
	}
	if err := json.Unmarshal(op.Spec.Parameters.Raw, &params); err != nil {
		return params, fmt.Errorf("decode job parameters: %w", err)
	}
	return params, nil
}

func (p jobParameters) serviceAccountName() string {
	if p.ServiceAccountName != "" {
		return p.ServiceAccountName
	}
	return defaultJobServiceAccount
}

func (p jobParameters) ttlSecondsAfterFinished() *int32 {
	if p.TTLSecondsAfterFinished != nil {
		return p.TTLSecondsAfterFinished
	}
	ttl := int32(86400)
	return &ttl
}

func envFromMap(env map[string]string) []corev1.EnvVar {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]corev1.EnvVar, 0, len(keys))
	for _, k := range keys {
		out = append(out, corev1.EnvVar{Name: k, Value: env[k]})
	}
	return out
}

func statusFromJob(job *batchv1.Job) Status {
	for _, cond := range job.Status.Conditions {
		switch cond.Type {
		case batchv1.JobComplete:
			if cond.Status == corev1.ConditionTrue {
				return Status{
					Phase:   opsv1alpha1.PhaseSucceeded,
					Reason:  "EngineCompleted",
					Done:    true,
					Outputs: map[string]string{"jobName": job.Name},
				}
			}
		case batchv1.JobFailed:
			if cond.Status == corev1.ConditionTrue {
				return Status{
					Phase:   opsv1alpha1.PhaseFailed,
					Reason:  "EngineFailed",
					Message: cond.Message,
					Done:    true,
					Failed:  true,
					Outputs: map[string]string{"jobName": job.Name},
				}
			}
		}
	}
	return Status{Phase: opsv1alpha1.PhaseRunning, Reason: "EngineStarted"}
}

func targetNamespace(target *appsv1alpha1.ApplicationInstance) string {
	if target.Spec.Release.Namespace != "" {
		return target.Spec.Release.Namespace
	}
	return target.Namespace
}

var _ Engine = (*JobEngine)(nil)
