# Glossary

**Last Updated:** 2026-05-28

The terms used across the `vworkspace-operator` documentation, in alphabetical order. Cross-links point at the page where the term is treated in more depth.

## ApplicationInstance

The CRD that represents the intent "this application should exist in this namespace, deployed from this chart with these parameters." Group/version/kind: `apps.vworkspace.io/v1alpha1` `ApplicationInstance`. The operator reconciles it into a Flux `HelmRelease` (and the matching chart-source resource) and aggregates status back into the standard condition vocabulary. Full reference in [../api/application-instance.md](../api/application-instance.md).

## Capability

A named day-2 action an application supports, declared as a `ops.vworkspace.io/<capability>=<engine>` annotation on an `ApplicationInstance`. Examples: `ops.vworkspace.io/backup=velero`, `ops.vworkspace.io/upgrade=helm`. Capabilities are populated from the Odoo catalog entry, not inferred from the chart. See [day-2-operations.md](day-2-operations.md) and [../api/labels-and-annotations.md](../api/labels-and-annotations.md).

## Cluster Identity

The stable identity record Odoo holds for each cluster: a unique ID, a display name, the owning organization, an optional public key for signed payloads, the allowed namespaces, the allowed catalog entries, and the allowed operation engines. The cluster holds the matching outbound credential. Each request from the operator to Odoo carries the cluster identity and is authorized against this record server-side. See [multi-cluster-and-multi-org.md](multi-cluster-and-multi-org.md).

## Day-2 Operation

Any action against a running application other than the initial install: backup, restore, upgrade, migration, rollback, configuration change with a maintenance window, or runbook. Day-2 operations are modeled as `Operation` resources rather than as imperative scripts. See [day-2-operations.md](day-2-operations.md).

## Engine

The executor a given `Operation` dispatches to. One of `velero`, `workflow`, `job`, `helm`, `volsync`, or `helmHookJob`. The operator picks the engine from `Operation.spec.engine`, creates the matching downstream resource, and watches for completion. See [day-2-operations.md](day-2-operations.md#the-five-execution-patterns) and [../api/operation.md](../api/operation.md).

## Flux HelmRelease

The custom resource that Flux Helm Controller reconciles. The operator materializes a `HelmRelease` from each `ApplicationInstance`, and Flux owns the Helm lifecycle from there: chart fetch, install/upgrade, drift remediation, rollback. See [helm-first.md](helm-first.md) and [reconciliation-model.md](reconciliation-model.md#applicationinstance--helmrelease).

## GitOps Mode

The connectivity mode in which Odoo renders the cluster's desired state as a set of manifests, commits them to a Git repository, and the cluster's Flux or Argo CD pulls them. The CRDs and the in-cluster reconciliation loop are the same as in the other modes; only the transport changes. See [../connectivity/gitops-mode.md](../connectivity/gitops-mode.md).

## Helm Engine

The component that actually performs Helm reconciliation. Flux Helm Controller is the default; an Argo CD `Application` adapter is supported as an alternative. See [helm-first.md](helm-first.md#the-three-helm-engine-options).

## Intent

The "what should exist" declaration produced by Odoo and consumed by the operator. Intent is expressed as either a rendered Kubernetes object (an `ApplicationInstance` or `Operation` manifest) or as a higher-level structured record (`ensure-application-instance`, `request-operation`, `delete-application-instance`, ...). Both shapes converge in the cluster as the same CRDs. See [../connectivity/pull-mode.md](../connectivity/pull-mode.md#payload-shapes).

## Job (pull-mode sense)

In Pull mode, a unit of work the operator fetches from Odoo's `/api/agent/jobs` endpoint. A Pull-mode job has an ID, a kind (`apply`, `delete`, `intent`), a payload, an idempotency key, and lifecycle endpoints (`ack`, `status`, `result`). It is distinct from a Kubernetes `Job`, which is a workload resource. Context disambiguates. See [../connectivity/job-protocol.md](../connectivity/job-protocol.md).

## Operation

The CRD that represents a day-2 action against a target `ApplicationInstance`. Group/version/kind: `ops.vworkspace.io/v1alpha1` `Operation`. The operator dispatches it to one of six engines and aggregates the engine's status back into the operation's conditions and outputs. Full reference in [../api/operation.md](../api/operation.md).

## Operation Template

A reusable recipe in the Odoo catalog that declares, for a given application and verb, which engine to use, which input schema applies, which RBAC profile is required, and which preconditions and target selectors apply. The operator does not store templates itself; it receives a fully-instantiated `Operation` resource and only enforces admission rules. See [day-2-operations.md](day-2-operations.md#templated-workflows-not-bespoke-code).

## Operator

The Kubernetes operator process — `vworkspace-app-operator` — running once per cluster. It owns the `ApplicationInstance` and `Operation` CRDs, drives the connectivity loop to Odoo, and aggregates status. In other contexts ("the operator of the platform") the same word means "the human running a vWorkspace install"; the documentation is explicit when that ambiguity matters.

## Pull Mode

The default connectivity mode: the operator initiates an outbound connection to Odoo, fetches jobs targeted at its cluster identity, applies them to its own API server, and reports status back over the same outbound channel. No inbound ports are required on the cluster network; Odoo does not hold a kubeconfig. See [../connectivity/pull-mode.md](../connectivity/pull-mode.md).

## Push Mode

The connectivity mode in which Odoo authenticates to the cluster's Kubernetes API directly and writes `ApplicationInstance`/`Operation` resources via server-side apply. The simplest choice when Odoo and the cluster share a trusted network — most commonly when Odoo is installed into the cluster it manages. See [../connectivity/push-mode.md](../connectivity/push-mode.md).
