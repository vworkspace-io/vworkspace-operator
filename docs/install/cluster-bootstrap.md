# Cluster bootstrap

**Status:** Alpha
**Last Updated:** 2026-06-06

This is the full bootstrap procedure for connecting a new Kubernetes cluster to an Odoo vWorkspace control plane in the default Pull-mode connectivity. It expands the six steps that appear in the source-of-truth design note into a complete procedure with the actual commands and the actual control-plane-side actions. Push-mode and GitOps-mode equivalents are noted at the end.

If you have already read [quickstart.md](quickstart.md), this document is the same flow seen from both sides — the cluster side and the control-plane side — and with the validation steps in their natural place rather than at the end.

The reader is assumed to be the cluster admin and, separately or as the same person, an Odoo organization admin. The two roles can be the same human; the procedure separates them only to make the credential-flow direction unambiguous.

## Step 1: provision the cluster

Provision the cluster on whichever substrate you run. The supported substrates and their gotchas are documented in [kubernetes-distros.md](kubernetes-distros.md). At the end of this step you have:

- A Kubernetes cluster at version 1.28 or newer.
- A working `kubeconfig` on the admin's machine pointing at the cluster.
- The prerequisites in [prerequisites.md](prerequisites.md) (default StorageClass, ingress controller of your choice, egress to the control plane).
- **Velero** (optional; enable with `velero.enabled=true` in the Helm chart): required for backup `Operation` CRs to succeed. See [helm.md](helm.md#bundle-v1-session-3) and [../operations/engines/velero.md](../operations/engines/velero.md). E2e can install Velero CRDs when `E2E_INSTALL_VELERO=true`.

Sanity check:

```
kubectl version --short
kubectl get nodes
kubectl get sc
```

## Step 2: install the operator bundle

The operator and its bundled controllers are installed as a single Helm release. Substitute the actual chart version once the project has a tagged release; `0.0.0` is the placeholder.

```
helm install vworkspace-app-operator \
  oci://registry.example.com/charts/vworkspace-app-operator \
  --version 0.0.0 \
  -n vworkspace-system \
  --create-namespace
```

Wait for the operator's pod to become ready and verify the bundled controllers are running:

```
kubectl -n vworkspace-system rollout status deploy/vworkspace-app-operator --timeout=180s

kubectl get pods -n vworkspace-system
kubectl get pods -n velero
kubectl get pods -n cert-manager
kubectl get pods -n external-secrets

kubectl get crd applicationinstances.apps.vworkspace.io operations.ops.vworkspace.io
```

At this point the operator is alive but does not yet know which cluster it is or how to reach Odoo. The next step gives it that identity.

## Flux: contract-only vs full reconcile

Some install paths — including the Phase 1 golden path on kind (`INSTALL_FLUX_CRDS=true` in [../install/helm.md](helm.md) and [../development/real-control-plane.md](../development/real-control-plane.md)) — apply **Flux CRDs only**. They do **not** start `helm-controller` or `source-controller` pods.

| Tier | What is installed | What you can verify |
|------|-------------------|---------------------|
| **Contract-only** (Phase 1 dev default) | `HelmRelease` / source CRDs | Operator materializes `HelmRelease`; control-plane instance may stay `deploying`; `ApplicationInstance` may not reach `Ready` |
| **Full reconcile** (production bundle or optional dev add-on) | CRDs + Flux controller Deployments | Flux installs charts; `HelmRelease` and `ApplicationInstance` can reach `Ready` |

**CRDs present ≠ controllers running.** The API types let the operator write a `HelmRelease`; only running Flux controllers reconcile that object into chart workloads.

- **Production:** the operator Helm bundle installs Flux controllers by default ([prerequisites.md](prerequisites.md#controllers-installed-by-the-operators-bundle)).
- **Dev (optional):** after a CRD-only bootstrap, install controllers — for example with the [Flux CLI](https://fluxcd.io/flux/installation/) (`flux install`) or by switching to the full bundle in [quickstart.md](quickstart.md). See [helm.md#optional-flux-controllers-for-ready](helm.md#optional-flux-controllers-for-ready).

## Step 3: generate a one-time registration token in Odoo

Open the vWorkspace control plane (Odoo) as an organization admin.

1. Go to **Workspace Hub → Cluster Registry → New Cluster**.
2. Fill in:
   - **Display name** — a short, human-readable name. Convention: `<env>-<purpose>`, e.g., `prod-emea-1`, `staging-dev-2`, `homelab-arash`.
   - **Owning organization** — the org this cluster belongs to. In a single-org install, this is fixed.
   - **Connectivity mode** — `pull` for the default.
   - **Allowed namespaces** — a list of namespace names (or glob patterns) the operator may manage. A common starting set: `org-*, default`.
   - **Allowed catalog entries** — which catalog applications the operator may install. Defaults to "all in the organization's plan".
   - **Allowed operation templates** — defaults to the built-in templates (`backup.velero`, `restore.velero`, `upgrade.helm`, `migration.helmHookJob`, `runCommand.job`, `runbook.workflow`).
3. Click **Issue registration token**.

Odoo creates the identity record (status: `Pending`) and shows you the token once. Copy it to a safe location; it is single-use and time-bounded (default 24 hours). Odoo stores only a hash; if you lose the token before exchanging it, issue a new one.

The token has the shape `vwksp-reg-<hex>`; the prefix lets the operator validate the format before sending it.

## Step 4: connect the cluster (declarative golden path)

After `helm install`, connectivity is **not** a Helm value. Apply two manifests: a token `Secret` and a `Cluster` CR that references it. The operator idles until those exist; no second `helm upgrade`, no `agent.enabled` toggle, no `kubectl exec`.

Example templates ship with the chart at `charts/vworkspace-operator/examples/cluster-bootstrap/`. Edit placeholders, then apply:

```
kubectl apply -f charts/vworkspace-operator/examples/cluster-bootstrap/registration-token.secret.yaml
kubectl apply -f charts/vworkspace-operator/examples/cluster-bootstrap/cluster.yaml
```

Or inline (replace placeholders):

```
cat <<'EOF' | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: cluster-prod-1-registration
  namespace: vworkspace-system
type: Opaque
stringData:
  registrationToken: <one-time-token>
---
apiVersion: ops.vworkspace.io/v1alpha1
kind: Cluster
metadata:
  name: cluster-prod-1
spec:
  controlPlaneEndpoint: https://workspace.example.org
  clusterId: <server-issued-uuid>   # optional on first registration; required when token is cluster-bound
  registrationTokenSecretRef:
    name: cluster-prod-1-registration
    key: registrationToken
EOF
```

Use the UUID shown in Cluster Registry for `clusterId` when the token is bound to a known cluster — not the human slug.

The `Cluster` reconciler resolves the token from the `Secret`, calls `POST /api/agent/register`, writes `Secret/vworkspace-system/vworkspace-agent-credentials` (owned by the `Cluster`), records `status.credentialsSecretRef` and `status.observedToken`, and the Pull-mode agent starts automatically when credentials exist.

Watch convergence:

```
kubectl get cluster cluster-prod-1 -w
kubectl get cluster cluster-prod-1 -o yaml | grep -E 'phase:|type: Connected' -A2
```

You expect `status.phase=Connected` and `Connected=True` within a minute. Set `spec.controlPlaneEndpoint` to the URL **reachable from operator pods** (on kind/Linux this is often the host gateway, not `127.0.0.1`).

### Break-glass: register CLI

For debugging only — not the supported golden path. The `register` subcommand applies a `Cluster` CR and waits for credential exchange; prefer the declarative manifests above.

```
kubectl -n vworkspace-system exec deploy/vworkspace-operator -- \
  /manager register \
    --token=<one-time-token> \
    --control-plane-endpoint=https://workspace.example.org \
    --cluster-name=cluster-prod-1 \
    --cluster-id=<server-issued-uuid>
```

### Deprecated: inline token in Cluster spec

`spec.registrationToken` and `spec.controlPlaneBaseUrl` are still accepted for one deprecation minor but emit warnings. Use `registrationTokenSecretRef` and `controlPlaneEndpoint` instead.

## Step 5: verify the cluster's health

Both Odoo and the operator know the cluster now. The verification:

- **From the cluster:** `Cluster.status.conditions[Connected]=True/ControlPlaneReachable`. `Cluster.status.lastHeartbeat` updates regularly (default every 30 seconds).
- **From Odoo:** the Cluster Registry view shows the cluster as `Connected`, with `operatorVersion`, `fluxVersion`, `veleroVersion`, and `lastHeartbeat` populated. If the AI assistant in Discuss is enabled, it confirms the connection in the cluster's channel.

If any bundled controller is missing or unhealthy, `Cluster.status.conditions` carries one of: `ControllerMissing`, `ControllerDegraded`. The condition message names the controller and a remediation. The AI assistant in Odoo Discuss can also be asked to install any missing prerequisite by emitting an `Operation`.

Other useful checks:

```
# The operator's connectivity loop is running
kubectl logs -n vworkspace-system deploy/vworkspace-app-operator --tail=50 \
  | grep -E '(pull_job|connectivity|heartbeat)'

# The bootstrap credential exists and is the only one in vworkspace-system
kubectl get secrets -n vworkspace-system

# The Cluster CR is the only ops.vworkspace.io/Cluster on the cluster
kubectl get clusters.ops.vworkspace.io -A
```

## Step 6: deploy the first application

With the cluster connected, deploy a first application from Odoo. The natural choice is something small and well-tested, such as Vaultwarden or a static-site WordPress, to confirm end-to-end before deploying anything heavy.

1. In Odoo, **Workspace Hub → Apps → Deploy app**.
2. Pick the catalog entry (Vaultwarden, Nextcloud, Mattermost, WordPress, Immich, Gitea, n8n, etc.).
3. Confirm the namespace and the hostname.
4. Click **Deploy**.

Odoo emits an `ensure-application-instance` job that the operator pulls and translates into an `ApplicationInstance` CR. The operator reconciler materializes a `HelmRelease` when Flux CRDs are present.

**Contract-only (CRDs, no controllers):** confirm the job applied and the CRs exist:

```
kubectl get applicationinstances -A
kubectl get helmreleases -A
```

The control-plane instance may remain `deploying`; `ApplicationInstance` may stay `Reconciling` without `Ready` — that is expected until Flux controllers are installed ([Flux: contract-only vs full reconcile](#flux-contract-only-vs-full-reconcile)).

**Full reconcile (controllers running):** the Flux Helm Controller reconciles the `HelmRelease`. The application's URL appears in the Workspace Hub when `ApplicationInstance.status.conditions[Ready]=True`. Watch:

```
kubectl get applicationinstances -A -w
kubectl get helmreleases -A -w
kubectl get pods -A | grep -E 'helm-controller|source-controller'
```

The first deploy is usually the slowest because chart images are not in the cluster's image cache; subsequent deploys are faster.

## Push-mode variation

In Push mode, steps 3 and 4 are replaced by:

3. Odoo's administrator generates a ServiceAccount kubeconfig scoped to `apps.vworkspace.io` and `ops.vworkspace.io` resources (and optional read on `helm.toolkit.fluxcd.io` for status). Apply the ServiceAccount, its `ClusterRoleBinding`s, and the kubeconfig generation procedure documented in [../security/authentication.md](../security/authentication.md).
4. Paste the kubeconfig into the control plane's Cluster Registry as the cluster's credential. Odoo connects to the cluster API, validates it can list `applicationinstances`, and marks the cluster `Connected`.

Steps 1, 2, 5, and 6 are unchanged. The cluster's `Cluster` CR exists but has `spec.connectivityMode: push`; the operator still maintains `Cluster.status` from the cluster side, and Odoo's `watch` on the cluster API reads it.

## GitOps-mode variation

In GitOps mode, steps 3 and 4 are replaced by:

3. The admin sets up a Git repository the cluster's Flux instance is configured to follow (a `GitRepository` resource in `flux-system`). Odoo is configured with a Git write credential to push manifests there.
4. the control plane writes the cluster's initial `Cluster` CR plus any pre-existing `ApplicationInstance` resources into the repo. Flux on the cluster pulls and applies them.

The operator's `Cluster` CR still drives status posts back to the control plane (over the same `POST /api/agent/events` endpoint), so Odoo's UI still reflects cluster health. Steps 1, 2, 5, and 6 are unchanged.

## Related material

- [prerequisites.md](prerequisites.md) — What needs to be true before step 1.
- [helm.md](helm.md) — In-repo chart on kind; `INSTALL_FLUX_CRDS` vs optional Flux controllers.
- [quickstart.md](quickstart.md) — The condensed version of steps 2–6.
- [../development/real-control-plane.md](../development/real-control-plane.md) — Golden path with vWorkspace Server.
- [offline-and-airgapped.md](offline-and-airgapped.md) — Step 2 and step 4 variations for air-gapped installs.
- [../security/authentication.md](../security/authentication.md) — The credential lifecycle Odoo and the operator share.
