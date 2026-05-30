# Architecture Decision Records

**Last Updated:** 2026-05-30

This directory contains the Architecture Decision Records (ADRs) for `vworkspace-operator`. An ADR records a decision the project has made about its architecture, why the decision was made, and what its consequences are. ADRs are not specifications and not RFCs (see [../rfcs/README.md](../rfcs/README.md) for that distinction); they are the project's institutional memory.

ADRs are written when:

- The decision is architecturally significant (it shapes how other decisions land).
- The decision is non-obvious enough that someone six months later would need to ask "why".
- The decision is intended to be hard to reverse — reversal requires a new ADR that supersedes the old one.

If a decision is reversible at the cost of a routine PR, it does not need an ADR. The bar is "would I want a record of this when I reread the codebase a year from now?"

## Format

The project uses the Michael Nygard ADR format. Each ADR is a Markdown file with five sections:

| Section       | Content                                                                                                       |
|---------------|---------------------------------------------------------------------------------------------------------------|
| Title         | A short, declarative sentence (e.g., "Helm-first via Flux HelmRelease").                                       |
| Status        | One of `Proposed`, `Accepted`, `Deprecated`, `Superseded`. Includes the date the status was set.               |
| Context       | What problem motivates the decision. What constraints apply. What was tried, considered, or rejected.          |
| Decision      | What the project will do. Imperative voice. Specific enough that a contributor can verify they are following it. |
| Consequences  | What follows from the decision. Both the benefits and the costs. The trade-offs other decisions inherit.       |

ADRs are immutable in spirit: once `Accepted`, the body does not change. To revise a decision, write a new ADR that `Supersedes` it and update the old ADR's `Status` to `Superseded by NNNN`.

## Numbering and filenames

ADRs are numbered sequentially, starting from `0001`. The filename is `NNNN-kebab-title.md`. The number is allocated when the ADR is proposed; if a proposed ADR is abandoned before acceptance, the number is retired (do not reuse it).

The title in the filename is a short kebab-case slug; the actual title in the file's H1 is the full sentence.

## How to add a new ADR

1. Pick the next free number by listing the existing ADRs and incrementing.
2. Create `docs/adr/NNNN-short-title.md` with the five sections from the template above.
3. Mark `Status: Proposed (YYYY-MM-DD)`.
4. Open a PR. The PR is the place the decision is discussed; the ADR captures the conclusion.
5. When merged, update the status to `Accepted (YYYY-MM-DD)` and add an entry to the list below.

## Existing ADRs

| Number | Title                                                                                                      | Status     |
|--------|------------------------------------------------------------------------------------------------------------|------------|
| 0001   | [Record architecture decisions](0001-record-architecture-decisions.md)                                      | Accepted   |
| 0002   | [Helm-first via Flux HelmRelease](0002-helm-first-via-flux-helmrelease.md)                                  | Accepted   |
| 0003   | [Pull mode as default connectivity](0003-pull-mode-as-default-connectivity.md)                              | Accepted   |
| 0004   | [Two CRDs: ApplicationInstance and Operation](0004-two-crds-applicationinstance-and-operation.md)            | Accepted   |
| 0005   | [One operator per cluster](0005-one-operator-per-cluster.md)                                                | Accepted   |

## When to write an ADR vs an RFC vs a PR description

The distinction matters because each artifact carries a different commitment.

- **PR description.** Routine changes (most). The PR description is the place the rationale lives. No long-term commitment.
- **RFC** ([../rfcs/README.md](../rfcs/README.md)). A substantial new feature, a new public surface, an irreversible-at-large-cost change that should be reviewed by more than the PR reviewers before it lands. RFCs are proposals; they are discussed in writing and can be amended or withdrawn before they become decisions.
- **ADR.** A decision the project has made and intends to keep. ADRs land after the question is resolved; they record the decision, not the discussion.

An RFC can graduate into one or more ADRs once it is accepted. An ADR rarely demotes back into an RFC; the only common reason is that the decision turned out to be wrong, in which case the new ADR that supersedes it carries the revision.
