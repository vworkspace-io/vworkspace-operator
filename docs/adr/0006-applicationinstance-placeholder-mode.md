# 0006 — ApplicationInstance placeholder mode

**Status:** Accepted (2026-06-27)

## Context

`Operation` CRs (backup, restore, upgrade, migration, runcommand, runbook) are
standalone Kubernetes resources, but at every layer they are hard-coupled to a
target `ApplicationInstance` via `spec.targetRef`:

- the admission webhook rejects an `Operation` whose target `ApplicationInstance`
  does not exist, and requires the target to carry the matching capability
  annotation `ops.vworkspace.io/<verb>=<engine>`;
- the operation controller loads the target by name and blocks if it is absent.

There is no way to express an **infra / cluster-level operation** — "run this
command on the cluster", "execute this runbook" — that is not "about" a deployed
application. The engines that matter for infra operations (`job` for
`runcommand`, `helmHookJob` for `migration`, `workflow` for `runbook`) only need
the target's namespace; they never read a Helm release. So a per-cluster
**placeholder / cluster-ops** `ApplicationInstance` that owns no workload but
advertises infra capability annotations is sufficient to be a valid `targetRef`.

The blocker was the reconciler: today it **always** runs `EnsureRelease` and
derives `Ready` purely from `HelmRelease` status (see ADR
[0002](0002-helm-first-via-flux-helmrelease.md)). A no-op placeholder backed by a
real `HelmRelease` (Option A in the hub design) drags in spurious Helm objects
and a Flux dependency for something conceptually empty.

The cross-cutting product design lives in the hub:
`vworkspace/docs/design/cluster-ops-placeholder-instance.md` (Option A vs B).

## Decision

Add a `mode` field to `ApplicationInstanceSpec`:

```go
// +kubebuilder:validation:Enum=managed;placeholder
type InstanceMode string

const (
	InstanceModeManaged     InstanceMode = "managed"
	InstanceModePlaceholder InstanceMode = "placeholder"
)
```

- `spec.mode` defaults to `managed` (CRD `+kubebuilder:default=managed`); empty
  is treated as `managed` in code. Existing instances are unaffected.
- `spec.chart`, `spec.release`, and `spec.values` become **optional** at the CRD
  level. Managed-mode validation still requires them (unchanged); placeholder
  validation forbids `chart`/`values` and, if `release` is set, still enforces
  `release.namespace == metadata.namespace`.

Reconciler behaviour for `mode: placeholder`:

- **skip** `EnsureRelease` (and `DeleteRelease` on finalize) — no Helm
  interaction at all, and no `helmengine.Engine` dependency required;
- set `Ready=True` (`Reason=Placeholder`), `Reconciling=False`,
  `Degraded=False`, `Blocked=False` directly and deterministically;
- preserve the `ops.vworkspace.io/*` capability annotations untouched so the
  operation webhook capability check passes;
- keep finalizer handling clean — nothing to uninstall.

`managed` keeps the existing Helm-first behaviour from ADR 0002 verbatim. The
admission webhook applies the same placeholder contract as the reconciler —
rejecting forbidden `chart`/`values` and an out-of-namespace `release` at
admission time — while skipping only the inline-secret scan for placeholders
(they carry no chart values). It is unchanged for managed instances.

To avoid orphaning a Helm release, the webhook also **rejects a managed →
placeholder update for any instance that owns (or may own) a release** — i.e.
`status.helmReleaseRef` is set, or the old managed spec has `chart`/`release`
configured (a release may already be materializing before its status is
written). The placeholder reconcile path performs no Helm work, so it would
never uninstall the release. Operators must delete the instance — which
uninstalls the release through the managed finalizer — and recreate it as a
placeholder. Only an unconfigured managed instance (no chart/release and no
release status) may be converted in place. As defence-in-depth,
`reconcilePlaceholder` also clears any leftover `status.helmReleaseRef` /
`lastAppliedChart` so a placeholder never reports `Ready` next to a stale
release reference.

This is **Option B** from the hub design — the recommended target end-state.
Option A (empty/raw chart) is not implemented.

## Consequences

- **Clean placeholders.** A cluster-ops instance reaches `Ready` instantly with
  no stray `HelmRelease`, no Flux dependency, and no workload — honestly modelling
  "this instance has no chart".
- **A narrow, well-contained operator change.** One enum field, one reconciler
  branch, mode-aware validation, and tests. The `Operation` pipeline, the
  capability/concurrency model, and the engines are untouched.
- **Backward compatible.** `mode` defaults to `managed`; loosening the
  chart/release/values CRD requirements only relaxes admission — managed
  instances that omit them are still blocked by controller validation, exactly as
  before they could not be created.
- **Security boundary unchanged.** The capability annotation set is still the
  allow-list enforced by the webhook; a placeholder advertises only the infra
  verbs it needs. The per-cluster ops-instance lifecycle/UI and RBAC profile
  review are tracked separately (hub `P6-T005`).
- **Does not supersede ADR 0002.** Helm-first remains the default and the only
  behaviour for managed instances; this ADR adds a non-Helm mode for the
  cluster-ops sentinel only.
