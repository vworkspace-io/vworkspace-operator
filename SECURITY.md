# Security Policy

`vworkspace-operator` is infrastructure software. Operators trust it with their cluster credentials, application data, and audit trails. We take security reports seriously and want to make it easy for researchers and users to report issues responsibly.

## Reporting a vulnerability

Please do **not** report security vulnerabilities through public GitHub issues, Discussions, or pull requests.

Instead, email `security@vworkspace.io`. If you would like to encrypt your report, our PGP key fingerprint will be published at this location once the repository is public (placeholder).

Your report should include, at minimum:

- A description of the issue and its impact.
- Steps to reproduce, or a proof of concept.
- The version(s) affected (operator image tag, CRD API version, Kubernetes version).
- Any suggested remediation, if you have one.

We will acknowledge your report within five working days. We aim to provide an initial assessment within fifteen working days.

## Disclosure policy

We follow a 90-day coordinated disclosure policy:

- We work with you to validate, reproduce, and remediate the issue.
- We will request that you withhold public disclosure until a fix is released, up to 90 days from the date of acknowledgement.
- We will credit you in the released advisory unless you ask to remain anonymous.

Once the repository is public on GitHub, we will publish advisories through the [GitHub Security Advisories](https://docs.github.com/en/code-security/security-advisories) feature.

## Supported versions

The project is pre-1.0. Only `main` (and the most recent tagged pre-release, once tags exist) is supported for security fixes.

| Version | Supported           |
| ------- | ------------------- |
| `main`  | Yes                 |
| any tag | Best-effort, latest only |

Once `v1.0.0` ships, this table will list supported minor versions and their end-of-support dates.

## Scope

In scope:

- The operator's own code, container image, Helm chart, CRDs, and admission webhooks.
- The Pull-mode HTTP job protocol implementation in this repository (the client side; the server side lives in the [vWorkspace](https://github.com/vworkspace-io/vworkspace) repository).
- Documented configuration defaults that result in insecure deployments.

Out of scope:

- Upstream Helm charts that the operator deploys (Nextcloud, Mattermost, etc.). Please report those directly to the chart's maintainers.
- Third-party controllers that the operator integrates with (Flux, Velero, cert-manager, external-secrets). Please report those upstream.
- Cluster-level misconfigurations not introduced by the operator's defaults.

## Automated PR code review (Cursor)

The [Code review](.github/workflows/code-review.yml) workflow checks out the **PR head
ref** (not `pull_request_target` merge semantics) and runs Cursor Agent with `--trust` on
the runner workspace. The PR unified diff is sent to Cursor’s API; the agent is instructed
to read primarily `PR_DIFF.txt`, but malicious PR content could attempt prompt injection
via files in the tree.

- Use label `skip-review` or `[skip review]` in the title to skip untrusted or sensitive PRs.
- Fork PRs from outside the org should be reviewed by maintainers before enabling the job
  (or skipped) — same caution as running `make test` on untrusted branches on a shared runner.
- `CURSOR_API_KEY` is a secret; restrict repository/org secret visibility to maintainers.

## Hardening guidance

See [docs/security/](docs/security/README.md) for guidance on RBAC, secrets handling, authentication, and the project's [threat model](docs/security/threat-model.md).
