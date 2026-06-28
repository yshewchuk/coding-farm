# Contributing to `<Project Name>`

First off — thank you for considering a contribution. `<Project Name>` is an open-source, self-hosted cloud coding platform for AI coding agents, and community contributions are essential to making it vendor-neutral, extensible, and useful across a wide range of infrastructure.

This guide covers:

1. Setting up the project locally.
2. The architecture of the platform and each component.
3. How to contribute to each layer (API, UI plugins, Workspace Daemon, templating, repo sync).

---

## Table of Contents
- [Code of Conduct](#code-of-conduct)
- [Repository Layout](#repository-layout)
- [Development Prerequisites](#development-prerequisites)
- [Local Setup](#local-setup)
- [Architecture Overview](#architecture-overview)
- [Contributing by Layer](#contributing-by-layer)
  - [API Layer (control plane)](#api-layer-control-plane)
  - [Web Interface & UI Plugins](#web-interface--ui-plugins)
  - [Workspace Daemon](#workspace-daemon)
  - [Workspace Templating Engine](#workspace-templating-engine)
  - [Repository Synchronization](#repository-synchronization)
- [Agent Harnesses](#agent-harnesses)
- [Testing](#testing)
- [Coding Standards](#coding-standards)
- [Pull Request Process](#pull-request-process)
- [Release Process](#release-process)

---

## Code of Conduct
Participation in this project is governed by the project's Code of Conduct. By participating you agree to abide by its terms. Please report unacceptable behavior to the maintainers via the security contact listed in the repository.

---

## Repository Layout

The repository is organized by component so that each layer can be developed and released somewhat independently.

```
.
├── README.md                  # Project overview and vision
├── CONTRIBUTING.md            # This file
├── agents.md                  # AI agent integration specification
├── docs/                      # Long-form design docs and ADRs
├── api/                       # Control plane (API Layer)
│   ├── cmd/                   # Server entrypoints
│   ├── internal/              # Internal services (lifecycle, routing, authn/z)
│   ├── proto/                 # Public API definitions (gRPC + REST)
│   └── providers/             # Compute/storage provider implementations
├── web/                       # Web Interface (dashboard + plugin runtime)
│   ├── app/                   # Core dashboard application
│   ├── plugins/               # Built-in UI plugins
│   └── sdk/                   # Plugin SDK for third-party front-ends
├── daemon/                    # Workspace Daemon (runs inside containers)
│   ├── cmd/
│   ├── internal/
│   └── harness/               # Harness host-side glue (spawning/supervising agents)
├── templating/                # Workspace Templating Engine
├── reposync/                  # Repository Synchronization
├── harnesses/                 # Bundled agent harness integrations (e.g. vscode)
│   └── vscode/
├── deploy/                    # Deployment manifests (compose, k8s, bare-metal)
└── e2e/                       # End-to-end test harness
```

> Component languages and toolchains are documented in each component's own `README`. The platform favors a small number of mainstream runtimes to keep the contributor surface area manageable.

---

## Development Prerequisites
- A container runtime (Docker or compatible OCI runtime).
- The language toolchains for the components you intend to work on (see each component's README).
- `git`, `make`, and a POSIX shell.
- For end-to-end tests: the ability to run local containers and bind a loopback port for the control plane.

---

## Local Setup

A single `make` target brings up a complete local stack: control plane, web interface, a sample workspace daemon, and a bundled harness.

```bash
git clone https://github.com/<org>/<project-name>.git
cd <project-name>

# Bootstrap dependencies and toolchains for all components
make bootstrap

# Start the local dev stack (control plane + web + one test workspace)
make dev

# Run the full test suite
make test
```

`make dev` exposes:
- The **Web Interface** at `http://localhost:<web-port>`.
- The **Control API** (gRPC + REST) at `http://localhost:<api-port>`.

You can create a sample multi-repo workspace and attach the bundled Visual Studio harness via the dashboard or the CLI:
```bash
make cli
./bin/project-name workspace create --template samples/multi-repo
./bin/project-name agent attach --harness vscode --workspace ws-local
```

---

## Architecture Overview

`<Project Name>` is a control plane plus an in-container agent runtime. The five components cooperate as follows:

1. **Workspace Daemon** (in-container) supervises agent processes and holds per-workspace state. It dials out to the control plane and maintains a long-lived control channel — workspaces are never exposed inbound to the platform, which keeps them portable across NAT'd or firewalled infrastructure.
2. **Workspace Templating Engine** resolves a workspace request (template + developer overrides + repo list) into a concrete workspace spec: container image, environment, mounts, harness, and sync plan.
3. **Repository Synchronization** materializes the requested repositories into the new workspace deterministically (pinned refs, shallow where appropriate, atomic application) so every workspace starts from identical code.
4. **API Layer** is the single control-plane entrypoint. It owns workspace lifecycle (create / suspend / resume / snapshot / destroy), routes state and session traffic between active workspaces and the web interface, and persists only the durable state required to recreate workspaces.
5. **Web Interface** is the operator/developer dashboard. It consumes the Control API and hosts a plugin runtime so that harness-specific or organization-specific front-ends can be loaded without forking the dashboard.

State is intentionally external to containers: the platform stores workspace specs, session metadata, and snapshots, but never depends on a container staying alive. This is what makes workspaces disposable.

---

## Contributing by Layer

### API Layer (control plane)
Location: `api/`

The API Layer is the brain of the platform. Contributions here usually fall into one of:

- **Workspace lifecycle** — new lifecycle transitions, improved scheduling, or better suspend/resume semantics in `api/internal/lifecycle`.
- **State/session routing** — how messages and session traffic flow between the web interface and active workspaces (`api/internal/routing`).
- **Provider implementations** — new compute or storage backends under `api/providers`. Each provider implements the `Provider` interface so the control plane stays vendor-neutral. This is the primary entry point for "make it run on my infrastructure" contributions.
- **Public API surface** — protocol definitions in `api/proto`. The public API is versioned; changes require an updated API version or a backward-compatible additive change.

When working on the API:
- Keep the public protocol in `api/proto` backward-compatible unless you're intentionally cutting a new major version.
- Add integration tests under `api/internal/.../*_test.go` for any new lifecycle behavior.
- Avoid introducing state that lives only inside a running container. Durable state belongs in the control plane's stores.

### Web Interface & UI Plugins
Location: `web/`

The dashboard is a standard front-end application, but its distinguishing feature is the **plugin architecture**. Plugins let organizations surface integration-specific details (a custom agent status panel, a vendor-specific action, an internal approval flow) without modifying core.

Contributing a UI plugin:
1. Use the Plugin SDK in `web/sdk` to scaffold a plugin. A plugin is a self-contained module that registers one or more *contributions* (panels, actions, settings tabs, workspace views).
2. Declare the manifest (id, version, required API version, contributions).
3. Implement against the stable dashboard extension points only — do not import internal dashboard modules.
4. Add the plugin under `web/plugins/<your-plugin>` (for built-ins) or distribute it out-of-tree using the SDK.

Dashboard core changes (`web/app`) should be additive and avoid breaking the published extension-point contracts. Extension points are versioned; bumping one requires a migration note in `docs/`.

### Workspace Daemon
Location: `daemon/`

The daemon is deliberately small and stable: it must run inside arbitrary agent containers with minimal dependencies. Contributions should keep its footprint low and its surface narrow.

Responsibilities of the daemon:
- Maintain the control channel to the API Layer.
- Spawn and supervise agent harness processes.
- Report local state (process health, resource usage, agent events) to the control plane.
- Expose local hooks for lifecycle events.

When contributing to the daemon:
- Do **not** add heavyweight dependencies. Anything that bloats the in-container image is a regression.
- All control-plane communication must be outbound and authenticated; never assume inbound network access to the workspace.
- New lifecycle hooks should be opt-in and declared in the workspace spec by the templating engine.
- Keep the daemon agnostic of any specific harness — harness-specific logic belongs in the harness package or the harness integration, not the daemon core.

### Workspace Templating Engine
Location: `templating/`

Templating is responsible for producing a *resolved* workspace spec from:
- An **organization-level template** (the source of truth for tooling, image, defaults).
- **Developer preferences/overrides** (allowed to tweak a constrained subset).
- The **requested repository list**.

Merging semantics are intentional and order-sensitive: org defaults apply first, developer overrides apply second, and only a vetted subset of fields is overridable. This lets organizations enforce standards while keeping per-developer ergonomics.

Contributing here:
- Changes to the merge precedence or the set of overridable fields are breaking changes and require a version bump plus a docs update.
- Add a templating test for every new overridable field, asserting both the org-default and developer-override cases.
- Keep templates declarative and serializable (no executable code in templates).

### Repository Synchronization
Location: `reposync/`

Repo sync guarantees deterministic workspace initialization. The non-goals are: not a Git hosting replacement, not a CI checkout, not a mirror.

Contributing here:
- New sync strategies (full, shallow, sparse, monorepo subtree) belong under `reposync/strategies`.
- Every strategy must be reproducible given the same spec: the same refs must produce the same tree, no implicit "latest branch" resolution unless the spec explicitly requests it.
- Concurrency, retry, and partial-failure behavior must be explicit; a half-initialized workspace is worse than a failed one.

---

## Agent Harnesses
Agent harnesses are how the platform talks to AI coding agents. They are documented in full in `agents.md`. At a high level:

- A harness is a discrete integration that knows how to start, drive, and observe a particular AI coding assistant.
- Harnesses plug into the **Workspace Daemon** via a stable harness interface; the daemon does not know about specific models or assistants.
- Bundled harnesses live under `harnesses/` (e.g. `harnesses/vscode`). Third-party harnesses can be distributed out-of-tree using the same interface.

To add a new harness, follow `agents.md`, then place the integration under `harnesses/<name>` (bundled) or publish it as a standalone module.

---

## Testing
- **Unit tests:** per-component, run with `make test-unit`.
- **Integration tests:** component-pair tests (e.g. daemon ↔ API) run with `make test-integration`.
- **End-to-end tests:** full stack under `e2e/`, run with `make test-e2e`. These spin up real containers; they require a working container runtime.

Please add tests alongside any change. New public API surfaces, new lifecycle transitions, new harnesses, and new templating fields all require accompanying tests.

---

## Coding Standards
- Follow the existing style in each component; tooling (formatters + linters) is wired into `make fmt` and `make lint`.
- Keep functions and modules small and single-purpose.
- Public API and interface contracts are documented inline; internal helpers need not be.
- No new dependencies without justification — especially in the daemon, which must stay lightweight.
- Never commit secrets, credentials, or organization-specific configuration.

---

## Pull Request Process
1. Open an issue or discussion for anything beyond a small fix, so design can be agreed first.
2. Fork and branch from `main` (or the relevant long-lived branch for a backport).
3. Keep PRs focused — one layer or one concern per PR where possible.
4. Ensure `make fmt`, `make lint`, and the relevant test targets pass locally.
5. Update documentation (`docs/`, `README.md`, `CONTRIBUTING.md`, `agents.md`) for any user- or contributor-visible change.
6. Add a changelog entry if one is requested in the PR template.
7. Mark the PR ready for review; a maintainer from the relevant component area will review.

For interface or protocol changes (Control API, daemon harness interface, UI plugin extension points), call this out explicitly in the PR description so maintainers can assess compatibility.

---

## Release Process
- The project uses semantic versioning.
- Breaking changes to the Control API, daemon harness interface, or UI plugin extension points require a major version bump.
- Each release publishes container images for the control plane and daemon, a CLI binary, and a changelog.
- See `docs/release.md` for the detailed runbook (maintainers).

---

Thank you for helping build a self-hosted, agent-first, multi-repo cloud coding platform.
