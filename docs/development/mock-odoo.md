# Mock Odoo agent API

**Status:** Alpha — development and test substitute for real Odoo modules.
**Last Updated:** 2026-05-30

Real Odoo modules for vWorkspace are not built yet. The in-repo mock server implements the Pull-mode [job protocol](../connectivity/job-protocol.md) so the operator agent can be developed and tested without an Odoo deployment.

## Package and binary

| Path | Purpose |
|------|---------|
| `test/mockodoo/server.go` | In-memory HTTP server (`mockodoo.Server`) |
| `test/mockodoo/admin.go` | Admin enqueue API and `AdminClient` for e2e |
| `test/mockodoo/cmd/mockodoo/main.go` | Standalone process for local dev |
| `Dockerfile.mockodoo` | Container image for in-cluster e2e |
| `test/mockodoo/server_test.go` | Unit and poller integration tests |
| `test/mockodoo/testserver.go` | `NewTestServer()` httptest helper for integration tests |
| `test/integration/pull_loop_test.go` | Full Pull loop: mock → poller → applier → reconciler |
| `test/e2e/pull_loop_test.go` | Kind e2e: in-cluster mock Odoo + deployed operator |
| `hack/dev-pull-loop.sh` | Start mock Odoo and print operator env hints |

## Run locally

```bash
go run ./test/mockodoo/cmd/mockodoo -addr :8080 \
  -registration-token dev-registration-token \
  -cluster-id cluster-dev-1
```

Default registration token: `dev-registration-token` (exchanged via `POST /api/agent/register`).

Point the operator at the mock:

```bash
export ODOO_BASE_URL=http://127.0.0.1:8080
# After registration:
export VWORKSPACE_AGENT_TOKEN=<bootstrap token from register response>
export VWORKSPACE_CLUSTER_ID=cluster-dev-1
make run -- --agent-enabled=true --odoo-base-url="$ODOO_BASE_URL" ...
```

Or register via CLI:

```bash
go run ./cmd/main.go register \
  --odoo-base-url http://127.0.0.1:8080 \
  --registration-token dev-registration-token \
  --cluster-id cluster-dev-1
```

## Implemented endpoints

| Method | Path | Behavior |
|--------|------|----------|
| `GET` | `/healthz` | Liveness/readiness probe (`200 ok`). |
| `POST` | `/api/agent/register` | Accepts one-time registration token; returns `clusterId` and bootstrap `token`. |
| `GET` | `/api/agent/jobs` | Long-poll job queue per cluster (`cluster`, `wait` query params). |
| `POST` | `/api/agent/jobs/{id}/ack` | Marks job acknowledged (`204` or `409` if already closed). |
| `POST` | `/api/agent/jobs/{id}/status` | Stores interim status updates. |
| `POST` | `/api/agent/jobs/{id}/result` | Records terminal result (`409` on duplicate). |
| `POST` | `/api/agent/events` | Appends batched events (deduplicates on `eventKey`). |
| `POST` | `/api/agent/credentials/rotate` | Issues a new bootstrap token for the authenticated cluster. |
| `POST` | `/api/admin/enqueue` | Enqueues a job for a cluster (test/e2e helper; optional `X-Mock-Odoo-Admin-Token`). |
| `GET` | `/api/admin/events` | Lists stored events (`cluster`, optional `kind`, `namespace`, `name` filters). |
| `GET` | `/api/admin/jobs/{id}/result` | Returns terminal result for assertions (same auth as enqueue). |

Authentication matches the protocol: `Authorization: Bearer <token>` and `Accept: application/vnd.vworkspace.agent.v1+json`.

## Test helpers

Prefer `NewTestServer()` for integration tests (includes httptest listener and agent client factory):

```go
ts := mockodoo.NewTestServer()
defer ts.Close()
ts.SetBootstrapToken("cluster-1", "token-1")
ts.EnqueueJob("cluster-1", agent.Job{ID: "job-1", Kind: "apply", ...})

httpClient, _ := ts.NewAgentClient("cluster-1", "token-1")
poller := &agent.AgentPoller{
    Client:  httpClient,
    Applier: &agent.Applier{Client: k8sClient, Scheme: scheme, ClusterID: "cluster-1"},
}
_ = poller.PollOnce(ctx)

if !ts.WasAcked("job-1") { /* ... */ }
res, _ := ts.JobResult("job-1")
```

For kind e2e, build and load the mock image, deploy in-cluster, then use `AdminClient` via port-forward:

```bash
make docker-build-mockodoo MOCK_ODOO_IMAGE=example.com/mock-odoo:v0.0.1
kind load docker-image example.com/mock-odoo:v0.0.1
make test-e2e
```

Inspect mock state: `WasAcked`, `JobResult`, `JobStatuses`, `Events(clusterID)`, `EventsFiltered(clusterID, filter)`, `PendingJobCount`.

### Event idempotency keys

Each `POST /api/agent/events` payload may include `eventKey` on each event. Mock Odoo stores the first occurrence of each key per cluster and ignores duplicates (returns `204` either way). The operator builds keys as:

```
condition/<apiVersion>/<kind>/<namespace>/<name>/<uid>/<conditionType>/<status>@<generation>
```

Use `EventsFiltered` or `GET /api/admin/events?cluster=...&kind=...&namespace=...&name=...` to assert reconciler status reporting in tests.

### Credential rotation

`POST /api/agent/credentials/rotate` (authenticated with the current bootstrap token) returns a new token and updates the mock's token index. The old token is invalidated immediately in the mock (no grace period). The Cluster reconciler calls this when `spec.rotateCredentials: true` on the Cluster CR.

Run tests:

```bash
go test ./test/mockodoo/...
go test ./test/integration/... -count=1
make test-e2e   # requires kind + docker
```

Optional Velero backup e2e: set `E2E_INSTALL_VELERO=true` before `make test-e2e` to install the Backup CRD in the suite.

## Pull loop example (local)

```bash
./hack/dev-pull-loop.sh
# In another terminal, after register:
make run -- --agent-enabled=true --odoo-base-url=http://127.0.0.1:8080
```

Full loop without a cluster is covered by `test/integration/pull_loop_test.go` under `make test`.
In-cluster coverage is in `test/e2e/pull_loop_test.go` under `make test-e2e`.

## Limitations (intentional)

- No payload signing or encryption.
- No rate limiting.
- In-memory only; state is lost when the process exits.
- Long-poll uses short sleeps rather than efficient blocking (sufficient for dev/test).
- Admin API is for tests only; do not expose on production networks without token protection.

Replace with real Odoo modules when the parent vWorkspace Odoo integration is available.
