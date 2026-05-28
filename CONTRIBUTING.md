# Contributing to vworkspace-operator

Thank you for thinking about contributing. `vworkspace-operator` is an open-source project under the GNU AGPL-3.0 license (see [LICENSE](LICENSE)), developed alongside the broader [vWorkspace](https://github.com/vworkspace-io/vworkspace) project. All contributions are accepted under the same license.

Before you start, please read our [Code of Conduct](CODE_OF_CONDUCT.md). By participating in this project you agree to uphold it.

## Filing issues

We use GitHub Issues for bug reports and small, well-scoped feature requests. Before opening an issue:

- Check existing issues and the [documentation](docs/README.md). The doc index links to the parts most often referenced from issue threads.
- For bug reports, use the [bug report template](.github/ISSUE_TEMPLATE/bug_report.md). Include operator version, Kubernetes version and distribution, Flux version, the `kubectl describe` output for the affected resource, and relevant operator logs (redacted as needed).
- For feature requests, use the [feature request template](.github/ISSUE_TEMPLATE/feature_request.md). For substantial features, prefer opening a Discussion first and consider whether an [RFC](docs/rfcs/README.md) is warranted.

## Proposing changes

We use three levels of design artifact, in increasing weight:

- **Pull request description.** Routine changes and bug fixes.
- **Architecture Decision Record (ADR).** Irreversible or architectural decisions that future maintainers should be able to understand without digging through git history. ADRs live in [docs/adr/](docs/adr/README.md).
- **Request for Comments (RFC).** Substantial new features or new public surfaces (CRD fields, API endpoints, engines). RFCs live in [docs/rfcs/](docs/rfcs/README.md).

If you are unsure which is appropriate, open a Discussion or ask in the relevant issue.

## Code contributions

We have not opened the code tree yet; the Phase 1 milestone (see [ROADMAP.md](ROADMAP.md)) lands the Kubebuilder scaffold. Once code is in tree, the workflow is:

1. Fork the repository and create a topic branch.
2. Make your changes. Keep commits focused and self-explanatory.
3. Add or update tests that cover your change.
4. Update relevant documentation under [docs/](docs/README.md).
5. Add a `[Unreleased]` entry to [CHANGELOG.md](CHANGELOG.md) using the [keep-a-changelog](https://keepachangelog.com/en/1.1.0/) format.
6. Open a pull request following the [pull request template](.github/PULL_REQUEST_TEMPLATE.md).

See [docs/development/contributing.md](docs/development/contributing.md) and [docs/development/build-and-test.md](docs/development/build-and-test.md) for the local development workflow once it exists.

## Developer Certificate of Origin (DCO)

Contributions must be signed off with the [Developer Certificate of Origin](https://developercertificate.org/). Sign off each commit with:

```
git commit -s -m "feat(scope): your change"
```

This adds a `Signed-off-by: Your Name <your.email@example.com>` trailer to the commit message. By signing off, you certify that you have the right to submit the contribution under the project's license.

## Commit messages

We follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) for commit messages. Common types:

- `feat:` a new feature
- `fix:` a bug fix
- `docs:` documentation only
- `refactor:` a code change that does not add a feature or fix a bug
- `test:` adding or updating tests
- `build:` build system or external dependencies
- `ci:` CI configuration
- `chore:` other maintenance

Scope is optional but useful, for example `feat(operation): add helmHookJob engine`.

## Pull request review

Once code is in tree, pull requests need:

- At least one approving review from a maintainer (see [MAINTAINERS.md](MAINTAINERS.md)).
- All CI checks green.
- A DCO sign-off on every commit.
- A `[Unreleased]` entry in the changelog when the change is user-visible.

For architectural or irreversible changes, the PR description must link to an accepted ADR or RFC.

## Security

Please do not report security issues through public GitHub issues. See [SECURITY.md](SECURITY.md) for the private disclosure process.

## Where to ask questions

- The [docs](docs/README.md) cover most "how do I" questions.
- For open-ended discussion, open a GitHub Discussion (placeholder link once the repository is public).
- For governance and project direction, see [GOVERNANCE.md](GOVERNANCE.md) and [ROADMAP.md](ROADMAP.md).

Thank you again for contributing.
