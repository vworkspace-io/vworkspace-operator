# Diagrams

**Last Updated:** 2026-05-28

This directory holds the diagrams used in the rest of the documentation. They are kept as ASCII text rather than as SVG or PNG for two reasons: text diagrams are portable across rendering environments (a Markdown viewer, a terminal, a code review tool) and they live in `git` as diffable source. Generated SVG renders may be added later as a build artifact, but the source-of-truth diagrams remain the text files in this directory.

If a diagram needs to be updated, edit the `.txt` file. The Markdown documents that reference these diagrams either link to them by relative path or embed them inline inside fenced code blocks. Keeping the canonical copy here ensures the two stay aligned.

## Available diagrams

### [architecture.txt](architecture.txt)

The high-level architecture. Shows the Odoo control plane on top, the three connectivity modes (Push, Pull, GitOps) as the arrows between control plane and clusters, and a representative cluster with the `vworkspace-operator`, Flux Helm Controller, and the ops controllers (Velero, CSI snapshots, Argo Workflows, cert-manager, external-secrets, ingress controller). A second cluster appears alongside the first to make the "one operator per cluster, isolated blast radius" property explicit. Referenced from [../concepts/architecture.md](../concepts/architecture.md).

### [pull-mode-sequence.txt](pull-mode-sequence.txt)

A sequence diagram for an end-to-end apply in Pull mode. Shows the operator long-polling `/api/agent/jobs`, Odoo returning a job to apply an `ApplicationInstance`, the operator acknowledging via `/api/agent/jobs/{id}/ack`, server-side applying the manifest, reconciling into a `HelmRelease`, Flux reconciling the chart, and the operator streaming `status` and `result` back. Referenced from [../connectivity/pull-mode.md](../connectivity/pull-mode.md) and [../connectivity/job-protocol.md](../connectivity/job-protocol.md).

## Conventions

- Diagrams are wide rather than tall when there is a choice; most rendering environments wrap text at 100 columns and keeping diagrams under that width avoids word-wrap surprises.
- Components are labeled with the same names used in the prose (`vworkspace-operator`, `Flux Helm Controller`, `Velero`, `Argo Workflows`, etc.) so the diagrams and the docs read together.
- Sequence diagrams use a column-per-actor style with arrows showing the direction of each interaction and brief notes on the right margin.
