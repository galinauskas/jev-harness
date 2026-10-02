# Project review, 2 October 2026

**Not recommended for use.** Jev `v0.2.0` remains an experimental terminal agent for disposable trials. The default local shell has host filesystem and network access. Passing the checks below does not establish live provider compatibility, routing quality or safe use on normal projects.

This review covers commit `92c88ee` and the documentation and gallery refresh that follows it. I reviewed the changes since the previous review at `bc96e28`, including provider selection, slash-command tables, Herdr reporting, CLI session restore and mouse scrolling. I also rechecked the earlier findings against the current CLI, redactor and recovery code. This is a source review with local checks, not an independent security audit. This update changes documentation and render fixtures, not runtime behaviour.

## Open findings

### P1. Two CLI paths omit the saved Brave key from redaction

The [route command](cmd/jev/main.go#L132) passes saved OpenRouter, DeepSeek, OpenCode Go and Exa keys to `redact.New`, but omits `cfg.BraveAPIKey`. The [dependency-preparation call](cmd/jev/main.go#L83) makes the same omission when calling `PrepareSandbox`.

If a Brave key exists only in the saved config and appears in a route prompt, the CLI can send it to the OpenRouter classifier. If it appears in `go.mod` or `go.sum`, dependency preparation can miss it and pass that file to the Docker build. Environment keys remain covered by the redactor's environment scan. The main agent, workspace snapshot and saved transcript paths include the saved Brave key.

Pass `cfg.BraveAPIKey` in both CLI paths and add regression coverage for a config-only key. Keep credentials out of prompts and lockfiles. This finding follows the call sites and redactor implementation. No real credential was sent during review.

### P2. CLI doctor overstates routing data transmission

[Doctor](cmd/jev/main.go#L96) always says automatic routing sends the prompt and recent context to OpenRouter. A [single configured role](internal/router/router.go#L91) bypasses classification. The diagnostic should describe the configured routing path.

The earlier search-provider diagnostic finding is resolved. Doctor now uses `WebSearchProvider` and `WebSearchStatus`. A fresh CLI check confirmed that a Brave-only search setup reports `brave` and key availability without printing the key.

### P2. Recovery acknowledgement does not require a diff review

The TUI blocks ordinary task submission and apply after resuming an interrupted session. However, [the recovery handler](internal/tui/safety_commands.go#L107) clears the flag without checking whether `/changes` ran. A user can acknowledge recovery and submit another task without inspecting partial local-shell changes. Completed tools are not automatically replayed. Apply still requires its separate current-diff review.

Decide whether recovery must enforce a prior diff review. The README describes `/recover` as acknowledgement and tells users to inspect `/changes` first.

## Changes checked in v0.2.0

The Providers tab selects a global default `web_search` provider, Exa or Brave. Chat providers come from configured roles. The former global and per-project provider restrictions no longer apply. Loading an older config ignores those fields, and saving drops them. Regression tests cover that migration. Multiple unpinned roles still use the OpenRouter classifier.

Slash-command results, confirmations and errors use bordered tables. Role and usage tables adapt to narrow terminals. Diff output wraps and retains the review-size limit before granting apply. Saved sessions remain selectable rows.

Herdr reports `idle`, `working` and `blocked` through its local CLI. Reports coalesce pending changes, run sequentially with bounded timeouts and use literal argument arrays. Exit requests release. Reporting failures do not stop the TUI. Resume arguments contain session identity, budgets and a permission mode, without prompts, tool arguments or keys. Autonomous mode resets to develop for automatic restore. Sessions from another project and ephemeral saved-session restores are rejected.

The installed Herdr 0.9.1 CLI passed state and release checks against an isolated mock socket. It rejects resume arguments, which the reporter handles with a state-only retry. Automatic restore needs Herdr 0.9.2 or later and `jev` on `PATH`. A real Herdr restart and saved-session restore were not tested.

The root view enables terminal mouse reporting. Wheel events scroll chat while idle, streaming or awaiting approval, and navigate settings, help and session pickers. Tests check that scrolling preserves drafts and that stream refresh preserves the scroll position. A PTY smoke test confirmed mouse reporting, wheel-triggered redraws and orderly exit. Physical touchpad gestures in a live Herdr pane were not tested.

No additional finding emerged from these changed paths during this pass. The findings above remain open.

## Execution and persistence limits

File tools use a private staged copy and Go's filesystem root boundary. Inspect mode denies model-requested edits and commands. Develop asks for approval. Autonomous skips per-call approval. Local commands start in the staged copy but inherit the host environment and can access host paths. Staging does not isolate the shell.

Docker is experimental and off by default. When enabled, commands run offline in constrained containers. The executor validates returned archives before importing changes. An unavailable engine or image causes an error without a local-shell fallback. Explicit image builds and Go dependency preparation use network access.

Apply checks the reviewed diff and project conflicts. Undo refuses to overwrite later edits. Multi-file apply is not atomic, and concurrent external edits can race with filesystem operations. Private plaintext sessions retain messages, snapshots, logs and undo data. Redaction filters known values, not every possible secret. Brave's unavailable per-request dollar cost stops subsequent requests when a cost budget is active.

## Verification run

| Check | Result |
| --- | --- |
| `go test -race -count=1 ./...` | Passed across all packages, without cached test results |
| `go vet ./...` | Passed |
| `go mod verify` | Passed |
| `go build -o jev ./cmd/jev` | Passed; local executable rebuilt |
| `gofmt` check | Passed |
| Isolated CLI doctor and invalid-resume checks | Passed |
| PTY startup, wheel events and exit | Passed with disposable config and provider access blocked |
| Installed Herdr 0.9.1 CLI with temporary socket | Passed idle, working, blocked, release and legacy resume rejection checks |
| Current TUI render fixtures | Passed 112-column and 34-row bounds for 30 states |
| PNG generation | 31 images decoded and verified, including the routing overview |
| Live Docker checks | Could not run; the local OrbStack Docker socket was absent |

The gallery uses the application's renderer with deterministic demo state. It makes no model or search requests. Routing choices, confidence, answers, tool output and usage are illustrative. The staged diff and apply captures execute those operations on disposable files. The scrolling capture sends wheel events to the model. All captures were inspected in a contact sheet, with provider settings, command tables, the version banner and scrolling also checked at full size.

The Docker preflight failed before the live tests could start. Earlier isolation, failure, timeout and compact-output results are not fresh verification. The full offline project suite inside Docker was also not run.

Not run in this update were authenticated provider completions, billable search, routing evaluation, a vulnerability scan, cross-platform trials or real Herdr automatic restore. Local mock tests do not establish compatibility with live authenticated services.

## Work needed before reconsidering use

Fix the CLI credential-filter and routing-diagnostic gaps. Decide whether recovery acknowledgement must enforce a prior diff review. Exercise authenticated completions and tool continuations for each supported provider and protocol with limited trial keys. Compare held-out routed tasks with a fixed-model baseline for task success, latency, cost and accounting completeness.

Rerun Docker checks when the daemon is available. Test Herdr automatic restore with a supported release and check physical wheel and touchpad input in the terminal used for trials. Keep the local-shell access warning visible and continue checking cancellation, recovery and staged conflict handling.
