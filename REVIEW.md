# Project review, 30 September 2026

Follow-up, 2 October 2026: provider allowlists and project toggles have been removed. Chat routing uses configured roles and web search uses a global default provider. CLI doctor now reports the selected search provider and its key availability. The observations below describe the earlier reviewed revision.

**Not recommended for use.** The current code has retained regression tests, staged file edits, reviewed apply and an optional offline Docker shell. The default local shell still has host filesystem and network access. This review does not establish live provider compatibility or a routing advantage.

This update replaces the earlier review, whose Docker-default and screenshot descriptions no longer matched the project. Reviewed commit `8412bc9` and the current CLI, provider configuration, routing, search integration, execution policy, staged apply/recovery, session handling and TUI. This was a source review and local verification, not an independent security audit. No runtime behaviour was changed for this documentation update.

## Open findings

### P1. CLI paths omit the saved Brave key from redaction

[The CLI route path](cmd/jev/main.go#L131) supplies OpenRouter, DeepSeek, OpenCode Go and Exa saved keys to `redact.New` for `jev route`, but omits `cfg.BraveAPIKey`. [The dependency-preparation call](cmd/jev/main.go#L78) makes the same omission when calling `PrepareSandbox`.

If a Brave key exists only in the saved config and appears in a route prompt, the CLI can send it to the OpenRouter classifier. If it appears in a dependency lockfile, the preparation check can miss it and pass that file to the Docker build. Environment keys remain covered by the redactor's environment scan. The main agent, workspace snapshot and saved transcript paths already include the Brave saved key.

Pass `cfg.BraveAPIKey` in both CLI paths and retain regression coverage for a config-only key. Until then, keep credentials out of prompts and lockfiles. This finding follows the call sites and redactor implementation; no real credential was sent during review.

### P2. CLI diagnostics report the wrong search provider

[CLI doctor](cmd/jev/main.go#L99) hardcodes Exa key and policy checks and prints `Web search: exa`, even if `search_provider` is `brave`. A Brave-only setup can therefore appear unconfigured. The same diagnostic always describes routing as sending data to OpenRouter, although pinned/single-role or disabled-classifier paths bypass classification.

Use `WebSearchProvider`, `ProviderKey` and the effective project policy when reporting diagnostics. Providers settings and the agent's search selection already use the selected service. The README calls out the current diagnostic limitation.

### P2. Recovery acknowledgement does not require a diff review

The TUI blocks ordinary task submission and apply after resuming an interrupted session. However, [the recovery handler](internal/tui/safety_commands.go#L102) clears the recovery flag without checking whether `/changes` ran. A user can resume, acknowledge and submit another task without inspecting partial local-shell changes. Completed tools are not automatically replayed, and apply still has its separate review check.

Require review of current staged state before recovery acknowledgement, or describe `/recover` as acknowledgement alone. The README now states the actual behaviour and tells users to inspect first.

## What the current implementation does

Roles define criteria, provider and model. Jev classifies each user turn among permitted roles; tool continuations stay on that model. Provider restrictions filter classification choices and fallback roles. Pinned roles bypass classification, and disabling OpenRouter uses an enabled default role. This is implemented routing behaviour, not evidence of better routing decisions.

File tools use a private staged copy and Go's filesystem root boundary. The execution policy denies edits and commands in inspect mode, asks for approval in develop mode and skips per-call approval in autonomous mode. Local shell commands begin in the staged copy but inherit the host environment and can access host paths. Staging is therefore not a shell sandbox.

Docker is experimental and disabled by default. When enabled, it runs commands offline in constrained containers and validates the returned archive before importing changes. Unavailable engines/images do not trigger a local-shell fallback. Explicit image build and Go dependency preparation use network access.

Reviewed apply checks the current diff and project conflicts. Undo refuses to overwrite later edits. Multi-file apply is not atomic, and concurrent external edits can still race with filesystem operations. Sessions retain private plaintext messages, snapshots, logs and undo data. Redaction filters known values; it cannot discover every secret.

Exa and Brave share the web-search tool. The selected provider must have a key and be permitted for the project. Brave's unknown per-request dollar cost stops further requests when a cost budget is active. Compact command output is optional and keeps bounded logs outside the staged project. Settings now group controls into five tabs.

## Verification run

| Check | Result |
| --- | --- |
| `go test -race ./...` | Passed across all packages after enabling loopback for local mock servers |
| `go vet ./...` | Passed |
| `go mod verify` | Passed |
| `go build -o /tmp/jev-review ./cmd/jev` | Passed |
| `TestLiveDockerIsolation` | Passed against the installed local runtime/image |
| `TestLiveDockerFailureAndTimeoutPreserveBoundary` | Passed |
| `TestLiveDockerCompactCommandOutput` | Passed |
| Current TUI render fixtures | Passed 112-column and 34-row bounds for 29 states |
| PNG generation | 30 images decoded and verified, including the routing overview |

The first restricted test attempt could not bind the local mock HTTP listener. The complete race suite passed with that environment restriction lifted. The Docker tests checked host-path isolation, absent provider credentials, blocked outbound network, staged writes, original-file preservation, nonzero exits, timeout handling and compact log retrieval. These tests use disposable workspaces.

The gallery renders actual TUI views with deterministic demo state. It makes no model or search requests. Routing confidence, answers, tool output and usage are illustrative. The staged diff and apply captures execute those operations on disposable files. All captures were inspected in a contact sheet, with settings, provider, approval and diff captures also inspected at full size. The renderer and fixture remain in the repository so the gallery can be regenerated.

Not run in this update: authenticated provider completions, billable search, routing evaluation, the full project suite inside the prepared Docker image, a new vulnerability scan, cross-platform trials or interactive PTY smoke tests. Prior results are not treated as fresh verification.

## Work needed before reconsidering use

Fix the CLI credential-filter and diagnostic gaps. Decide whether recovery acknowledgement must enforce a prior diff review. Exercise live authenticated completions and tool continuations for every supported provider/protocol with limited trial credentials. Run held-out routing tasks against a fixed-model baseline and compare task success, latency, cost and accounting completeness.

Keep the local-shell access warning visible. Continue testing staged conflict handling, cancellation and recovery, and verify the experimental Docker path on each supported platform. Passing the current suite does not make the default shell isolated or the project ready for normal coding work.
