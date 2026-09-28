# Interface screenshots

These are illustrative captures of the terminal interface. Task text, Jev decisions, confidence scores, model replies, timings and usage figures are demo data; no live OpenRouter request was made for these captures. They show the routing flow, not measured speed or accuracy.

| Screenshot | What it shows |
| --- | --- |
| [Routing overview](routing-overview.png) | Basic, complex, default and pinned outcomes side by side. |
| [Welcome](01-welcome.png) | The starting chat screen. |
| [Basic routing](02-auto-routing-and-code.png) | A focused request sent to the basic role. |
| [Complex routing](03-complex-task-routing.png) | A broader request sent to the complex role. |
| [Confidence fallback](04-confidence-fallback.png) | An uncertain decision using the default role. |
| [Pinned role](05-pinned-role.png) | A role selected by the user. |
| [Tool approval](06-tool-approval.png) | A local action waiting for permission. |
| [Tool result](07-tool-output.png) | The result shown after a tool call. |
| [Roles](08-roles-command.png) | The configured role list. |
| [Settings](09-settings-overview.png) | The settings screen. |
| [Edit a role](10-edit-role-criteria.png) | The text Jev uses to decide when a role fits. |
| [Add a role](11-add-role.png) | Creating another role. |
| [Routing settings](12-routing-settings.png) | Model, confidence threshold and API key fields. |
| [Status line](13-status-line-settings.png) | Choosing visible status details. |
| [Shortcuts](14-settings-shortcuts.png) | Keyboard help. |
| [Multiline input](15-multiline-input.png) | A longer message draft. |
| [YOLO mode](16-yolo-mode.png) | Automatic tool approval, shown for completeness; avoid it in a trial. |

**Safety:** This harness is for tests, not real coding work. Use a throwaway workspace and temporary API key, then revoke it and create a new key after use.
