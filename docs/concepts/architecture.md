# Architecture

**Status:** Alpha
**Last Updated:** 2026-05-30

This page describes how the major components of `vworkspace-operator` fit together, why the project commits to "one operator per cluster", and how intent flows from the control plane into a cluster and back. For the protocol-level detail of how intent reaches the cluster in each connectivity mode, see [../connectivity/README.md](../connectivity/README.md). For the spec and status fields of the CRDs the operator owns, see [../api/README.md](../api/README.md).

## High-level picture

```
+---------------------------------------------------------------------+
| ODOO (single install, owner-controlled)                             |
| - App catalog + policy                                              |
| - ApplicationInstance / Operation intent (desired state)            |
| - Cluster registry + per-cluster identity                           |
| - Audit log + Discuss surface for the AI assistant                  |
+----------------------------------+----------------------------------+
                                   |
                Push / Pull / GitOps (see connectivity/)
                                   |
+----------------------------------v----------------------------------+
| Cluster A                                                            |
|  +-----------------------------+    +----------------------------+   |
|  | vworkspace-app-operator     |    | Flux Helm Controller       |   |
|  | - reconciles ApplicationIns.|    | - reconciles HelmRelease   |   |
|  | - creates HelmRelease       |    | - upgrade / rollback       |   |
|  | - reconciles Operation CRs  |    +----------------------------+   |
|  +--------------+--------------+                                     |
|                 |                                                    |
|                 v                                                    |
|       Chart renders K8s objects                                      |
|     (Deployments, Services, Ingresses, ...)                          |
|                                                                      |
| Ops controllers: Velero, CSI snapshots, Argo Workflows, Jobs         |
+---------------------------------------------------------------------+

+---------------------------------------------------------------------+
| Cluster B (same pattern, isolated blast radius)                     |
+---------------------------------------------------------------------+
```

The same picture, in higher resolution and with the connectivity arrows broken out, lives in [../diagrams/architecture.txt](../diagrams/architecture.txt). The Pull-mode sequence for an apply is in [../diagrams/pull-mode-sequence.txt](../diagrams/pull-mode-sequence.txt).

## Components

### Odoo (control plane)

Odoo holds the application catalog (which charts are allowed, which versions are recommended, which capability annotations they declare), the cluster registry (which clusters belong to which organization, what identity each one uses, which catalog entries it may run), the per-cluster identity material (one stable identity record per cluster), the request log (every `ApplicationInstance` or `Operation` requested by a human or the AI assistant), and the audit trail. It also hosts the Discuss surface where the AI assistant talks to the operator about the fleet. Odoo is **not** a reconciler. It records intent, exposes a job endpoint for Pull-mode clusters, accepts status events, and surfaces everything to humans and the AI.

### Per-cluster operator (`vworkspace-app-operator`)

A single Kubernetes operator installed once per cluster. It owns two CRD groups:

- `apps.vworkspace.io/v1alpha1` — currently `ApplicationInstance`.
- `ops.vworkspace.io/v1alpha1` — currently `Operation`.

It reconciles those CRDs by creating downstream resources (`HelmRelease`, `Backup`, `Workflow`, `Job`, `VolumeSnapshot`, ...) that third-party controllers act on. It also owns the connectivity loop back to the control plane — Pull, Push, or GitOps — and the cluster-level `Cluster` status object that reports connectivity, controller presence, and the last successful round-trip to the control plane.

The operator is multi-tenant inside the cluster: it serves all namespaces labeled `managed-by=vworkspace`. It is not multi-tenant across clusters; each cluster has its own operator instance with its own RBAC and its own identity. That property is the heart of the next section.

### Helm engine

