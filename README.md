# Ctrldown CLI

`ctrldown` is an enterprise-grade, developer-friendly CLI tool for scaffolding microservices and manipulating ASTs (Abstract Syntax Trees) in Go. It acts as a composable template generator and code manipulation engine.

## Features

- **Robust Routing:** Built on top of [Cobra](https://github.com/spf13/cobra).
- **Beautiful UX:** Utilizes [Charmbracelet's Huh and Lipgloss](https://charm.sh/) for elegant, readable terminal outputs and error handling.
- **Testability First:** Uses [Afero](https://github.com/spf13/afero) to abstract the file system, ensuring code is 100% testable via in-memory file systems.
- **AST Manipulation:** Powered by [dst](https://github.com/dave/dst) to programmatically inject and modify Go code without breaking formatting.

## Installation

*Installation instructions will be provided once binaries are released. For now, you can build from source:*

```bash
cd cli
go build -o ctrldown ./cmd/ctrldown
```

## Usage

Run the CLI for help:

```bash
./ctrldown --help
```

### Global Flags

- `--debug`: Enable verbose debug output and stack traces for troubleshooting.
