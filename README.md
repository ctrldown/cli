# Ctrldown CLI (`ctrldown`)

`ctrldown` is an enterprise-grade, developer-friendly CLI tool for scaffolding monolithic Turborepo + Go Echo monorepos, integrating multi-repo Microfeatures, and manipulating ASTs (Abstract Syntax Trees) across Go and TypeScript.

## Key Capabilities

- **Monolithic Monorepo Scaffolding (`ctrldown create`):** Scaffolds a production-ready Turborepo + `pnpm` workspace with a Go Echo v4 backend (`domains/api`), shared `@workspace/ui` design system, Ory Kratos/Keto authentication, and Next.js frontend domains.
- **Multi-Repo Microfeature Integration (`ctrldown integrate`):**
  - `project`: Inspects and adopts existing repositories into `ctrl.yaml` (Schema v2).
  - `feature`: Wires independent microfeatures (`frontend-package`, `fullstack-feature`, `backend-module`, `backend-worker`) into Go Echo AST (`routes.go`, `main.go`) and workspace packages.
  - `ctrlweb`: Integrates the Ctrlweb visual builder, Go Echo read-only delivery service, `@workspace/ui` component bridge, and optional ultra-minimal Next.js example consumer app.
  - `extract`: Extracts modular slices out of an existing monolith into an independent microfeature repository with `feature.toml`.
- **Drift Detection & Schema Upgrades (`ctrldown status` & `ctrldown upgrade`):** Tracks `ctrl.yaml` schema versions, SHA-256 microfeature checksums (`feature_locks`), and filesystem drift across domains, Go Echo modules, and workspace packages.
- **Cloudflare Edge Auth & Entitlements (`ctrldown login`, `whoami`, `list`):** Authenticates against Ctrldown Edge to display `🔓 Unlocked` and `🔒 Locked` microfeatures in the catalog and enforce license tier access.
- **Standalone Multi-OS Executables (`ctrldown self-update`):** Native standalone binaries for **Linux**, **Windows (`.exe`)**, and **macOS (`amd64` & `arm64`)** with in-place GitHub Release self-updating.

---

## Installation

### 1. Download Standalone Executable (Linux, Windows, macOS)
Download the binary for your platform directly from [GitHub Releases](https://github.com/ctrldown/cli/releases):
- **Windows (`amd64` / `arm64`)**: `ctrldown_windows_amd64.exe`
- **Linux (`amd64` / `arm64`)**: `ctrldown_linux_amd64`
- **macOS (`amd64` / `arm64`)**: `ctrldown_darwin_arm64`

### 2. Build from Source
```bash
git clone git@github.com:ctrldown/cli.git
cd cli
go build -o ctrldown ./cmd/ctrldown
```

---

## Core Commands

```bash
# Authenticate with Ctrldown Edge & inspect session
ctrldown login
ctrldown whoami

# Sync boilerplates and entitled microfeature repositories into ~/.ctrldown/
ctrldown config sync
ctrldown config show

# Browse available Microfeatures (Unlocked / Locked), Templates, and Mixins
ctrldown list

# Create a new monolithic project (Turborepo + pnpm + Go Echo v4)
ctrldown create my-platform

# Integrate an existing project, microfeature, or Ctrlweb pipeline
ctrldown integrate project
ctrldown integrate feature --name auth
ctrldown integrate ctrlweb --with-example-app

# Check ctrl.yaml schema version, architecture drift, and feature updates
ctrldown status
ctrldown upgrade --features

# Check version or self-update binary from GitHub Releases
ctrldown version
ctrldown self-update
```
