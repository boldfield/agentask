---
name: agentask-breakdown
description: Use to turn an idea or a new feature into an Agentask board — collaboratively brainstorm the design, formalize it to a doc, create or locate the repo, decompose into model-pinned bite-size tasks, and register the project/document/tasks via the Agentask API. The skill proposes and takes positions but STOPS for the human's decision at every design choice, task boundary, and task's content; it never finalizes alone. Triggers on requests like "let's break this down for the board", "decompose this feature into Agentask tasks", or "scaffold a new project on Agentask".
---

# Agentask design + breakdown

Drive the collaborative workflow that turns intent into an executable Agentask board:
**brainstorm → formalize a design doc → (greenfield) create the repo → decompose into
bite-size, model-pinned tasks → register project/document/tasks via the API.** A model-pinned
fleet then drains the board — Haiku implements, Opus reviews, the human merges.

## The one rule that overrides everything

**Propose, take a position, then STOP for the human's decision.** At every design choice,
every task boundary, and every task's spec you put forward a concrete recommendation with a
one-line rationale — then you wait for the human to confirm or override. You **never** finalize
the design, the task list, or any task's content on your own. This is collaborative authorship,
not autonomous generation. When in doubt, surface the choice; do not absorb it.

## Phase 0 — Configuration & mode

- `AGENTASK_URL` and `AGENTASK_TOKEN` must be set (the running Agentask instance + bearer token).
  Confirm both; if missing, ask the human. Every API call sends `Authorization: Bearer $AGENTASK_TOKEN`.
- Pick the **mode**, asking if it isn't obvious:
  - **Greenfield** — a new project needing a new repo + a `design` document.
  - **Feature-on-existing** — a new capability on an existing Agentask project; a `feature_spec`
    document, no new repo.

**Endpoint shapes** (get these right): list/create under a project at
`$AGENTASK_URL/projects/$PROJECT/tasks` and `.../documents`; every per-task call is at the ROOT,
`$AGENTASK_URL/tasks/<id>/{promote,transition}`. Prefer `scripts/agentask.sh` over hand-built
curl — inline `jq` quoting is error-prone, and the script keeps payloads correct.

## Phase 1 — Brainstorm the design (collaborative)

Iterate the problem and the shape of the solution *with* the human. Challenge assumptions, name
failure modes, rank approaches and state the trade-offs — take positions. But settle nothing
alone: each design choice is the human's call. **Write no code here** — this is intent and shape.

Output: shared agreement on the problem, the solution approach, the scope **and explicit
non-scope**, and the acceptance criteria that will define "done."

## Phase 2 — Formalize the design document

Turn the agreed design into a prose document — greenfield → `DESIGN.md`; feature-on-existing → a
feature-spec doc. It must contain:

- Problem / motivation.
- Goals and **non-goals** (the scope boundary).
- The behavior — what it does.
- **Acceptance criteria = the stopping condition**, concrete and testable.
- Constraints and gotchas.
- Test expectations (unit / integration / e2e, as the work warrants).

**Prose only, no code.** The human approves the document before anything is created.

## Phase 3 — Repo (greenfield only)

Behind a pluggable forge seam, **GitHub/`gh` is first-class.** `scripts/create-repo.sh` handles
`git init` → create the remote → commit the document. For feature-on-existing, skip this — locate
the existing repo and its Agentask project instead.

## Phase 4 — Decompose (the heart, collaborative)

Break the design into tasks. For **each** task, propose and STOP for the human:

- **Title** + a **prose spec** carrying: intent, constraints/gotchas, **pattern pointers**
  (`file:line` into existing code), and acceptance criteria.
- **Dependencies** on other tasks (by key).
- A proposed **model**, with a one-line rationale.
- An **`agent_merge`** suggestion (default `false`).

Non-negotiable decomposition rules:

- **NO code in a spec.** The spec says *what* and *why*; the implementer writes the *how*. A spec
  that contains code reduces the implementer to a paste buffer — that is contrary to the job.
- **Decompose-to-executor: every coding task is Haiku-sized — landable in one pass.** If a task is
  too big for Haiku, decompose it **finer**. NEVER escalate to a bigger model. Coding is Haiku;
  Opus reviews and gates, it does not implement.
- **Dependency-order to serialize file overlap.** Two tasks that touch the same file must be
  ordered by a dependency, never left concurrent — that is the merge-conflict trap. Tasks can have
  multiple dependencies, but the graph must be a **DAG**: a cycle (A→B, B→A) leaves both tasks
  permanently unclaimable, and the server rejects only self-deps, not cycles — so catch them here.
- Keep each spec to one change; prefer a `file:line` pattern-pointer over prose where it says more.

## Phase 5 — Register + hand off

Using `scripts/agentask.sh`:

1. Create the project (greenfield) or confirm the existing one.
2. Create the document (`kind` = `design` | `feature_spec`, with its repo ref).
3. Bulk-create the tasks — dependencies by intra-batch `key`, plus `model` and `agent_merge`.
4. **Promote milestone-by-milestone — NOT the whole backlog at once.** There is no `ready→backlog`
   demote; to halt a promoted task, transition it to `blocked` (reversible — `blocked→ready`
   re-readies it and clears its stale lease) or `failed` (terminal). Promote the first milestone,
   then the next once its predecessor has merged — this is a **human checkpoint** between milestones
   (review milestone 1's approach before milestone 2's leaves auto-flow), NOT a dependency-safety
   measure: the `claimable` filter already gates every task on its deps being `done`, regardless of
   what's been promoted.

Report the project id, document id, and task ids, and tell the human how to point the fleet at the
board (`AGENTASK_PROJECT=<id>` + the worker/reviewer loops).

## The gates you do not cross

- The human owns the **merge gate** — tasks default `agent_merge=false`; review workers approve but
  never merge.
- You never finalize a design choice, a task boundary, or a task's spec alone.
- Coding is Haiku; specs carry no code; same-file tasks are dependency-ordered.
