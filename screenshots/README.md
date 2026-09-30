# Interface screenshots

These are illustrative captures of the terminal interface. Task text, Jev decisions, confidence scores, model replies, timings and usage figures are demo data; no live provider request was made for these captures. They show the routing flow, not measured speed or accuracy.

| Screenshot | What it shows |
| --- | --- |
| [Routing overview](routing-overview.png) | Basic, complex, default and pinned outcomes side by side. |
| [Welcome](01-welcome.png) | The starting chat screen. |
| [Basic routing](02-auto-routing-and-code.png) | A focused request sent to the basic role. |
| [Complex routing](03-complex-task-routing.png) | A broader request sent to the complex role. |
| [Confidence fallback](04-confidence-fallback.png) | An uncertain decision using the default role. |
| [Pinned role](05-pinned-role.png) | A role selected by the user, labelled pinned rather than a measured confidence. |
| [Tool approval](06-tool-approval.png) | A local action waiting for permission. |
| [Tool result](07-tool-output.png) | The result shown after a tool call. |
| [Roles](08-roles-command.png) | The configured role list. |
| [Settings](09-settings-overview.png) | The settings screen with routing, provider keys, roles and compaction. |
| [Edit a role](10-edit-role-criteria.png) | The text Jev uses to decide when a role fits. |
| [Add a role](11-add-role.png) | Creating another role. |
| [Routing settings](12-routing-settings.png) | Model, confidence threshold and masked keys for all three providers. |
| [Status line](13-status-line-settings.png) | Choosing visible status details. |
| [Shortcuts](14-settings-shortcuts.png) | Keyboard help. |
| [Multiline input](15-multiline-input.png) | A longer message draft. |
| [YOLO mode](16-yolo-mode.png) | Automatic tool approval, shown for completeness; avoid it in a trial. |
| [Command suggestions](17-command-suggestions.png) | Filtering slash commands while typing, with keyboard completion. |
| [Saved sessions](18-saved-sessions.png) | Browsing earlier conversations in the current working directory. |
| [Session stats](19-session-stats.png) | Reported tokens, costs, requests by model and last prompt usage. |
| [Context settings](20-context-settings.png) | Choosing a compaction percentage or disabling it. |
| [Context compaction](21-context-compaction.png) | A notice when earlier context has been summarised. |
| [Provider roles](22-provider-roles.png) | Role criteria mapped to OpenRouter, direct DeepSeek and OpenCode Go. |
| [Direct provider](23-direct-provider.png) | A pinned role using the direct DeepSeek backend. |

Every image was refreshed from the reviewed TUI on 30 September 2026. The caption beneath each capture identifies its illustrative data. The routing overview combines the same four rendered routing states.

**Safety:** This harness is for tests, not real coding work. Use a throwaway workspace and temporary API keys, then revoke them and create new keys after use.
