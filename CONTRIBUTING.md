# Contributing to Ctrldown CLI

We love your input! We want to make contributing to this project as easy and transparent as possible.

## Architectural Guidelines

To maintain an enterprise-grade standard, all contributions MUST adhere to the following strict rules:

1. **No Global State:** Do not use global variables for Cobra commands or flags. Commands must be instantiated via constructor functions (e.g., `NewRootCmd(fs afero.Fs)`).
2. **Abstract the File System:** NEVER use `os.WriteFile`, `os.ReadFile`, or `ioutil` directly. All file system interactions must be injected and pass through `github.com/spf13/afero`. This ensures we can test everything using `afero.MemMapFs` without affecting the real disk.
3. **Error Handling:** Always wrap errors with context using `fmt.Errorf("failed to do X: %w", err)`.
4. **No Stack Traces for Users:** The CLI must catch errors gracefully at the root level. Use the `tui` package to print human-readable error messages. Panic logs and stack traces should only appear if the `--debug` flag is passed.
5. **Code Style:** Write clean, well-commented, idiomatic Go code.

## Development Setup

1. Fork and clone the repository.
2. Navigate to the `cli/` directory.
3. Install dependencies: `go mod download`.
4. Run tests: `go test ./...` (Make sure your logic is tested against the memory file system).

## Pull Request Process

1. Create a new branch for your feature or bugfix.
2. Ensure your changes include appropriate unit tests.
3. Update documentation (like this README) if necessary.
4. Submit a Pull Request with a clear description of the changes and the problem they solve.
