# <Project Name>

> A modern, self-hosted cloud coding platform built for AI coding agents and complex, multi-repository development workflows.

> **Status:** Early-stage / experimental. APIs and architecture are subject to change.

`<Project Name>` is an open-source, self-hosted platform for running AI coding agents and human developers in reproducible, disposable development environments. It is designed from the ground up to treat development containers as **ephemeral resources** ("cattle, not pets") while providing first-class support for **multi-repository workspaces** and **pluggable agent harnesses**.

---

## Why Another Cloud Coding Platform?

Existing cloud development environments tend to fall into one of two camps:

1. **Hosted SaaS platforms** that are easy to use but lock you into a vendor, a region, and their billing model.
2. **Long-lived dev containers** that slowly accumulate state, drift from their intended configuration, and become impossible to safely reproduce.

`<Project Name>` takes a different stance. Every workspace is:

- **Disposable** — spun up from a template, torn down when no longer needed, and recreatable from configuration alone.
- **Reproducible** — initialized from version-controlled templates combined with deterministic repository synchronization.
- **Multi-repository by default** — a single workspace can mount and operate across many repositories at once, which is essential for AI agents that need to navigate cross-cutting changes.
- **Agent-first** — the platform is built around the assumption that the primary "user" of a workspace may be an AI coding agent, not a human typing in a terminal.

---

## Core Objectives & Differentiators

### Ephemeral Dev Containers
Workspaces are short-lived, automatically managed resources. Lifecycle operations (create, snapshot, suspend, destroy) are handled by the control plane so that developers and agents never need to think about the underlying container runtime. Because state lives outside the container, a workspace can be destroyed and recreated in seconds without losing work.

### Multi-Repo Agent Workspaces
A workspace is not bound to a single repository. Workspaces aggregate multiple repositories into a single working tree with shared tooling, environment variables, and agent context. This enables workflows such as:

- An agent making a coordinated change across a service and its clients.
- Cross-repo refactors with a single source of truth for dependencies.
- Monorepo-like ergonomics over a polyrepo reality.

### Diverse Agent Harnesses
The platform does not assume a single coding agent. It ships with a harness abstraction that can integrate arbitrary AI coding assistants — from CLI-based agents to fully integrated editor experiences, including high-quality integration with **Visual Studio**. New harnesses can be added without modifying the core control plane.

### Self-Hosted Infrastructure
`<Project Name>` is designed to run on infrastructure you control. There is no mandatory cloud account, no required managed database, and no telemetry phone-home. Deployment targets range from a single node to a full Kubernetes cluster.

### Vendor Neutrality
The platform is agnostic to specific cloud providers. Compute is abstracted behind a provider interface so that the same workspace definition can run on bare metal, a private cloud, or a hyperscaler. Instance types, regions, and capacity are configuration, not architecture.

### Extensibility
A modular design lets you extend the platform at every layer:

- **Agent harnesses** — plug in new AI models and coding assistants.
- **Web interface** — a plugin architecture for custom front-ends and surfaced integration details.
- **API layer** — a stable control-plane API for programmatic workspace management.
- **Workspace Daemon** — an in-container process that exposes hooks for custom lifecycle behavior.

---

## System Architecture

The platform is composed of five cooperating components. See `CONTRIBUTING.md` for a developer-oriented deep dive and `agents.md` for the agent integration specification.

| Component | Responsibility |
| --- | --- |
| **Workspace Daemon** | A lightweight process running *inside* each agent container. Manages local execution, agent process lifecycle, and per-workspace state. Communicates with the API Layer over a control channel. |
| **Workspace Templating Engine** | Merges organization-level workspace templates with individual developer preferences to produce a final, reproducible workspace configuration. |
| **Repository Synchronization** | Ensures every new (and resumed) workspace is initialized with the latest repository code, deterministically and without manual `git clone` steps. |
| **API Layer** | The central control plane. Handles workspace lifecycle (create/suspend/resume/destroy), routes state and session traffic between active workspaces and the platform, and exposes the stable public API. |
| **Web Interface** | A management dashboard for interacting with cloud agents. Features a plugin architecture to support custom front-ends and surface integration-specific details directly in the UI. |

```
                 ┌─────────────────────────────────────────────┐
                 │                 Web Interface                │
                 │   (management dashboard + plugin front-ends)  │
                 └──────────────────────┬──────────────────────┘
                                        │  HTTPS / control API
                 ┌──────────────────────▼──────────────────────┐
                 │                  API Layer                    │
                 │   workspace lifecycle · state/session routing  │
                 │   templating engine · repo sync orchestration  │
                 └──────┬───────────────────────────┬──────────┘
      control channel  │                             │  templating + sync
                 ┌──────▼──────────┐          ┌──────▼──────────────────┐
                 │ Workspace Daemon │          │  Repo Sync / Templates  │
                 │  (in container)  │          └─────────────────────────┘
                 └──────┬──────────┘
                        │ spawns / supervises
                 ┌──────▼──────────────────────────────────────┐
                 │          Agent Harness(es)                   │
                 │   CLI agents · editor integrations · custom   │
                 └──────────────────────────────────────────────┘
```

---

## Getting Started

> The platform is in early development. The instructions below describe the intended end-user experience; some pieces may still be under construction. See `CONTRIBUTING.md` for the current state of local development.

### Prerequisites
- A container runtime (Docker or a compatible OCI runtime).
- A control-plane host (a single VM is sufficient for evaluation; Kubernetes for production).
- One or more repositories you want to develop against.

### Quick Start (intended)
```bash
# Deploy the control plane (self-hosted)
project-name deploy --config ./platform.yaml

# Create a multi-repo workspace from a template
project-name workspace create \
  --template org/backend-services \
  --repo github.com:acme/payments.git \
  --repo github.com:acme/clients.git

# Attach an agent harness to the workspace
project-name agent attach --harness vscode --workspace ws-123
```

---

## Documentation
- **`CONTRIBUTING.md`** — Local setup, architecture walkthrough, and how to contribute to each layer (API, UI plugins, Workspace Daemon).
- **`agents.md`** — Technical specification for the AI agent integration: harness architecture, workspace support, and how to add new models/assistants.

---

## Project Goals / Non-Goals

### Goals
- Make AI coding agents productive in self-hosted, multi-repo environments.
- Keep workspaces reproducible and disposable without sacrificing developer ergonomics.
- Remain deployable on infrastructure the operator fully controls.
- Stay vendor- and model-neutral across compute providers and AI providers.

### Non-Goals
- Becoming a hosted SaaS. There is no managed offering of this project.
- Replacing your existing CI/CD system. Workspaces are for development and agent execution, not production builds.
- Lock-in to a specific AI vendor, editor, or container runtime.

---

## License
`<Project Name>` is open-source software. License details will be added prior to the first public release (intended: a permissive OSI-approved license such as Apache 2.0 or MIT).

---

## Community
- **Issues:** File bugs and feature requests in the issue tracker.
- **Contributing:** Read `CONTRIBUTING.md` before opening a pull request.
- **Discussions:** Use GitHub Discussions for design conversations and questions.

This project follows a Code of Conduct that all contributors are expected to uphold.
