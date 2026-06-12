---
name: agentask-board
description: Use to SEE an Agentask board from an interactive session — render the task board by
  state (the TUI's columns, in text), show one task/project/document in detail, and watch work
  move. Read-only — it never changes board state (that's agentask-ops). Built for headless/sandbox
  use where no TUI is available. Triggers on "show me the board", "board status", "what's in
  flight?", "what's in review?", "anything blocked?", "show task <id>", "show the board for <project>".
---

# Agentask board (read-only visibility)

A conversational stand-in for the Agentask TUI. When you run the fleet headless — e.g. inside an
`sbx` sandbox where the terminal UI isn't available — this is how you get eyes on the board: it reads
the board over the API/CLI and renders it the way the TUI would, so you can see what's queued, in
flight, in review, and done without leaving the `claude` session.

## The one rule that overrides everything

**This skill only reads. It never mutates.** No promote, transition, archive, claim, approve, or
create — ever. If the human wants to *act* on what they see (unstick a task, approve, archive),
that's `agentask-ops`; hand off, don't reach for the CLI's mutating verbs here. Reading is always
safe; that's the whole point of keeping this skill clean.

## Phase 0 — Configuration

- `AGENTASK_URL` and `AGENTASK_TOKEN` must be in the environment; the `agentask` CLI reads them.
  If either is missing, ask the human.
- You need a project to focus on. Use `$AGENTASK_PROJECT` if set; otherwise run `agentask projects`
  and let the human pick, or render the multi-project overview (below).
- **Never print a token value** — not the bearer token, not a forge token.

## The board overview (all projects)

For "what's going on overall": `agentask projects --json` (add `--claimable` to see only boards with
claimable work). Render a compact table — project name, id (first 8), repo, and a quick count of
claimable work — so the human can pick where to look.

## The task board (one project) — render it like the TUI

Pull the tasks once: `agentask tasks --project <id> --json`, then group by **state**, in the TUI's
column order:

```
backlog → ready → in_progress → review → approved → done        (blocked / failed shown off to the side)
```

Render each column as a short list; lead with a per-column count. For each task show, at minimum:

- the **short id** (first 8 chars) and `[model]` badge and **title** — same as the TUI line
  (`<id8> [haiku] Implement the foo`);
- for `in_progress` and `review`: the **assignee** and the **lease** — and **flag an expired lease**
  (`lease_expires_at` in the past) as a likely-stuck task worth a closer look;
- for `review`: the `review_round` and how many reviewers have voted, if visible.

**Match the TUI's done-column filtering (important):** in the `done` column, **omit `kind=review`
and `kind=merge` tasks** — they're server-spawned bookkeeping, not deliverables. Show only
`implement` (and design/track) deliverables there, exactly as the TUI does. If the human asks, you
can report the hidden review/merge counts separately.

Keep it scannable: a human reading this in a chat window wants columns with counts and one line per
task, not a JSON dump. Summarize empty columns as "(none)".

## One task / document in detail

- A specific task: `agentask show <id>` — render its state, kind, model, assignee, lease (flag if
  expired), `review_round`/verdict, **links** (`pr` / `branch` / `commit` / `ci`), and dependencies.
  If a link is a PR, offer to surface its diff (`agentask diff <id>`).
- A project's documents: `agentask project <id>` / the documents listing — show kind, title, ref.

## Watching work move

- Re-render on request ("refresh", "what changed?") by re-pulling and diffing against what you last
  showed — call out tasks that changed state, got claimed, or whose lease expired.
- In a sandbox run (`sbx.sh`), the fleet's per-kind logs live under `$AGENTASK_HOME/logs/`
  (`workers.log`, `reviewers.log`, `server.log`). For "what is the fleet actually doing right now?",
  tail those alongside the board — each line is prefixed with the agent's slot id.

## When the board shows something wrong

If a render surfaces a problem — a `merge` task stuck `in_progress`, an expired lease that wasn't
reclaimed, a task ping-ponging in review — **say so and hand off to `agentask-ops`**. Diagnosing and
fixing is that skill's job (and it carries the guardrails for mutating the board). This skill points;
it does not act.
