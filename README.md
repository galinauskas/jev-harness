# Jev harness

A terminal coding harness that uses **Jev to route each user turn to a configured model role**. It supports OpenRouter, direct DeepSeek and OpenCode Go, with streamed replies, tool use, context compaction and resumable sessions.

Agent tools work in a **private staged copy**. File tools use Go's filesystem root boundary. Shell commands run locally in that copy by default, with normal host and network access. Use `/changes` and `/apply` to review and apply staged edits. The optional **Docker sandbox is experimental** and disabled by default.

This is a development release. The retained tests cover the execution and recovery controls; live routing quality and authenticated provider compatibility still require trials with your selected models. Read [SECURITY.md](SECURITY.md) for the boundary and its limits.

## Start

Install Go 1.26+. From this repository:

```sh
go build -o jev ./cmd/jev
./jev doctor
export OPENROUTER_API_KEY='your-key'
./jev
```

To opt into the **experimental Docker sandbox**, toggle it with `x` in `/settings` → Appearance, or set `"docker_sandbox": true` inside the config's `safety` object. Install a local Docker runtime such as Docker Desktop, OrbStack or Docker Engine, then run:

```sh
./jev sandbox build
./jev sandbox prepare  # optional: preload this project's Go modules
./jev doctor
```

`sandbox build` creates the local tool image from the bundled Dockerfile using an empty build context. `sandbox prepare` downloads this project's Go modules using only root `go.mod` and `go.sum`; repeat it after dependency changes. These explicit setup commands use network access; sandboxed commands remain offline. Relative module replacements and other dependency ecosystems need a trusted custom image with dependencies preinstalled. When enabled, unavailable engines or images fail clearly without falling back to the host shell. Remote Docker engines are rejected.

`doctor` and `/doctor` report the selected shell backend. They check Docker readiness only when the experimental sandbox is enabled.

## Work and review

| Command | Behaviour |
| --- | --- |
| `/mode inspect` | Read, list and search staged files; use configured web search; mutations and commands are denied |
| `/mode develop` | Inspect without prompts; approve proposed edits and shell commands |
| `/mode autonomous` | Allow tools with the configured shell backend without per-call prompts |
| `/changes [path]` | Review staged diffs, optionally for one file |
| `/apply [path]` | Apply the currently reviewed changes, with conflict checks |
| `/undo <path>` | Restore an applied file if no later project or staged edits would be overwritten |
| `/discard` | Discard staged changes and refresh the copy from your project |
| `/attach <path>` | Put a staged file into your draft; paths may contain spaces |
| `/compact` | Summarise older context now |
| `/recover` | Acknowledge interrupted work after reviewing its state |
| `/settings` | Configure roles, models, credentials and interface preferences |
| `/stats` | Show the session ID, models, usage and reported cost |

Settings use text-only tabs for Appearance, Routing, Context, Providers and Roles. Use ←/→ to switch tabs, ↑/↓ to select a row, Tab/Shift+Tab to jump between sections (or rows on a single-section page), and Enter/Space to edit or toggle. Tabs remember your selection. Status items toggle directly in Appearance; model, threshold and masked key edits open only the selected field. The bottom help line describes the selected setting. In Roles, use `n` to add, `d` to delete, and `D` to choose the default. Deleting a role requires Enter to confirm; Esc keeps it. Routing also has a default-role chooser. Role edit forms use Tab/Shift+Tab to move between fields and ←/→ or Space to choose a provider. Enter saves and Esc cancels; Esc from the settings list returns to chat. Press `?` for all shortcuts.

The default mode is `develop`. `/yolo` remains an alias for toggling `autonomous`; it changes approvals, never the filesystem or network boundary. Elevated permissions reset when starting, resuming or forking sessions, or changing configuration. `/apply` always requires a current review, even in autonomous mode. Inspect mode permits the user to explicitly apply previously staged work.

While an answer is running, Enter queues steering for the next model request and Alt+Enter queues a follow-up after the task. Shift+Enter adds a newline. Escape cancels the current turn. Ctrl+C, SIGINT and SIGTERM request an orderly cancellation and save before exit.

Use `@path` in a prompt for an explicit file attachment, or `/attach` for paths containing spaces. Attachments are limited to 64 KiB. `search_files` finds files by glob or searches literal text, with at most 100 matches.

