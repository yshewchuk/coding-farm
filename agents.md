# Agent & IDE Integration (`agents.md`)

This document describes how **workspaces** host developers and AI coding agents
in Cloud Sandbox, and how the platform exposes them.

> **Direction note:** This project pivoted from an earlier design built around
> an in-container **Workspace Daemon** and a pluggable **agent harness
> abstraction** (the original content of this file). The shipped MVP is leaner:
> a workspace is a Fly Firecracker microVM running an open-source web IDE
> (`code-server`), and both humans and AI agents operate *inside* it. The
> earlier daemon/harness architecture is preserved verbatim-ish in
> [Future evolution](#future-evolution) so the thinking is not lost, but it is
> **not** implemented today. Do not write code that assumes a daemon or a
> harness interface exists.

For the broader platform architecture, see
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md); for build/test commands, see
[`AGENTS.md`](AGENTS.md).

---

## Table of Contents
- [Goals & Principles](#goals--principles)
- [Concepts](#concepts)
- [The Shipped Model: IDE-as-Workspace](#the-shipped-model-ide-as-workspace)
- [How Agents Use a Workspace](#how-agents-use-a-workspace)
- [Workspace Lifecycle & Scale-to-Zero](#workspace-lifecycle--scale-to-zero)
- [Security Boundary](#security-boundary)
- [Extending the IDE Experience](#extending-the-ide-experience)
- [Future Evolution](#future-evolution)

---

## Goals & Principles
- **Self-hosted first.** Everything runs on infrastructure the operator
  controls (Fly.io compute, Neon data, Logto identity). No mandatory SaaS.
- **Disposable workspaces.** A workspace is ephemeral compute + a persistent
  volume; it can be destroyed and recreated from configuration alone.
- **Vendor neutrality for identity and data.** Logto and Neon are swappable;
  the Management API talks plain Postgres and validates JWTs against any OIDC
  JWKS endpoint.
- **Agent-friendly, not agent-coupled.** The platform hosts an editor and a
  shell; it does not assume any specific AI agent, model, or vendor. Agents are
  just processes running inside the workspace.
- **Scale-to-zero by default.** Idle workspaces suspend and wake on demand, so
  long-running agent or developer sessions cost nothing when paused.

---

## Concepts
- **Workspace** — a single Fly Firecracker microVM booting a template image,
  with an NVMe Fly Volume mounted at `/workspace`. One workspace == one Fly App
  (so it gets a unique scale-to-zero URL).
- **Template** — a version-controlled Dockerfile that, once built, produces an
  image workspace machines boot from. Templates are owned by an organization.
- **Session** — the durable record of a workspace: its status, Fly resource
  ids, and public URL. (In the MVP a session maps 1:1 to a running or suspended
  workspace; the database row survives machine destruction.)
- **Management API** — the Go control plane. It is the sole authorization
  gatekeeper: it validates Logto JWTs, enforces org ownership, and drives the
  Fly Machines/Apps REST API.
- **IDE** — the open-source `code-server` (VS Code in the browser), the
  workspace's primary user-facing surface. Humans and AI agents both interact
  through the editor and the shell it exposes.

---

## The Shipped Model: IDE-as-Workspace

A workspace is, from the outside, a URL serving a web IDE:

```
Browser / Agent ──HTTPS+WS──▶ Fly Proxy ──▶ :8080 ──▶ code-server ──▶ /workspace (NVMe volume)
                                          (autostop=suspend, autostart=true)
```

- **Compute:** a Firecracker microVM created by the Management API via the Fly
  Machines REST API, booting the template's image.
- **Persistence:** an NVMe **Fly Volume** provisioned per session and mounted
  at `/workspace`. The root filesystem is ephemeral; only `/workspace` survives
  suspend/recreate.
- **Networking:** the Fly Proxy terminates TLS and passes WebSockets through to
  `code-server` on port 8080. The workspace requires no inbound configuration
  beyond the `services` array the Management API sets at creation time.
- **Scale-to-zero:** the `services[0]` entry is configured with
  `autostop="suspend"` and `autostart=true`. When all HTTP/WS connections drop,
  the machine suspends (volume retained); the next request to the workspace URL
  wakes it.

The reference image (`docker/Dockerfile.codeserver-workspace`) ships Ubuntu +
Go + `code-server`, running as a non-root `dev` user, with `code-server`
binding `0.0.0.0:8080` and serving `/workspace` as its root folder.

---

## How Agents Use a Workspace

There is no agent-specific protocol in the MVP. An AI coding agent is simply a
process inside the workspace, same as a human developer:

1. **Connect** to the workspace URL (a human opens it in a browser; an agent or
   automation can drive the editor or open a terminal/SSH session as configured
   by the template image).
2. **Work** inside `/workspace`, which is the persistent volume. Anything an
   agent writes there survives hibernation and machine recreation.
3. **Use the template's tooling.** The Dockerfile determines what's installed
   (languages, CLIs, model client tools). An operator extends a template to
   pre-install a specific agent's runtime — this is configuration, not platform
   code.
4. **Hibernate / resume.** The Management API exposes resume and hibernate; the
   Fly Proxy wakes a suspended workspace on demand. Agent state that lives in
   `/workspace` persists; agent state in the ephemeral rootfs does not.

Because the platform is agent-agnostic, adding a new AI assistant today means
baking its runtime into a template Dockerfile — no Management API or UI changes
required.

---

## Workspace Lifecycle & Scale-to-Zero

Driven by the Management API (`internal/service/orchestrator.go`):

1. **Create** — validate the template is ready and belongs to the caller's org;
   ensure a dedicated Fly App; provision an NVMe volume; create the machine
   (image + mount + scale-to-zero `services`); record resource ids + URL.
2. **Running** — the workspace serves `code-server` at its Fly URL.
3. **Hibernate** — `StopMachine`; status → `suspended`. The Fly Proxy keeps the
   URL; the volume is retained.
4. **Resume** — `StartMachine`, or simply hit the URL (Fly Proxy autostarts the
   suspended machine). Status → `running`.
5. **Delete** — destroy the machine, destroy the volume, remove the session row.

The Fly Proxy's autostop/autostart means a workspace can scale to zero without
an explicit Management API call; the API's hibernate/resume are explicit
shortcuts on top of the same mechanism.

---

## Security Boundary

- **Authentication:** Logto issues short-lived JWTs; the Management API verifies
  each request against the IdP JWKS (signature, issuer, audience, expiration).
- **Authorization:** the Management API is the sole gatekeeper (no RLS, no
  DB-level auth). Every template/session lookup is scoped by `org_id`; cross-org
  access returns `404`.
- **Workspace URL = the perimeter.** The reference image disables code-server's
  own auth (`--auth none`) because the Fly Proxy + per-session URL is the
  network boundary. Templates that need stronger per-workspace auth should
  enable it in their own image.
- **No inbound-to-platform assumption for workspaces.** Workspaces are reached
  via the Fly Proxy, not by the Management API dialing in. The Management API
  only calls the Fly REST API outbound.

---

## Extending the IDE Experience

To change what runs inside a workspace, contribute a **template Dockerfile**
rather than platform code:

- Add a language, toolchain, or AI agent runtime by extending
  `docker/Dockerfile.codeserver-workspace` or authoring a new Dockerfile as a
  template in the UI.
- Keep the contract: expose port `8080`, run as a non-root user, serve out of
  `/workspace`, and disable in-editor auth (the URL is the perimeter).
- The Management API will build the Dockerfile via the Fly Apps REST API and
  push it to the Fly internal registry; new workspaces boot from the resulting
  image ref.

This keeps the platform small: "support a new agent" == "ship a new template
image", not "extend the control plane".

---

## Future Evolution

The earlier design (the original content of this document) proposed a richer
agent integration story with an in-container **Workspace Daemon** and a
pluggable **agent harness abstraction**. It is **not** implemented in the MVP
and is captured here as a possible future direction, not a current contract.

### Why it was deferred
The MVP intentionally ships the smallest thing that lets an admin sign up,
create a sandbox, and connect to a web-based IDE. A daemon/harness layer adds
real complexity (in-container supervision, an outbound control channel, a
versioned harness interface, session-state externalization) that the IDE-as-
workspace model does not need yet.

### What the future design envisioned
- A **Workspace Daemon** running inside each container, supervising agent
  processes and reporting state to the control plane over an outbound channel.
- A **harness interface** — a stable contract an AI-agent integration conforms
  to (`initialize` / `run` / `suspend` / `resume` / `destroy`, plus capability
  declaration and `AgentEvent` observation), so new agents plug in without
  modifying the control plane.
- **Session state externalized** to the control plane so agent conversations
  survive workspace recreation ("cattle, not pets" for long-running agent
  sessions).
- **Capability declaration** (`EDIT_FILES`, `RUN_COMMANDS`, `READ_REPOS`, …) so
  the UI gates actions by what an agent can actually do.

### How it would layer onto the current architecture
If adopted, the daemon would be **packaged into the template image** (it already
controls the Dockerfile) and the Management API would gain an outbound control
channel + session-state tables — without changing the Fly Machines/Proxy
substrate or the gatekeeper model. The IDE-as-workspace path would remain valid
for workspaces that opt out of the daemon.

Contributors: do not implement any of the above until it is formally adopted in
an ADR under `docs/`. Until then, the shipped model in
[The Shipped Model](#the-shipped-model-ide-as-workspace) is the source of truth.

---

For the platform-wide architecture and request flows, see
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md). For build & test commands, see
[`AGENTS.md`](AGENTS.md).
