<!--
Thank you for contributing to vworkspace-operator.

Before opening this PR, please read CONTRIBUTING.md and make sure your commits
are signed off with the Developer Certificate of Origin (DCO):
  git commit -s -m "feat(scope): your change"
-->

## Tracking

Link this PR to the hub backlog. Task IDs and issues come from
[`docs/tracking/phase-*/tasks.md`](https://github.com/vworkspace-io/vworkspace/tree/main/docs/tracking)
after merge ([WORKFLOW.md](https://github.com/vworkspace-io/vworkspace/blob/main/docs/tracking/WORKFLOW.md)).

- **Task ID:** P4-T### (or other `P#-T###` from hub `tasks.md`)
- **Tracked by:** vworkspace-io/vworkspace#N (hub issue `**GitHub:** #N` on the task)

See [cross-repo PR conventions](https://github.com/vworkspace-io/vworkspace/blob/main/docs/cursor-rules/cross-repo-prs.mdc).

## Summary

What does this PR do, in one or two sentences.

## Motivation

Why this change is needed. Link to the issue, ADR, or RFC that motivates it.

- Closes #
- Related to #

## Implementation notes

Anything reviewers should know up front: trade-offs considered, alternatives rejected, areas to focus the review on.

## How was this tested?

- [ ] Unit tests added or updated
- [ ] Envtest / integration tests added or updated
- [ ] Manual verification (describe steps and environment)

## Checklist

- [ ] Commits are signed off (DCO)
- [ ] Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/)
- [ ] Tests cover the change
- [ ] Documentation updated under [docs/](../docs/README.md) if user-facing behavior changed
- [ ] An entry was added to [CHANGELOG.md](../CHANGELOG.md) under `[Unreleased]` if the change is user-visible
- [ ] If this is an architectural or irreversible decision, an [ADR](../docs/adr/README.md) is added or linked
- [ ] If this introduces a substantial new public surface (CRD field, API endpoint, engine), an [RFC](../docs/rfcs/README.md) is added or linked

<!--
Reviewers: please confirm at least one maintainer approval and green CI before merging.
-->
