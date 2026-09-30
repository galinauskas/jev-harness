# Security boundary

**Not recommended for use.** This project is experimental. Use disposable workspaces and limited trial credentials. The [current review](REVIEW.md) records open findings and verification limits.

Treat repository content, model responses and generated commands as untrusted. Prompt instructions are defence in depth; capability enforcement does not depend on the model obeying them.

File tools access a private staged copy through `os.Root`, reject absolute paths and traversal, and refuse links and protected paths. Shell commands run locally by default, starting in the staged directory with the host environment, filesystem access and network access. This is not a security sandbox: commands can access credentials and modify files outside the staged directory, and cancellation can leave partial staged changes. Develop mode requires command approval; inspect mode disables commands.

The optional Docker sandbox is **experimental** and disabled by default (`safety.docker_sandbox`). When enabled, shell tools run through a local Docker engine. They receive a read-only mount of that copy, create their writable workspace in bounded tmpfs, and return a bounded archive. Import rejects traversal, links, special files, duplicate names, oversized content and missing completion records. A failed or interrupted import preserves the previous staged state.

Tool containers run without network, privileged mode, added capabilities, host PID/network namespaces, credential mounts or the Docker socket. Their root filesystem is read-only; they use an unprivileged UID, process/memory/CPU limits, no-new-privileges and independent command timeouts. The harness force-removes each tool container after execution. Docker and the chosen image are trusted parts of this boundary; containers do not protect against kernel/runtime vulnerabilities.

The model has no apply, undo or configuration tool. File-tool and Docker-staged project mutations require an explicit user command after reviewing current diffs. Local shell commands can bypass this staging boundary through host paths. Apply validates all selected paths for conflicts before changing any file, uses atomic per-file replacements and persists undo records before mutations. Undo refuses to overwrite later edits. Multi-file apply is not an atomic transaction, and portable filesystem APIs cannot eliminate every race with another process editing the same file between validation and rename. Avoid simultaneous edits while applying; partial operations remain individually recoverable through their recorded undo data.

Filename filtering and known-value redaction do not identify all sensitive content, hard-link aliases, encoded secrets or secrets baked into custom images. Select a workspace that contains only files the model may see. The CLI route and dependency-preparation paths currently omit the saved Brave key from their known-value filter. Environment keys and the main agent paths remain covered. Keep credentials out of prompts and dependency lockfiles; see REVIEW.md. The optional `web_search` tool sends model-generated search queries and domain filters to the selected Exa or Brave HTTPS endpoint, using a host-held API key. It requires both a configured key and the selected service in the project/provider allowlist; it is read-only and available in inspect mode. Redirects are disabled, requests have a 30-second timeout and responses are capped at 1 MiB. Search results remain untrusted tool data. Brave does not report per-request dollar cost. Reported search costs count toward spending limits; missing costs stop subsequent requests when a cost limit is active.

Project/provider policies govern classification, answering and web search; permitted remote providers still receive the permitted conversation content.

Saved configuration credentials, sessions, source snapshots and undo records are private local files, not encrypted storage. Keep backups and exports under equivalent protection. Ephemeral mode prevents session persistence and cleans temporary staged state on orderly exit, but cannot guarantee cleanup after an OS crash or SIGKILL.

`jev sandbox build` and `jev sandbox prepare` are explicit operator-controlled setup operations with network access. The base build uses no repository context; Go preparation exposes only dependency lockfiles. Custom images must supply the trusted shell, coreutils timeout, tar and development tools, contain no credentials, and have dependencies ready for offline execution. Do not let repository instructions or model output choose an image or Docker endpoint.

Cost limits use provider-reported data and stop subsequent requests. Hard financial caps require provider-side enforcement. Context windows and token estimates may be imperfect; errors must not be interpreted as permission to broaden execution or replay completed tools.

Local shell execution targets macOS and Linux. The experimental Docker backend requires a local Docker socket. Other platforms and model/provider changes require their own verification. Live provider task quality is evaluated separately from boundary tests.

Report security-sensitive findings privately to the maintainer through an available private reporting channel. Include the affected version, reproduction using disposable files, observed access and the expected boundary; do not include live credentials.
