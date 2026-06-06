# Cluster bootstrap manifests (v2)

Templates for the declarative golden path after `helm install`. Connectivity is data (a `Cluster` CR + token `Secret`), not Helm values.

1. Issue a one-time registration token in vWorkspace Server (Cluster Registry → New Cluster → Issue registration token).
2. Edit `registration-token.secret.yaml` — set `stringData.registrationToken`.
3. Edit `cluster.yaml` — set `spec.controlPlaneEndpoint` to the URL **reachable from operator pods** (on kind/Linux this is often the host gateway, not `127.0.0.1`).
4. Apply (Secret namespace is set in the manifest; `Cluster` is cluster-scoped):

```bash
# From repository root:
kubectl apply -f charts/vworkspace-operator/examples/cluster-bootstrap/registration-token.secret.yaml
kubectl apply -f charts/vworkspace-operator/examples/cluster-bootstrap/cluster.yaml
kubectl get cluster cluster-local -w
```

The reconciler exchanges the token, writes `Secret/vworkspace-agent-credentials`, and the Pull-mode agent starts automatically.

Server-side manifest rendering (from an issued token) is planned in vworkspace-server PR 4. Until then, copy these templates and fill in the placeholders.

Break-glass: `kubectl exec` with `/manager register` — see [docs/install/cluster-bootstrap.md](../../../../docs/install/cluster-bootstrap.md#break-glass-register-cli).