The root `AGENTS.md`, when present in the staged copy, supplies repository guidance. Repository text, tool output and model responses cannot broaden the capability policy. Executable project extensions, MCP configuration and package scripts are not loaded automatically.

## Routing and data destinations

Each role defines its name, criteria, provider and model ID. Jev selects a role for each user turn; tool continuations stay on that model. `/role <name>` pins a role and `/role auto` restores automatic routing. `/roles` shows the mapping.

Automatic classification sends the current prompt and recent conversation text to OpenRouter. The selected answering provider receives the conversation context and tool results. One role or a pinned role bypasses classification.

Configure providers in `/settings` → Providers. Each provider has an On/Off toggle for the current project; API keys and the search-provider choice are saved in the same tab. Settings prevent disabling the last provider used by your roles. **Restore providers** enables all providers for the current project and repairs restrictions saved by older versions.

To use only DeepSeek, first define a role with provider `deepseek` in the Roles tab, then enable DeepSeek and disable the other chat providers in Providers. When OpenRouter is disabled, automatic classification is skipped and the enabled default role is used (or the first enabled role if the default is disabled). `/role <name>` still pins a specific role. Disabled providers are excluded from classification choices and fallbacks. Project provider settings are saved under the canonical project directory in the user configuration; repositories cannot change them.

A saved key takes priority over its environment variable. Configure masked key fields in `/settings`, or use:

```sh
export OPENROUTER_API_KEY='...'
export DEEPSEEK_API_KEY='...'
export OPENCODE_GO_API_KEY='...'
export EXA_API_KEY='...'  # optional Exa web search
export BRAVE_API_KEY='...'  # optional Brave web search
```

