# Governance

`vworkspace-operator` is part of the broader [vWorkspace](https://github.com/vworkspace-io/vworkspace) open-source project. This document describes how decisions are made in this repository specifically.

## Roles

We recognize three roles. Roles are about responsibility and trust, not seniority.

### Contributor

Anyone who has contributed to the project — a pull request, a substantial issue, a documentation improvement, a code review, a triaged report. Contributors do not have repository write access. They participate through pull requests, issues, and Discussions.

### Reviewer

A trusted long-term contributor who has demonstrated good judgement in the areas they review. Reviewers can review and approve pull requests in their area of focus, but cannot merge by themselves. Reviewers are nominated by maintainers and confirmed by lazy consensus.

### Maintainer

A contributor with merge access who is accountable for the long-term health of the project. Maintainers shepherd releases, approve ADRs and RFCs, and are listed in [MAINTAINERS.md](MAINTAINERS.md). New maintainers are nominated by an existing maintainer after sustained meaningful contribution, and confirmed by lazy consensus among current maintainers.

## Decision making

We default to lazy consensus. A change is considered approved if no maintainer objects within a reasonable review window (typically three working days for non-trivial changes; immediate for trivial ones).

Architectural or irreversible decisions require an [Architecture Decision Record (ADR)](docs/adr/README.md). ADRs are merged via the standard pull request flow with at least one maintainer approval and no unresolved objections.

Substantial new features or public-surface changes (new CRDs, new CRD fields, new API endpoints, new engines) require a [Request for Comments (RFC)](docs/rfcs/README.md). RFCs go through a review window (typically two weeks) and require maintainer consensus before they are marked Accepted.

If lazy consensus fails — that is, maintainers disagree and cannot reach agreement after good-faith discussion — the dispute is escalated to the vWorkspace project's overall steering process, which is documented in the parent repository's `GOVERNANCE.md` (forthcoming).

## Adding maintainers

Maintainers are added by nomination from an existing maintainer, after the candidate has demonstrated sustained meaningful contribution to the project across a period of at least three months. The nomination is opened as an issue, reviewed by current maintainers, and confirmed by lazy consensus. The new maintainer is added to [MAINTAINERS.md](MAINTAINERS.md) and [CODEOWNERS](.github/CODEOWNERS).

## Removing maintainers

Maintainers who have not engaged with the project for twelve consecutive months are moved to emeritus status. Maintainers may step down at any time. In rare cases, a maintainer may be removed for cause (sustained Code of Conduct violations, sustained inactivity without communication, or actions damaging to the project) by consensus of remaining maintainers.

## Code of Conduct

All participation in the project is governed by the [Code of Conduct](CODE_OF_CONDUCT.md). Enforcement is the responsibility of the maintainers; see the Code of Conduct for the reporting process.

## License and contributions

All contributions are made under the [GNU AGPL-3.0](LICENSE). Contributors retain copyright in their contributions; by contributing, they grant the project a license under those terms. See [CONTRIBUTING.md](CONTRIBUTING.md) for the DCO sign-off requirement.

## Changes to this document

Changes to this `GOVERNANCE.md` require an ADR.
