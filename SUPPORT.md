# Getting Support

`vworkspace-operator` is open-source software. Support is community-driven and best-effort.

## Where to look first

1. **Documentation.** The full project documentation lives under [docs/](docs/README.md). The most-referenced sections from support threads are:
   - [Installation and bootstrap](docs/install/README.md)
   - [Connectivity modes](docs/connectivity/README.md)
   - [Operations](docs/operations/README.md)
   - [Troubleshooting](docs/operate/troubleshooting.md)
   - [Observability](docs/operate/observability.md)
2. **Existing issues.** Search the GitHub issue tracker for similar problems and resolutions.
3. **The AI assistant in your vWorkspace install.** The Odoo Discuss `@AI_DevOps` and `@AI_Apps` assistants can answer many operator questions against your own running install. They can also read this repository's documentation.

## Where to ask questions

- **GitHub Discussions.** For open-ended questions, design proposals, and "how do I" questions that are not yet documented. Discussions live at `https://github.com/vworkspace-io/vworkspace-operator/discussions` (placeholder; available once the repository is public).
- **GitHub Issues.** For confirmed bugs and well-scoped feature requests. See [CONTRIBUTING.md](CONTRIBUTING.md) for the templates.
- **Community chat.** A community Matrix or Discord space will be linked here once it exists.

## Filing a good question

We can help faster if your question includes:

- Operator version, Kubernetes version and distribution (k3s, Talos, Harvester, managed K8s, single-node Docker host with embedded k3s).
- The relevant `ApplicationInstance` or `Operation` YAML and its `kubectl describe` output.
- Recent operator logs (`kubectl -n vworkspace logs deploy/vworkspace-app-operator`), redacted as needed.
- Whether you are using Pull, Push, or GitOps connectivity (see [docs/connectivity/](docs/connectivity/README.md)).

## Security issues

For vulnerabilities, do **not** open a public issue. Follow the private disclosure process in [SECURITY.md](SECURITY.md).

## Commercial support

Commercial support is not currently offered for this repository. The broader vWorkspace project may offer commercial support in the future; that information will be linked from this page when available.
