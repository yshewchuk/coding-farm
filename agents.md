# Agent Integration Specification (`agents.md`)

This document is the technical specification for AI agent integration in `<Project Name>`. It covers the **agent harness architecture**, how **workspaces** are built to support agents, and how to **extend the platform** to support new AI models or coding assistants.

It is intended for harness authors, agent vendors, and contributors to the Workspace Daemon. For the broader platform architecture, see `CONTRIBUTING.md`.

---

## Table of Contents
- [Goals & Principles](#goals--principles)
- [Concepts](#concepts)
- [Agent Harness Architecture](#agent-harness-architecture)
- [How Workspaces Support Agents](#how-workspaces-support-agents)
- [Harness Lifecycle](#harness-lifecycle)
- [The Harness Interface](#the-harness-interface)
- [State & Session Communication](#state--session-communication)
- [Extending with a New Harness](#extending-with-a-new-harness)
- [Integrating New AI Models / Assistants](#integrating-new-ai-models--assistants)
- [Bundled Harnesses](#bundled-harnesses)
- [Stability & Versioning](#stability--versioning)

---

## Goals & Principles
- **Agent-first, not human-first.** The platform assumes the primary actor in a workspace may be an AI coding agent. Ergonomics, APIs, and lifecycle are designed around that.
- **Vendor neutrality.** No AI model, vendor, or editor is privileged by the core. All integrations are harnesses that conform to the same interface.
- **Diverse harnesses.** From headless CLI agents to fully integrated editor experiences (e.g. Visual Studio), all are first-class.
- **Pluggability without forking.** A new harness must be addable without modifying the control plane or the Workspace Daemon core.
- **Reproducible execution.** A workspace + harness + model spec must produce a deterministic starting state; runtime nondeterminism from the model is expected, but the environment must not be.

---

## Concepts
- **Agent** — an AI coding assistant that operates inside a workspace (e.g. an LLM-backed coding agent, a CLI assistant, an editor-embedded assistant).
- **Agent Harness** — the integration that knows how to start, drive, and observe a specific agent. A harness is the adapter between the platform and an agent.
- **Workspace** — an ephemeral, reproducible containerized environment that may span multiple repositories and hosts one or more harnesses.
- **Workspace Daemon** — the in-container process that supervises harnesses and bridges them to the control plane.
- **Session** — a durable conversation/execution context between a user (human or automation) and an agent within a workspace. Sessions survive workspace recreation.
- **Control Plane / API Layer** — the central service that manages workspace lifecycle and routes state/session traffic.

---

## Agent Harness Architecture

A harness is a discrete module that conforms to a stable interface and runs *inside* the workspace, supervised by the Workspace Daemon. The control plane never speaks directly to an agent — it always goes through the daemon, which delegates to the harness. This indirection is what keeps the platform vendor-neutral and keeps agents portable across infrastructure.

```
            ┌──────────────────────────────────────────────┐
            │               Control Plane (API Layer)        │
            └──────────────────────┬─────────────────────────┘
                                   │ control channel (outbound from workspace)
            ┌──────────────────────▼─────────────────────────┐
            │              Workspace Daemon                   │
            │  (supervises harnesses, reports state, hooks)   │
            └──────┬───────────────┬───────────────┬───────────┘
                   │               │               │
        ┌──────────▼───┐   ┌───────▼──────┐   ┌────▼────────────┐
        │ Harness: CLI  │   │ Harness: VS  │   │ Harness: Custom │
        │ Agent        │   │ Code/VS      │   │ (3rd-party)     │
        └──────┬───────┘   └──────┬───────┘   └────┬────────────┘
               │                  │                │
        ┌──────▼─────┐     ┌──────▼──────┐   ┌────▼──────────┐
        │  Agent proc │     │ Editor ext  │   │ Your agent     │
        │  (model API)│     │ + agent     │   │ implementation  │
        └─────────────┘     └─────────────┘   └─────────────────┘
```

Key properties:
- The **daemon** is harness-agnostic. It loads harnesses by name and supervises whatever process/extension they spawn.
- A **harness** owns all knowledge of its agent: how to launch it, how to feed it context, how to observe its actions, and how to translate platform concepts (workspace, session, file edits, terminal commands) into agent-native terms.
- The **control plane** only knows workspace IDs, session IDs, and harness names. It does not know about models, vendors, or editor protocols.

### Harness responsibilities
1. **Launch** — start the agent process/extension within the workspace with the resolved environment.
2. **Context binding** — expose the workspace's repositories, environment, and tooling to the agent in the agent's native format.
3. **Session management** — create/resume agent sessions and persist session state via the daemon so it survives workspace recreation.
4. **Action observation** — report agent actions (file edits, command runs, lifecycle events) back through the daemon to the control plane, so the web interface and audit logs can surface them.
5. **Health & telemetry** — report process health and resource usage to the daemon.
6. **Graceful shutdown** — respond to suspend/destroy signals by flushing session state and exiting cleanly.

---

## How Workspaces Support Agents

Workspaces are the substrate agents run on. Their design is driven by agent requirements:

### Multi-repository by default
A workspace aggregates multiple repositories into one working tree with shared environment and tooling. Agents receive a unified view of the code, which is essential for cross-repo refactors and coordinated multi-service changes. The repository list is part of the workspace spec and is initialized deterministically by **Repository Synchronization** — agents never perform ad-hoc `git clone`s.

### Ephemeral and reproducible
Workspaces are disposable. Because agent session state is externalized (via the daemon to the control plane), a workspace can be destroyed and recreated without losing the conversation. An agent resuming a recreated workspace should see the same repositories, same session history, and same resolved configuration as before destruction.

### Templated configuration
The **Workspace Templating Engine** merges an organization template with developer preferences to produce the resolved spec. For agents, this means a consistent baseline of tooling, secrets, and model access policy across all workspaces, while still allowing per-developer or per-task customization within a vetted subset.

### Deterministic initialization
Repo sync + templating together guarantee that two workspaces created from the same spec start from byte-identical state (modulo time and externally-fetched resources the spec declares as nondeterministic). This makes agent behavior reproducible and debuggable.

### Outbound-only networking
The daemon dials out to the control plane. Agents running inside the workspace do not require inbound connectivity from the platform, which keeps workspaces deployable behind NAT/firewalls and across diverse self-hosted infrastructure.

---

## Harness Lifecycle

A harness is driven through a small state machine, mediated by the daemon:

1. **Resolve** — templating + repo sync produce the workspace spec, including the chosen harness and its configuration.
2. **Provision** — the workspace container is created and the daemon starts.
3. **Load** — the daemon loads the named harness (bundled or registered third-party).
4. **Initialize** — the harness binds workspace context (repos, env, session) and prepares the agent.
5. **Run** — the agent is active; the harness observes actions and reports state through the daemon.
6. **Suspend** — on a suspend signal, the harness flushes session state and the daemon snapshots durable state; the container may be stopped.
7. **Resume** — a new container is created from the same spec; the daemon reloads the harness, which restores the session from externalized state.
8. **Destroy** — the harness performs final flush, the daemon finalizes session state, and the container is torn down. Session state may be retained per retention policy.

Sessions are decoupled from container lifetime. This is the core mechanism that makes "cattle, not pets" compatible with long-running agent conversations.

---

## The Harness Interface

All harnesses conform to a single interface. The concrete language bindings live in `daemon/harness/`; conceptually the interface is:

```text
Harness {
  # Metadata
  name() -> HarnessId
  apiVersion() -> SemVer            # harness interface version this targets

  # Lifecycle
  initialize(WorkspaceContext) -> Result
  run(SessionHandle) -> Result
  suspend(SessionHandle) -> Result
  resume(SessionHandle) -> Result
  destroy(SessionHandle) -> Result

  # Observation (called by the daemon)
  onEvent(event: AgentEvent) -> void

  # Capability declaration
  capabilities() -> set<Capability> # e.g. { EDIT_FILES, RUN_COMMANDS, READ_REPOS, MULTI_REPO }
}
```

`WorkspaceContext` carries everything a harness needs to bind an agent to a workspace:
- Resolved repository paths (multi-repo).
- Resolved environment variables and secrets (via the templating engine's secret policy).
- The selected harness configuration.
- The session handle for state continuity.

`AgentEvent` is the common event shape the harness emits to report agent activity (file edits, commands, lifecycle transitions, errors, custom metadata). The control plane forwards these to the web interface, where UI plugins can render harness-specific detail.

**Capability declaration** is important: a harness declares what its agent can do (edit files, run commands, read multiple repos, etc.). The control plane and UI use capabilities to decide what actions to expose and what to render. A read-only analysis agent, for example, would not declare `EDIT_FILES`.

---

## State & Session Communication

Session and state traffic flows:

```
Agent -> Harness -> Daemon -> API Layer -> Web Interface (UI plugin)
```

- **Session state** (conversation, agent-internal memory) is owned by the harness but persisted via the daemon to control-plane storage. The daemon provides a key/value session store keyed by session ID; harnesses define their own value schema.
- **Live events** flow in real time over the control channel so the UI can stream agent activity.
- **Commands inbound to the agent** (e.g. a user sending a message, or automation triggering a task) flow the opposite direction through the same channel.

Because all of this is outbound from the workspace and abstracted behind the harness interface, swapping the underlying AI model or assistant does not change the control plane or the UI — only the harness.

---

## Extending with a New Harness

To add support for a new AI coding assistant:

1. **Choose a distribution model.**
   - **Bundled:** contribute the harness under `harnesses/<name>` and it ships with the platform. Recommended for widely-used agents.
   - **Out-of-tree:** distribute the harness as a standalone module that registers against the same harness interface. Recommended for proprietary or niche agents.

2. **Implement the harness interface** (see above) against the harness API version documented in `daemon/harness/`.

3. **Declare capabilities.** Only declare what your agent actually supports. The platform uses these to gate UI actions and control-plane features.

4. **Handle session state.** Persist session state through the daemon's session store so suspend/resume works across workspace recreation. Do not store session state only inside the container.

5. **Emit `AgentEvent`s** for any action the platform or UI should observe (file edits, commands, errors, lifecycle). Custom metadata can be attached for harness-specific UI plugins to render.

6. **Add a UI plugin** (optional, under `web/plugins/` or out-of-tree via the SDK) if your harness needs custom front-end rendering. The dashboard renders standard agent activity by default; plugins are for harness-specific detail.

7. **Add tests.** At minimum:
   - A harness unit test using a mock WorkspaceContext.
   - An integration test with the daemon verifying suspend/resume preserves session state.
   - An e2e test that creates a workspace, attaches the harness, and runs a scripted interaction.

8. **Document the harness** in its own README: supported models/assistants, capabilities, configuration schema, and required environment.

A harness must never:
- Require inbound network access to the workspace.
- Store durable state only inside the container.
- Modify the control plane, daemon core, or public API to support itself.
- Assume a specific compute provider or AI vendor in its core logic (provider/model selection is configuration).

---

## Integrating New AI Models / Assistants

"Adding a new model" usually means one of two things:

### A. New model backend for an existing harness
If a harness already supports an agent that can target multiple model backends, adding a model is configuration, not code:
- Provide the model endpoint/credentials via the templating engine's secret policy.
- Declare the model in the workspace spec's harness configuration.
- Ensure the harness's session store can serialize any model-specific context.

No platform changes are required. The harness abstracts the model; the platform only sees the harness.

### B. New assistant that needs its own harness
If the assistant has a distinct runtime (different launch semantics, different event model, different editor integration), implement a new harness per the steps above. The platform remains unchanged.

### Model selection & policy
Model selection is a workspace-spec concern, resolved by the templating engine. Organizations can constrain which models/harnesses are available through template policy, so platform operators retain control over model access, cost, and data residency without hard-coding anything into the control plane.

---

## Bundled Harnesses

Bundled harnesses live under `harnesses/`. The flagship integration is:

- **`harnesses/vscode`** — a high-quality integration with Visual Studio / VS Code-family editors. It launches the editor remote experience inside the workspace, binds the multi-repo workspace as the editor's working tree, and exposes agent activity (edits, commands, diagnostics) back through the daemon. The integration is designed so that a human developer and an AI agent can collaborate in the same editor session.

Additional bundled harnesses will be listed here as they are added. Third-party harnesses are encouraged and are first-class — the bundled set is a convenience, not a requirement.

---

## Stability & Versioning

- The **harness interface** is versioned independently of the platform. Harnesses declare the interface version they target. Breaking changes require a new interface version and a migration guide.
- The **Control API** used by the daemon is versioned; harnesses should not call the Control API directly — they go through the daemon.
- The **session store** schema is harness-defined; the platform guarantees the opaque bytes are preserved across suspend/resume, not their interpretation.
- **UI plugin extension points** used to render harness-specific detail are versioned per `CONTRIBUTING.md`.

When in doubt, prefer additive changes and configuration over new interface methods. A harness that can be expressed purely through the existing interface + configuration is preferable to one that requires new platform code.

---

For the platform-wide architecture, contributor workflow, and component locations, see `CONTRIBUTING.md`. For the project vision and differentiators, see `README.md`.
