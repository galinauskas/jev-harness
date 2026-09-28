# Code review — 28 September 2026

Reviewed the CLI, configuration, routing, HTTP client, streaming parser, agent loop, local tools and terminal interface.

## Changes

- Shell timeouts and cancellation terminate the Unix process group, including ordinary child processes. Output-pipe waits are bounded, and child environments omit `OPENROUTER_API_KEY`.
- Authenticated HTTP requests reject redirects. Streaming requests have a ten-minute deadline; routing has a 45-second deadline. API-key access is synchronised with settings updates.
- Streams have an 8 MiB total limit and a 3 MiB per-tool argument limit. Incomplete, malformed, duplicate and sparse tool calls are rejected before execution.
- Aborted tool approvals receive matching history results, allowing subsequent turns to proceed. Cancelled stream consumers drain the terminal event. History accounting includes tool arguments.
- Filesystem tools reject special files, bound reads and edits, handle malformed directory arguments, and avoid integer overflow in line limits.
- Configuration reads and writes are limited to 1 MiB. Saves validate configuration before writing, retaining atomic replacement and private permissions.
- Terminal text is sanitised after decoding JSON and after assembling streamed fragments, closing escape-sequence injection paths.
- Role changes are blocked during active turns to avoid a data race. Completed turn contexts are released. Paste and cursor events now reach the chat input.
- Split filesystem and shell execution from tool definitions, and separated chat/settings rendering from their state handling. Consolidated syntax highlighting and removed unused bell code.
- Changed project-owned spelling to British English, including `sanitise.go` and colour terminology. Third-party identifiers such as `lipgloss.Color`, module paths and protocol headers retain their required spelling.
- Removed all 13 original test files and four temporary security regression files. Rebuilt the root `jev` executable and tidied dependency declarations.

## Verification

Before deleting the tests, the full suite and temporary regression checks passed with `go test -race ./...`. Targeted checks covered cancellation of shell descendants, credential inheritance, redirected requests, malformed/incomplete tool calls, oversized streams, file bounds, terminal controls, paste handling and resuming after an aborted approval.

After removal:

- `go build -o jev ./cmd/jev` passed.
- `go vet ./...`, formatting checks and `go mod verify` passed.
- `govulncheck` reported no known vulnerabilities.
- `go test ./...` confirmed package compilation; no test files remain.
- CLI smoke checks passed for single-role routing, invalid arguments and malformed configuration.
- Terminal smoke checks passed for startup, opening settings, saving an input preference, returning to chat, bracketed paste and clean exit. Saved configuration permissions were 0600, with a 0700 directory.

Live OpenRouter completions were not exercised; network-flow regression checks used local mock servers. The tools remain approval-controlled local execution, not an OS sandbox: approved shell commands and file operations can access the user's files, and YOLO mode skips individual approval. Process-group cancellation is implemented for Unix; other platforms retain shell cancellation and bounded output-pipe waits.
