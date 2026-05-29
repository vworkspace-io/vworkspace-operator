# Mock Odoo agent API

**Status:** Alpha — development and test substitute for real Odoo modules.
**Last Updated:** 2026-05-29

Real Odoo modules for vWorkspace are not built yet. The in-repo mock server implements the Pull-mode [job protocol](../connectivity/job-protocol.md) so the operator agent can be developed and tested without an Odoo deployment.

## Package and binary

| Path | Purpose |
|------|---------|
| `test/mockodoo/server.go` | In-memory HTTP server (`mockodoo.Server`) |
| `test/mockodoo/cmd/mockodoo/main.go` | Standalone process for local dev |
| `test/mockodoo/server_test.go` | Unit and poller integration tests |

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
| `POST` | `/api/agent/register` | Accepts one-time registration token; returns `clusterId` and bootstrap `token`. |
| `GET` | `/api/agent/jobs` | Long-poll job queue per cluster (`cluster`, `wait` query params). |
| `POST` | `/api/agent/jobs/{id}/ack` | Marks job acknowledged (`204` or `409` if already closed). |
| `POST` | `/api/agent/jobs/{id}/status` | Stores interim status updates. |
| `POST` | `/api/agent/jobs/{id}/result` | Records terminal result (`409` on duplicate). |
| `POST` | `/api/agent/events` | Appends batched events. |

Authentication matches the protocol: `Authorization: Bearer <token>` and `Accept: application/vnd.vworkspace.agent.v1+json`.

## Test helpers

In tests, construct a server and enqueue jobs:

```go
srv := mockodoo.NewServer()
srv.SetBootstrapToken("cluster-1", "token-1")
srv.EnqueueJob("cluster-1", agent.Job{ID: "job-1", Kind: "apply", ...})
httptest.NewServer(srv.Handler())
```

Run mock Odoo tests:

```bash
go test ./test/mockodoo/...
```

## Limitations (intentional)

- No payload signing or encryption.
- No rate limiting or credential rotation endpoints.
- In-memory only; state is lost when the process exits.
- Long-poll uses short sleeps rather than efficient blocking (sufficient for dev/test).

Replace with real Odoo modules when the parent vWorkspace Odoo integration is available.
