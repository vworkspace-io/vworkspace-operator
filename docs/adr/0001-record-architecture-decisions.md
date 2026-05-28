# 0001 — Record architecture decisions

**Status:** Accepted (2026-05-28)

## Context

The project is at an early stage. Many architectural choices are being made (Helm engine, connectivity model, CRD shape, blast-radius posture) and many more will follow as the operator matures. Decisions of this kind are difficult to reconstruct from commit history alone: a PR's discussion is rich at the time it lands and impenetrable six months later, especially when the PR's title and body describe the change but not the alternative the project considered and rejected.

Other infrastructure projects have solved this with Architecture Decision Records — short, structured documents that capture the why of a decision in a place a future contributor can find without archaeological work. Michael Nygard's format is the de facto convention in the Kubernetes ecosystem: title, status, context, decision, consequences. It is small enough that a maintainer can write one in an hour and large enough to capture what matters.

The choice we are recording with this ADR is **the practice of recording decisions**. Without it, the next five ADRs in this directory have no agreed-upon format or governance.

## Decision

We will record architecturally significant decisions for `vworkspace-operator` as Architecture Decision Records using the Michael Nygard format:

- One Markdown file per decision, in `docs/adr/`, named `NNNN-kebab-title.md`.
- Sections: Title, Status (`Proposed`, `Accepted`, `Deprecated`, `Superseded`, with the date), Context, Decision, Consequences.
- Numbering is sequential, allocated when the ADR is proposed.
- ADRs are immutable once `Accepted`: revisions land as new ADRs that `Supersede` the old one.
- The index of ADRs lives in [README.md](README.md).

We will write an ADR when a decision is architecturally significant — that is, when it shapes how other decisions land and is intended to be hard to reverse. Routine PRs do not get ADRs; their rationale lives in the PR description. Larger proposals that need wider discussion before becoming decisions are RFCs ([../rfcs/README.md](../rfcs/README.md)).

We will record existing decisions retroactively, starting with this ADR (the meta-ADR), and proceeding with the four prior decisions (Helm engine, connectivity model, CRD count, one-operator-per-cluster).

## Consequences

**Decisions become findable.** A contributor looking at "why does this operator only run one instance per cluster" can answer the question by reading [0005](0005-one-operator-per-cluster.md). A contributor proposing the opposite choice now has a concrete document to argue against, rather than reconstructing a year-old discussion thread.

**The bar for ADRs is "architecturally significant".** This is a judgment call. We accept that some decisions will get an ADR retroactively because they turned out to matter more than expected; we also accept that some ADRs will be written for decisions that, with hindsight, were not significant enough. The cost of writing one ADR too many is small; the cost of missing one is large.

**Maintenance overhead.** New ADRs are part of the PR review process. A maintainer adding an ADR to an existing PR is normal; a separate ADR-only PR is also normal. The overhead is bounded: there will not be hundreds of ADRs.

**Supersession over revision.** ADRs are not edited after acceptance. Decisions that turn out to be wrong are replaced with new ADRs. This is occasionally annoying — "the body of 0042 is misleading and I want to fix it" — but the discipline preserves history. The replacement ADR can explain what the original got wrong.

**Cross-references.** Other documents in the project link to ADRs as the source of truth for "why". The [concepts chapter](../concepts/README.md), the [security chapter](../security/README.md), the [operations chapter](../operations/README.md), and the README all reference the relevant ADRs rather than restating the reasoning inline.

**RFCs feed ADRs.** When an RFC is accepted, one or more ADRs typically land alongside it to record the decisions the RFC made. RFCs without resulting ADRs are unusual; usually they were accepted without enough conviction to be ADR-worthy, which is fine.

**Tooling is minimal.** We do not need an ADR-specific tool. `git`, `grep`, and the README index are enough. We may revisit if the directory grows past 50 ADRs; until then, the format scales with sequential numbering.
