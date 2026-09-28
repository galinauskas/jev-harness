# Jev model routing showcase

A small terminal harness for trying **Jev as a model router**. You give it a task, Jev chooses a role, and the harness sends the task to the model assigned to that role. The point of this project is to see the routing decision, not to build a production coding assistant.

> **Test purposes only. Do not use this harness for real coding work.** It can read and change local files and run shell commands when you approve a tool call. `/yolo` skips those approvals. Use a throwaway workspace and a separate, limited API key. **After trying it, revoke that key and create a new one.**

![Four illustrative routing outcomes in the terminal interface](screenshots/routing-overview.png)

## What the showcase demonstrates

| Task | Jev's routing result | What to notice |
| --- | --- | --- |
| A short, focused question | `basic` → `openai/gpt-4o-mini` | A lightweight model handles a small request. |
| A task spanning several packages | `complex` → `anthropic/claude-sonnet-4.5` | A more capable model handles a broader request. |
| An unclear request | Default role after low confidence | A confidence threshold gives the router a predictable fallback. |
| A manually pinned role | The selected role | You can override automatic routing for a turn. |

The main learning outcome is that **Jev can be a very fast and accurate model router** for this kind of role selection. This repository shows the decision flow and the situations it is meant to handle. It is **not a benchmark**: the screenshots below use illustrative task text, decisions, confidence values, responses, timings, and usage figures. They were rendered in the app without live OpenRouter calls. Run your own trials before drawing conclusions about speed or accuracy for your tasks.

## How routing works

1. Define roles in the settings screen. Each role has a plain English description and an OpenRouter model ID.
2. For each new message, the harness asks Jev which role best fits the task. Recent conversation text provides context.
3. If Jev's confidence is below the chosen threshold, the harness uses the default role.
4. The selected model handles the message. The terminal shows the role, confidence, model, and any tool approval requests.

You can also pin a role with `/role <name>` or return to automatic routing with `/role auto`. Tab cycles through the available roles.

## Screenshots

The images are **illustrative captures of the interface**, not evidence of measured model performance or live API responses.

| Routing | Controls |
| --- | --- |
| [Small task → basic](screenshots/02-auto-routing-and-code.png) | [Tool approval](screenshots/06-tool-approval.png) |
| [Multi-package task → complex](screenshots/03-complex-task-routing.png) | [Roles list](screenshots/08-roles-command.png) |
| [Low-confidence fallback](screenshots/04-confidence-fallback.png) | [Routing settings](screenshots/12-routing-settings.png) |
| [Pinned role](screenshots/05-pinned-role.png) | [More screenshots](screenshots/README.md) |

## Try it

You need Go 1.26 or newer and an [OpenRouter API key](https://openrouter.ai/keys). The models in the starter configuration are examples; choose models available to your account in `/settings`.

```sh
git clone https://github.com/galinauskas/jev-harness.git
cd jev-harness
export OPENROUTER_API_KEY='your-temporary-key'
go run ./cmd/jev
```

Enter a small request, then a broader one, and compare the displayed routing choices. `/roles` shows the role definitions, `/settings` lets you edit them, and `jev route "your task"` prints a routing decision without starting the terminal interface after you build the binary:

```sh
go build -o jev ./cmd/jev
./jev route "Explain this Go function"
```

The terminal can also save a key in its private configuration file, but an environment variable is easier to discard after a trial. Never commit a key or paste one into screenshots. When finished, revoke the temporary key in OpenRouter and issue a new key if you continue using the service.

## Scope

This harness exists to make Jev routing easy to inspect. It includes a chat interface and local file and shell tools so you can see an end-to-end turn, but it has no operating-system sandbox. Approving a tool call allows it to act on your machine. Avoid real projects and sensitive files, and keep `/yolo` off during trials.

The code review and verification notes are in [REVIEW.md](REVIEW.md). The repository deliberately contains no test files.
