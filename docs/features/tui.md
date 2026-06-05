# Feature: Agentask TUI

Status: feature spec, 2026-06-05
Kind: feature_spec (for project Agentask)

## What this is

A minimal terminal UI for Agentask — a keyboard-driven board you live next to in the
terminal. It fixes the one ergonomic hole in the system: today the human side of the loop
is `curl | jq`. The TUI lets a human **see the board** (what's in backlog, being worked,
waiting for review) and **act on it** (promote backlog → ready; later, approve/reject
reviews) without leaving the terminal.

It is **just another client of the existing HTTP API** — the same API the Haiku agents
drive. It holds no state of its own and talks to no database directly. This re-validates
the API-only design: humans and agents drive the same single source of truth.

## Goals (v1)

- Pick a project (`GET /projects`).
- Render the board as five columns — `backlog`, `ready`, `in_progress`, `review`, `done` —
  from a single `GET /projects/{id}/tasks` call, bucketed by state client-side.
- Show enough per card to be useful: title; and for `in_progress`, the `assignee` and a
  lease countdown (so you can watch agents work).
- Keyboard navigation across columns and cards.
- **Promote** a selected `backlog` task to `ready`.
- Refresh: poll on a short interval, plus a manual refresh key.

## Non-goals (v1)

- No web UI. Terminal-only, keyboard-driven.
- No live push/streaming — poll for v1 (an SSE endpoint can come later).
- No task creation/editing in the TUI (composing a multi-line spec in a TUI is fiddly;
  shell out to `$EDITOR` if added later).
- No event timeline yet (needs a `GET /tasks/{id}/events` API endpoint that does not exist).

## Stack & architecture

- **Go + Bubble Tea** (Charm): `bubbletea` for the loop, `lipgloss` for styling, `bubbles`
  for list/viewport widgets. The de-facto Go TUI stack; kanban examples exist to crib from.
- New binary `cmd/agentask-tui` (builds to `bin/agentask-tui`); add a `tui` Makefile target.
- An **API client** package (e.g. `internal/tuiclient`) wrapping the HTTP API, configured
  from `AGENTASK_URL` + `AGENTASK_TOKEN` (same env the agents use). It defines its own small
  response structs (it is an external client; it does not import `internal/store`). Methods:
  `ListProjects`, `ListTasks(projectID)`, `PromoteTask(id)`, and later `ReviewTask`,
  `TransitionTask`.
- Polling: a Bubble Tea tick command refetches every ~2–3s; a manual refresh key forces it.

## Keybindings (target)

- `↑/↓` move within a column, `←/→` move between columns, `enter` open task detail.
- `p` promote (backlog → ready), `a` approve, `x` reject (these last two land with reviews).
- `r` refresh, `q` quit. A help line shows the active bindings.

## Future (post-v1, several gated on API work)

- **Review/approve from the TUI** — the highest-value addition: the `review` column shows
  PR links; `a` approves (`POST /tasks/{id}/review` approve + `POST /tasks/{id}/transition`
  done), `x` rejects (`transition` → ready). Makes the human gate a keystroke.
- Task **detail pane** — spec, links, deps, result, timestamps.
- Event **timeline** — requires a new `GET /tasks/{id}/events` API endpoint first.
- **Live** updates via an SSE/stream endpoint instead of polling.
- Block/fail from the TUI (the `transition` endpoint already supports it).

## Task breakdown

Tightly coupled UI code (one Bubble Tea model), so the tasks are a **linear chain** — each
builds on the previous, one claimable at a time:

1. **TUI-1 — scaffold + API client + project picker.** `cmd/agentask-tui`, the `tuiclient`
   package (config from env; `ListProjects`, `ListTasks`, `PromoteTask`), Bubble Tea
   skeleton that connects and lists projects; `q` quits; `tui` Makefile target. *(no deps)*
2. **TUI-2 — board view + nav + polling.** Selecting a project renders the five columns
   from one `ListTasks` call; cards show title, and assignee + lease countdown on
   `in_progress`; arrow-key navigation; periodic + manual refresh. *(deps: TUI-1)*
3. **TUI-3 — promote action.** `p` on a selected `backlog` task → `PromoteTask` → refresh,
   with error surfacing on failure. *(deps: TUI-2)*
4. **TUI-4 — review actions.** In the `review` column, show the PR link; `a` approves
   (review approve + transition done), `x` rejects (transition → ready), with a confirm
   prompt. *(deps: TUI-3)*
5. **TUI-5 — task detail pane.** `enter` opens a detail view (spec, links, deps, result,
   timestamps); `esc` returns to the board. *(deps: TUI-4)*

## Acceptance (feature-level)

- `agentask-tui` builds, connects to a live Agentask via `AGENTASK_URL`/`AGENTASK_TOKEN`,
  shows the board for a chosen project, and can promote a backlog task — verified against
  the deployed instance.
- It uses only the public HTTP API (no DB access, no `internal/store` import).
