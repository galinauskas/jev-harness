# Code review — 30 September 2026

Reviewed the CLI, configuration, routing, all three provider clients, streaming parsers, agent loop, context compaction, session persistence, local tools and terminal interface. This follows the 28 September review and covers the newly added features.

## Findings and fixes

- Saved transcripts could replay terminal commands from altered session files. Resumption now preserves only bounded colour and emphasis sequences; clipboard, cursor and other terminal controls are removed.
- Background context lookups could read agent configuration while settings replaced it. Each lookup now captures its provider client before starting, and replies from an older settings generation are ignored. Context windows and token-estimate calibration are kept separate by provider. Changed or deleted roles refresh the displayed mapping.
- OpenCode Go's native streams lacked the per-tool argument bound applied to chat completions. A shared validator now enforces complete calls, unique IDs, valid JSON, at most 128 calls and a 3 MiB argument limit across all protocols. Messages fragments are bounded before concatenation. Failed calls never reach approval or execution.
- Saved native provider history was only shallow-copied. History snapshots and restored sessions now detach native JSON buffers as well as tool-call slices.
- Configuration and session reads now share a bounded regular-file reader. On Unix it rejects symbolic links and opens without blocking on named pipes before checking the file type. Atomic saves and private permissions remain in place.
- A stale error status could prevent a successful save-and-quit after an aborted turn. Quitting now depends on the current save result.
- Repeated routing choices were hidden, including changes between automatic and pinned routing. Every turn now shows its source and provider; a pin is labelled `pinned`, rather than displaying an invented confidence measurement.
- OpenRouter streams explicitly request usage. Missing routing credentials fail before an HTTP request. Provider keys remain excluded from shell-tool environments, and authenticated requests reject redirects.
- Split chat commands, event handling and context lookups into separate files. Consolidated private-file reads and stream tool validation. Updated project-owned prose to British spelling while preserving protocol and library identifiers.
- Refreshed 23 interface images and the routing overview, including providers, command suggestions, sessions, stats, compaction and the yellow approval panel. All decisions, replies, timings and usage in the gallery are illustrative; no live model request or tool execution was used for those captures.

## Verification

The existing suite passed with `go test -race ./...` before changes. The full suite and targeted regression checks passed again after the fixes and file split. Local HTTP mocks covered direct DeepSeek, all three OpenCode Go protocols, tool/reasoning history replay, provider key changes, cancellation, usage accounting, session resumption and compaction failures. Additional checks covered transcript controls, detached native history, context/settings concurrency, stale metadata replies, oversized native arguments, duplicate calls, named pipes, symlinks and bounded private-file reads.

The rebuilt executable passed isolated CLI and real PTY terminal smoke checks: single-role routing, invalid arguments, startup, settings save, input style change, stats, session browsing, missing-key failure, saved-turn resumption and clean exit. Saved config and session files were verified as 0600. Screenshot generation checked terminal row and width bounds; all 24 PNG files were decoded and verified, with representative captures inspected visually.

As requested in the previous review, test files were removed after verification: 13 existing files, four temporary security/concurrency regression files and one temporary showcase fixture. Verification copies were retained outside the project. Subsequent `go test ./...` checks package compilation only.

Final checks:

- `go build -o jev ./cmd/jev` passed.
- `go vet ./...`, formatting checks and `go mod verify` passed.
- `govulncheck` reported no known vulnerabilities.
- CLI/TUI smoke checks passed.

Live authenticated provider completions were not exercised. DeepSeek metadata and OpenCode Go endpoint/session requirements were checked against their provider documentation; the Go model limits remain a dated bundled catalog snapshot. This review does not establish measured routing speed, accuracy or production readiness.

**Historical limitation of the earlier showcase, superseded by the implementation below:** the harness then had no operating-system sandbox. Approved tools can access local files and execute commands, and YOLO mode skips approval. Keep trials in a throwaway workspace with separate, limited API keys, then revoke every trial key and create new ones. Saved sessions include conversation and tool output; remove the private session files when discarding a trial. Unix process-group cancellation covers ordinary child processes; platform-specific handling and private-file flags are more limited outside Unix.

## Staged-workspace implementation — 30 September 2026

The harness now uses scoped staged file access and an offline Docker shell backend, central inspect/develop/autonomous policies, reviewed apply, conflict checks and selective undo. Provider policy is enforced before classification and fallback. Known credentials are redacted, progress is checkpointed during work, tools have durable lifecycle records, and resumed interrupted sessions require reconciliation. Added steering/follow-up input, attachments/search, manual compaction, session forks/lifecycle commands, CLI diagnostics, optional offline Go dependency preparation and a real-request evaluation runner.

The regression suite is retained from this point onward. The prior findings above about removed tests and unrestricted host tools describe the earlier showcase, not the current implementation. The root application already intercepted Ctrl+C correctly; lower-level handlers and OS signal handling now use the orderly shutdown path too.

Verification for this implementation covers race-enabled regressions, package build, vet, module integrity and live Docker checks of blocked host paths, absent credentials, blocked outbound network, staged writes, nonzero command results and timeout handling. The full project suite also passes offline inside the dependency-prepared sandbox; compile/test execution uses bounded executable tmpfs. Terminal smoke checks cover commands, ephemeral cleanup and Ctrl+C/SIGTERM shutdown. CI retains both boundary and offline project checks. Live authenticated model completions and representative routing benchmarks remain separate work. SECURITY.md documents the remaining boundary limits, including non-atomic multi-file apply, races with concurrent external editors, plaintext private persistence and reported-cost budgets.
