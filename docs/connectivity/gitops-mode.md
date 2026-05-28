# GitOps mode

**Status:** Alpha
**Last Updated:** 2026-05-28

In GitOps mode, Odoo renders the desired state of each cluster as a set of manifests and commits them to a Git repository. Flux (or Argo CD) on the cluster pulls the Git repository, applies what is there, and reports status back. Neither Odoo nor the operator talks to the other directly; they communicate through Git.

GitOps is the right choice when change control must flow through Git: regulated organizations, multi-team approval workflows, audit regimes that require every change to land in a reviewed commit, and orgs that already treat Git as the system of record. It is not the right choice when low-latency, ad-hoc operator action is the dominant workflow — for that, [Pull](pull-mode.md) or [Push](push-mode.md) are simpler.

## What Odoo writes to Git

The manifests Odoo writes are the **same CRDs** used in every other mode: `ApplicationInstance` (`apps.vworkspace.io/v1alpha1`) and `Operation` (`ops.vworkspace.io/v1alpha1`). Optionally — for organizations that want every Helm intent visible in Git — `HelmRelease` resources may be written directly, in which case the operator can be configured to treat them as authoritative and skip the `ApplicationInstance → HelmRelease` materialization step.

A typical repository layout looks like:

```
clusters/
  cluster-prod-1/
    namespaces/
      org-myteam/
        applicationinstance-nextcloud-myteam.yaml
        operation-nextcloud-myteam-backup-2026-05-28.yaml
```

Each manifest carries the same labels as in any other mode (`app.vworkspace.io/managed-by=odoo`, `app.vworkspace.io/cluster-id=<id>`) so the operator can recognize Odoo-owned objects regardless of who applied them.

## How the cluster syncs

The cluster runs Flux (or Argo CD) configured to follow a path in the Git repository corresponding to its own cluster ID. Flux pulls the commit, applies the manifests, and emits standard Flux events for each apply. The operator on the cluster reconciles the resulting `ApplicationInstance` and `Operation` resources exactly as in any other mode — it has no idea Git is involved.

The Flux components used for GitOps mode (`GitRepository`, `Kustomization`) are the same components already installed by the operator's Helm bundle for the default Pull-mode deployment, so GitOps mode does not introduce new cluster-side dependencies.

## Authentication

- **Odoo holds a Git write credential.** A deploy key, an SSH key, a fine-grained personal access token, or an OAuth app credential, scoped to write the relevant repository path. Best practice is one write credential per Odoo install, and to gate the actual cluster paths via repository-side `CODEOWNERS` or branch-protection rules.
- **The cluster holds a Git read credential.** A deploy key or a fine-grained token scoped to read the relevant repository path. The cluster never needs write access to the repository for the GitOps loop to function.
- **Neither side holds a kubeconfig for the other.** Like Pull mode, GitOps mode keeps Odoo out of the cluster's credential plane.

## Status

Two paths exist for status to reach Odoo in GitOps mode.

- **Through the operator's status channel.** The operator on the cluster can be configured to post status events to the same `POST /api/agent/events` endpoint used by Pull mode. This keeps status low-latency and uniform across modes, at the cost of one outbound HTTPS dependency from the cluster.
- **Through Git events and annotations.** Flux and Argo CD emit events for every apply, every health change, and every drift. Odoo can subscribe to those events (via the Git provider's webhooks, or by polling) and reconstruct the cluster's status without any operator-to-Odoo channel. This is the path organizations choose when the requirement is "no out-of-band signal channel, only Git."

In practice most GitOps deployments combine both: the status channel for low-latency condition transitions, Git events for the audit trail.

## Trade-offs

- **Change control is rigorous.** Every change is a commit. Every commit has an author, a timestamp, a diff, and an optional reviewer. The Git history is the audit trail. This is the property regulated organizations are paying for.
- **Latency is higher.** Two cycle times stack: Odoo's commit interval and Flux's sync interval. End-to-end "I asked for it in Discuss" → "it landed in the cluster" can take minutes rather than seconds. For change-control workloads this is rarely a problem; for ad-hoc human-driven work it can feel slow.
- **Operational complexity is higher.** A Git repository is now part of the platform. Repository availability, repository auth, repository structure, and repository naming all become operational concerns. The dependency is well-understood and well-supported by the GitOps community, but it is a real dependency.
- **Rollback is trivial.** `git revert` plus a Flux re-sync is a complete rollback. There is no operator-side state to walk back; the cluster simply applies the prior commit.
- **Approval workflows are native.** Pull-request review, required approvers, and branch protection rules in Git are the approval workflow. There is no need for separate approval logic in Odoo or the operator.
- **Offline behavior is the strongest.** The cluster keeps reconciling whichever Git revision it last saw. If Git is unreachable, the cluster continues to converge on the last applied revision indefinitely; only *new* intent is paused. The operator's [`Disconnected` condition](../api/conditions.md) is not raised in GitOps mode because there is no operator-to-Odoo connection that can be disconnected; instead, Flux's own `Stalled` conditions on the `GitRepository` surface the situation.

## When GitOps is the wrong choice

- **Single-tenant homelab or small business.** The Git repository is overhead that buys little. Pull is materially simpler.
- **High-throughput operation traffic.** Workflows that generate many `Operation` resources per hour are awkward in Git — every operation becomes a commit. Pull's job stream handles bursty operation traffic without polluting Git history.
- **Truly ad-hoc operator work.** When the dominant workflow is "fix this thing now," the commit + sync latency is friction. Use Pull for these clusters, GitOps for the ones that need it. Hybrid fleets are explicitly supported (see [modes-overview.md](modes-overview.md#hybrid)).

GitOps mode is supported as a first-class alternative for the deployments where its trade-offs are the right ones. It does not change the operator's behavior; it changes how intent and audit travel.
