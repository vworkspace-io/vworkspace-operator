# Connectivity

**Status:** Alpha
**Last Updated:** 2026-05-28

This chapter describes how Odoo's intent reaches a cluster and how cluster status reaches Odoo. The operator supports three modes — **Push**, **Pull**, and **GitOps** — plus a hybrid posture where different clusters in the same fleet use different modes. The CRDs (`ApplicationInstance`, `Operation`) and the in-cluster reconciliation loop are identical in every mode; only the transport changes.

**Pull is the default.** It matches the self-hosted ethos of the wider project: the cluster opens an outbound HTTPS connection to Odoo, no inbound ports are required on the cluster network, and Odoo never holds a kubeconfig. **Push** is the simpler choice when Odoo runs inside the cluster it manages or both sides sit on the same trusted network. **GitOps** is the choice when change control must flow through Git — typically a regulated organization or a multi-team approval workflow.

## Read in order

1. [modes-overview.md](modes-overview.md) — Side-by-side comparison of Push, Pull, GitOps, and Hybrid. Read this first.
2. [pull-mode.md](pull-mode.md) — Deep dive on the default. Protocol options, identity, the job model, status reporting, offline behavior, drift handling, security.
3. [push-mode.md](push-mode.md) — When Odoo writes CRDs directly via the K8s API.
4. [gitops-mode.md](gitops-mode.md) — Odoo renders manifests to Git; the cluster's Flux or Argo CD syncs them.
5. [job-protocol.md](job-protocol.md) — Wire-level HTTP contract for Pull mode, with request and response shapes.

For the reconciliation that happens once intent has reached the cluster (the same in every mode), see [../concepts/reconciliation-model.md](../concepts/reconciliation-model.md). For the CRDs themselves, see [../api/README.md](../api/README.md).
