# RFCs

**Last Updated:** 2026-05-28

This directory holds the project's Requests for Comments. An RFC is a written design proposal that needs broader review than a PR description provides, but is not yet a decision the project has made. RFCs are the place a substantial new feature or a new public surface starts; once accepted, they typically result in one or more ADRs that record the decisions the RFC made.

No RFCs exist yet. This document is the procedure for adding one when the time comes.

## When to write an RFC, an ADR, or a PR description

These three artifacts carry different commitments. Picking the right one keeps each useful.

| Artifact          | When                                                                                                                         | Lifecycle                                                                                       |
|-------------------|------------------------------------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------|
| **PR description** | Routine changes. Bug fixes, refactors, doc updates, additive features small enough that the rationale fits in a PR body.       | Lives in the PR; archived with merge. Not promoted to a long-lived doc.                          |
| **RFC**            | Substantial new features. New public surfaces (a new CRD, a new transport, a new engine, a new admission rule). Changes that affect many parts of the codebase and need a broader audience before implementation begins. Reversible only at large cost. | Drafted, reviewed publicly, accepted or withdrawn. An accepted RFC informs implementation PRs; it may produce one or more ADRs once specific decisions crystallize. |
| **ADR**            | A decision the project has made and intends to keep. Architecturally significant. Hard to reverse — reversal is a new ADR.    | Immutable once accepted. Updated only by being superseded.                                       |

The line between "RFC" and "PR description" is most often "how many reviewers do I want, and how much do I want to commit to design before writing code". The line between "RFC" and "ADR" is "is this still a proposal, or is it a settled decision". An RFC can graduate into ADRs; an ADR can replace ADRs; a PR description is rarely the right artifact for either.

## Format

RFCs follow the same numbering and naming convention as ADRs: `NNNN-kebab-title.md`, sequential numbering, in `docs/rfcs/`. RFCs and ADRs use independent number sequences (an RFC can be `0001-...` even though `0001-record-architecture-decisions.md` already exists in `docs/adr/`).

An RFC carries the following sections:

| Section          | Content                                                                                                                  |
|------------------|--------------------------------------------------------------------------------------------------------------------------|
| Title            | Declarative, specific. "Chart provenance verification via cosign" is good; "Better security" is not.                       |
| Status           | One of `Draft`, `Accepted`, `Withdrawn`. Includes the date the status was set.                                            |
| Summary          | One paragraph. What the RFC proposes and why, in language a non-implementer can read.                                     |
| Motivation       | What problem this solves. Concrete scenarios. Why now.                                                                    |
| Proposal         | The design. Code-level detail where relevant; API shapes where applicable. The body of the RFC.                            |
| Alternatives     | The serious alternatives considered and why they were not chosen. (Not "we could do nothing"; the alternatives that someone would otherwise propose.) |
| Open questions   | What the RFC has not resolved. The places review is most needed.                                                          |

Authors are encouraged to be honest in the Alternatives and Open Questions sections. An RFC that lists no alternatives is harder to review; an RFC that admits open questions is easier to accept because reviewers know what they are signing up for.

## How to propose an RFC

1. Pick the next free number under `docs/rfcs/`.
2. Create `docs/rfcs/NNNN-short-title.md` with the sections above. Mark `Status: Draft (YYYY-MM-DD)`.
3. Open a PR titled `rfc(NNNN): <title>`. The PR is where review happens.
4. Solicit review. RFCs benefit from explicit cross-team review: at minimum, a maintainer of the area the RFC touches; preferably also a maintainer in an adjacent area.
5. Iterate on the document in response to review. Substantive changes are commits on the PR; the PR's discussion thread is part of the audit trail even after merge.
6. Accept (the maintainer team agrees the RFC is the right direction) or withdraw (the author or the team decides the proposal is not the right direction). Update `Status` accordingly.
7. If accepted, merge the PR. Implementation can then proceed in follow-up PRs. As specific decisions land, they may produce ADRs that reference the RFC.

A withdrawn RFC is still kept in the repository: the document records what the project considered and decided against. Future RFCs that revisit similar territory can link back.

## Open questions and amendments

An accepted RFC's body should not change substantively. If the design turns out to need an amendment during implementation:

- Small clarifications (typos, missing references) are PRs to the RFC.
- Material amendments are a new RFC that supersedes the old one. The old RFC's `Status` becomes `Superseded by NNNN` (this is an exception to the "Status: Accepted is forever" rule).

Open questions left in the original RFC are answered either in the implementation PRs (recorded in commit messages or PR descriptions) or in follow-up ADRs.

## The current state

This directory contains the index document you are reading. No RFCs have been written for `vworkspace-operator` yet. Topics that may warrant future RFCs, based on the open issues in [../security/threat-model.md](../security/threat-model.md) and the roadmap:

- Chart provenance verification (signed chart digests, cosign or sigstore integration).
- A `Schedule` CRD for recurring operations.
- The Argo CD adapter as a primary Helm engine option.
- A multi-cluster `Operation` orchestrator (cross-cluster work coordinated by Odoo).
- Drift-detection policy as a configurable per-namespace rule.

These are not commitments — only candidates. Anyone in the project's community can propose any of them by following the procedure above. The decision to accept or withdraw an RFC is the maintainer team's; the conversation that leads to that decision is the project's.

## Related material

- [../adr/README.md](../adr/README.md) — Architecture Decision Records, which RFCs can graduate into.
- The repository root's `CONTRIBUTING.md` — How to participate in the project more broadly.