[Flux Helm Controller](https://fluxcd.io) is the default. It reconciles `HelmRelease` resources the operator creates, handles upgrade and rollback semantics, runs drift remediation, and surfaces native conditions back. Source Controller pairs with it to fetch chart artifacts from OCI registries or Helm repositories. An Argo CD `Application` adapter is supported as an alternative; the rationale for the default is in [helm-first.md](helm-first.md).

### Ops controllers

The operator delegates the actual work of day-2 operations to specialized controllers:

- **Velero** for namespace-scoped backup and restore.
- **CSI snapshot controller** and **VolSync** for storage-centric backup and replication.
- **Argo Workflows** for multi-step operation DAGs.
- **cert-manager** for TLS issuance (driven from chart values, but the operator wires the right defaults).
- **external-secrets** for chart values that reference external secret stores.
- **The cluster's ingress controller** for north-south routing.

The operator does not run any of this work itself. It writes the appropriate input CRs, watches the appropriate output CRs and conditions, and aggregates the result into the `Operation` or `ApplicationInstance` status.

## Why one operator per cluster

A single operator instance per cluster is a deliberate design constraint, not an implementation accident. It buys four properties that matter for the kind of organizations vWorkspace is built for.

- **Bounded blast radius.** A bug, a compromise, or an upgrade gone wrong on cluster A cannot reach cluster B, because the operator on cluster B is a different process with different RBAC and a different identity. The cluster boundary is the trust boundary.
- **Independence from the control plane availability.** Once an `ApplicationInstance` is in the cluster's API server, the operator and Flux reconcile it without consulting Odoo. If Odoo is down for an hour, a day, or a week, applications keep running and recovering from disruptions. The cluster is offline-capable.
- **Local RBAC and namespace policy.** Each cluster owns its own RBAC. The operator's permissions are scoped to namespaces labeled `managed-by=vworkspace`. There is no central place that can grant a cluster more permissions than it has locally agreed to. The cluster admin keeps the keys.
- **A clear ownership story for upgrades.** The operator is itself a workload reconciled by Flux. Upgrading the operator on cluster A is just bumping its `HelmRelease`. Different clusters can be on different operator versions without coordination, within the documented version-skew window.

The cost is that the operator process must contain everything a cluster needs to do without round-tripping to Odoo: the CRD reconcilers, the connectivity loop, the status aggregator, the admission webhooks, the metrics surface. We accept that cost. It is small compared to the cost of getting any of the four properties above wrong.

## Control and data flow

The dataflow is intentionally one-directional in shape:

1. **Intent enters the cluster.** Odoo emits intent — either by Pull-mode jobs, by Push-mode server-side apply, or by writing manifests to a Git repository that Flux on the cluster syncs. Whatever the transport, the result inside the cluster is identical: an `ApplicationInstance` or `Operation` resource appears in the API server, owned by the operator's field manager and labeled with `app.vworkspace.io/managed-by=odoo` and `app.vworkspace.io/cluster-id=<id>`.
2. **The operator reconciles.** For an `ApplicationInstance`, the operator materializes the matching `HelmRelease` (and, where needed, the upstream `HelmRepository` or `OCIRepository`). For an `Operation`, the operator picks the right engine (`velero`, `workflow`, `job`, `helm`, `volsync`, `helmHookJob`) from the request and creates the matching CR. Both are server-side applies under the operator's own field manager.
3. **Third-party controllers do the work.** Flux upgrades the release. Velero takes the backup. Argo Workflows runs the DAG. The operator does not poll their state; it watches their CRDs and reacts when their conditions change.
4. **Status propagates up.** As downstream conditions change, the operator updates the `ApplicationInstance.status.conditions` or `Operation.status.conditions` accordingly, emits a Kubernetes `Event` on the source resource, and queues a batched event for the Pull-mode status endpoint (or pushes back to the control plane in Push mode, or annotates the Git commit in GitOps mode).
5. **Odoo learns about the outcome.** The audit channel in Odoo Discuss receives the event; the cluster registry view shows the updated conditions; the AI assistant can summarize what happened.

The key invariant is that **the in-cluster reconciliation loop is the same across all three connectivity modes**. Only step 1 — how intent arrives — differs. Steps 2 through 5 are the same code paths on the same controllers, regardless of whether the cluster is sitting behind NAT in a clinic basement or installed alongside Odoo on the same Kubernetes cluster.

## What the operator never owns

Two categories of state stay deliberately outside the operator.

- **Application internals.** The operator never knows what a `Deployment` or `Service` produced by a chart is for. It treats the rendered Kubernetes objects as opaque outputs of the chart, watched only via the chart's `HelmRelease` conditions. It does not patch them, label them, or read their `.spec`. Application correctness is the chart's job.
- **Cross-cluster orchestration.** The operator does not know about other clusters and does not try to coordinate with them. Cross-cluster orchestration (e.g., "fail Nextcloud over from cluster A to cluster B") belongs in Odoo as a sequence of per-cluster `Operation` requests, not in the operator itself.

The result is an operator that can be small, predictable, and unsurprising. The interesting behaviour is in the things the operator wires together, not in the operator itself.