Models with tools enabled can call `web_search` using [Exa](https://exa.ai/docs/reference/search) or [Brave Web Search](https://api-dashboard.search.brave.com/api-reference/web/search/get). Choose `/settings` → Providers → Search provider, and set the matching masked API key there or export `EXA_API_KEY` / `BRAVE_API_KEY`. Saved `exa_api_key` / `brave_api_key` values override the environment. The `search_provider` config field accepts `exa` or `brave`; older configs default to Exa. Only the selected provider is used, without automatic fallback.

Search returns titles, URLs, publication dates when available, and highlights/snippets, defaults to five results (maximum ten), and supports `include_domains` / `exclude_domains`. Brave converts bare domain filters to search operators and limits the complete query to 600 characters and 75 words. It works in all modes, including inspect, without Docker.

The selected provider receives the search query and domain filters through a fixed HTTPS endpoint in the host process. Search results are untrusted data; the model should cite their URLs. Credentials stay outside tool arguments and containers. Provider toggles in `/settings` → Providers control both search services. Enable the selected provider there; settings mark disabled keys clearly. Existing project restrictions remain visible in the toggles and can be repaired with **Restore providers**. The system prompt describes the available search capability and directs models to use it for current information such as weather. Search usage is recorded under `exa:web_search` or `brave:web_search` and counts toward the turn spending budget. Brave does not report per-request dollar cost; unknown cost stops further requests when a cost budget is enabled.

Saved credentials remain in a private 0600 config file. That file and host credential directories are outside the tool environment. Known provider credentials and secret-valued environment variables are redacted before persistence and transmission. This does not discover every secret; keep sensitive data out of the selected workspace.

Role JSON supports optional `context_window`, `output_limit` and `disable_tools` overrides. Unknown model limits are shown as unavailable. Compaction reserves output headroom and commits only complete, smaller summaries. A recognised context-overflow rejection can trigger one compaction retry; completed tools are never automatically replayed. Explicit transient HTTP rejections receive at most two retries. Transport errors and partial streams are preserved without automatic replay.

Enable **compact command output** in `/settings` with `b`, or set `"compact_command_output": true` in config. This setting works with local commands and the experimental Docker sandbox; this optional setting keeps their output from filling the session context and terminal transcript. Each result includes a roughly 2 KiB preview of the beginning and end, completion status, and a log ID. The model can use `read_command_output` to fetch selected byte ranges (default 2,000 bytes, maximum 4,000 per call). Known credentials are redacted before logs are saved outside the staged project; logs retain up to 1 MiB per command and report truncation. Logs survive resume and fork and share the session's deletion/ephemeral lifecycle. The setting defaults to off for compatibility; disabling it restores standard command output. Command output formatting does not change the selected shell backend or approval rules.

## Sessions and privacy

Sessions contain conversation messages, model/provider provenance, usage totals and a rendered transcript. Progress checkpoints and an event journal are written during work, including before and after tools. An interrupted session requires `/changes` and `/recover` before another task or apply. Pending calls are marked interrupted, not executed again on resume.

- `/session` opens saved sessions for the current project.
- `/session search <text>` filters by title or ID.
- `/name <title>` renames the current session.
- `/fork` copies the conversation and staged files into an independent session. It does not rewind project files.
- `/session delete <id>` deletes the session, journal, staged files and undo records.
- `/session export <new path>` creates a private JSON export. Inspect it before sharing.
- `/session new` or `/clear` starts fresh while preserving the previous saved session.

Files live under `$XDG_CONFIG_HOME/jev-harness` or `~/.config/jev-harness`. Staged copies and undo records are private but can contain proprietary source. Set `safety.retention_days` to prune old completed sessions, or leave the default `0` for manual retention. Retention deletes the associated staged state and undo records too.

Use `./jev --ephemeral` to avoid persistent conversation/session files. Its temporary staged copies are removed on orderly exit; an OS crash or forced termination can leave private temporary files that need manual removal. Saved-session browsing and export are disabled in this mode.

## Limits

```sh
./jev --mode inspect
./jev --output-tokens 4096 --token-budget 100000 --time-budget 300
./jev --cost-budget 0.50
```

Defaults are 8,192 output tokens per request, 200,000 tokens per turn, 600 seconds per turn, and 25 tool rounds. Missing usage reserves estimated input plus maximum output rather than giving a request a free budget. Token estimates are approximate; provider-side limits remain useful.

The USD budget stops further requests when reported cost reaches the limit or cost is unavailable. It cannot guarantee the price of an in-flight request. Use provider-side spending limits for a hard financial cap. Stats distinguish reported cost from requests whose cost is unavailable.

Snapshots are limited to 128 MiB, 10,000 files and 2 MiB per file. Symlinks, credential filenames/directories, `.git`, `node_modules`, `vendor`, `.cache`, the local `jev` binary, the harness's config directory and files containing known plaintext credentials are omitted. Choose a narrower working directory if the project exceeds these limits. The experimental Docker sandbox has 2 GiB RAM, one CPU, 128 processes, a 512 MiB workspace tmpfs and a 1 GiB temporary tmpfs. Commands have a maximum ten-minute timeout. Docker also has a container-side timeout if the harness dies.

## Verification and evaluation

Tests are retained in the repository and run in CI:

```sh
go test -race ./...
go vet ./...
go mod verify
JEV_DOCKER_TEST=1 go test -v ./internal/tools -run TestLiveDocker
JEV_DOCKER_PROJECT_TEST=1 go test -v ./internal/tools -run TestLiveDockerProjectChecks
```

CI includes a separate Docker isolation job. The live test checks host-path isolation, absent credentials, blocked outbound network and staged shell writes.

Evaluate routing against the configured default-role baseline:

```sh
./jev eval eval/cases.jsonl > results.jsonl
```

This makes real, billed provider requests. Each JSONL case contains `id`, `prompt`, optional sequential `followups`, and a `check` shell command. Each strategy gets a fresh ephemeral workspace. Checks use the configured shell backend: local by default, or the offline experimental Docker sandbox when enabled. Output reports completion, check success, selected models, latency, reported tokens/cost and accounting completeness. The included two cases demonstrate the format; they are not a representative benchmark. Build a held-out dataset covering your projects, debugging, ambiguous tasks and short contextual follow-ups before claiming an advantage over a fixed model.

The [screenshot gallery](screenshots/README.md) shows the earlier routing showcase. Its routing decisions, replies, timings and usage are illustrative, and its tool/approval screens predate the staged-workspace controls.

Exa and Brave request, error, cancellation, credential, settings selection and policy tests use local mock servers. To make one billable live search with your own key:

```sh
JEV_EXA_TEST=1 go test ./internal/tools -run '^TestLiveExa$' -v
```
