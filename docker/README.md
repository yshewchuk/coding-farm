# Reference Workspace Image

This is the sample Dockerfile shipped with Cloud Sandbox. It produces a
workspace image with everything needed for the MVP: a web-based VS Code IDE
(`code-server`) running out of a mounted `/workspace` directory.

## What it provides

- **Ubuntu 22.04** base image.
- Development tooling: `curl`, `git`, `vim`, `openssh-client`, `build-essential`.
- **Go 1.23** toolchain.
- **code-server** — the open-source VS Code Server implementation, exposing a
  full editor in the browser on port `8080`.
- A non-root `dev` user (uid 1000) so files created in the mounted `/workspace`
  volume are not owned by root.

## How Cloud Sandbox uses it

1. An operator creates a **template** in the UI whose `dockerfile_contents` are
   this file (the UI ships a pre-filled copy of this Dockerfile as a default).
2. Clicking **Build image** asks the Management API to build this via the Fly
   Apps REST API; the resulting image is pushed to `registry.fly.io`.
3. When a **workspace** is created from that template, the Management API:
   - provisions an NVMe **Fly Volume** mounted at `/workspace`,
   - creates a Firecracker machine booting the built image,
   - exposes port `8080` through the Fly Proxy with `autostop=suspend` and
     `autostart=true` (scale-to-zero).
4. The user clicks **Open IDE** and is taken to the workspace's unique Fly URL,
   where code-server loads with `/workspace` as the root folder.

## Build & run locally (outside Cloud Sandbox)

```bash
docker build -f docker/Dockerfile.codeserver-workspace -t codeserver-workspace .
docker run -p 8080:8080 -v $(pwd)/workspace:/workspace codeserver-workspace
# Open http://localhost:8080
```
