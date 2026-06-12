---
name: review
description: Use to drive the human review gate from inside an interactive `claude` session — show the queue of tasks awaiting a decision, show the diff for one of them, and, on the human's explicit instruction, record their verdict by running `agentask approve`/`reject`. The skill NEVER decides on its own — the human supplies the judgment, the CLI does the mechanics. Triggers on requests like "what's waiting for review?", "show me the review queue", "let me review the board", "show me the diff for <task>", "approve <task>", or "reject <task> because …".
---

# Agentask review (human gate)

A conversational wrapper for the **human review gate**. The sandbox's only interface is an
interactive `claude` session, so this skill is how a human inspects what is waiting, reads a diff,
and then — once they have made the call — records that verdict on the board. It surfaces the queue
and the diff so the human can decide, then executes the decision they hand back.

## The one rule that overrides everything

**The human decides; this skill only records.** It never forms its own opinion of a diff and never
approves or rejects on its own initiative. It runs `agentask approve`/`reject` **only** when the
human gives an explicit instruction to do so ("approve it", "reject it because X"). The skill is
the steward of the board state, not the reviewer: the CLI does the mechanics (transition, freeze,
cleanup), and the human supplies the judgment. If you are unsure whether the human has actually
decided, ask — do not guess and do not act.

## When to use it

- The human wants to see what work is awaiting a decision ("what's in the review queue?").
- The human wants to read the diff for a specific pending task before deciding.
- The human has decided and wants the verdict recorded ("approve it", "reject it, the tests are
  flaky").
- As the human-facing companion to the model-pinned fleet: Haiku implements, the Opus reviewer
  worker verdicts review-kind tasks, and the human drains the `approved` lane. This skill is how
  the human looks before they leap **and** how they record the leap once they take it.

## Phase 0 — Configuration

- `AGENTASK_URL` and `AGENTASK_TOKEN` must be set (the running Agentask instance + bearer token);
  the CLI reads them from the environment. If either is missing, ask the human.
- You need the **project id**. Use `$AGENTASK_PROJECT` if it is set; otherwise ask the human, or
  run `agentask projects` to list them and let the human pick.
- In `local_commit` mode (`AGENTASK_DELIVERY_MODE=local_commit`) the approve/reject mechanics touch
  the git repo, so the repo directory must be resolvable — `$AGENTASK_REPO` (or a `--repo <dir>`
  the human supplies). If it is unset when an action needs it, the CLI errors; ask the human.

## Phase 1 — Show the queue

Run:

```
agentask pending --project <project-id>
```

This lists every task in the **`review`** or **`approved`** state for that project — the work
waiting on a decision. The output is a table of `ID  STATE  KIND  TITLE` (truncated 8-char ids);
add `--json` for the full records. `review` rows are awaiting a reviewer verdict; `approved` rows
have passed review and await the human's merge.

Present this queue to the human and ask which task they want to inspect.

## Phase 2 — Show the diff

For the task the human chose, run:

```
agentask diff <task-id>
```

What it shows depends on the delivery mode:

- **`pull_request` mode** (default): prints the PR URL, then — if `gh` is on PATH — the PR diff
  (`gh pr diff <url>`), best-effort.
- **`local_commit` mode** (`AGENTASK_DELIVERY_MODE=local_commit`): shows the diff of the task's
  `commit` link against the base (`origin/main`). Add `--full` to show the entire commit instead
  of just the diff against base. The repo directory comes from `--repo <dir>` or, if unset,
  `$AGENTASK_REPO`.

Useful flags:

- `--full` — full commit rather than diff-against-base (local_commit mode).
- `--repo <dir>` — repository directory (local_commit mode; defaults to `$AGENTASK_REPO`).

Present the diff to the human and let them read it.

## Phase 3 — Record the human's decision

This is the action step. Run it **only** on the human's explicit instruction, and run **exactly**
the verb their instruction maps to — never substitute your own judgment for theirs.

### Approve

When the human says to approve a task (one in the **`approved`** state — it has passed reviewer
verdict and is on the human's merge lane):

```
agentask approve <task-id>
```

- The task must be in `approved`; `approve` errors otherwise.
- **`pull_request` mode**: transitions `approved → done`. (The human still does the actual PR merge
  out of band; the skill only records the state.)
- **`local_commit` mode**: transitions `approved → done` **and then freezes** — advances the MR
  branch `wi/<slug>` onto the per-item WIP branch `wip/<id>`, removes the item's worktree, and
  deletes the WIP branch. In this mode, **approve = freeze**: there is no separate merge step.

### Reject

When the human says to reject a task (one in **`review`** or **`approved`**), you must have a
reason from them — `--note` is required:

```
agentask reject <task-id> --note "<the human's reason>"
```

- Without `--abandon`, this transitions the task to **`ready`** for rework (the implementer
  re-claims it; a fresh review round spawns on resubmit).
- With `--abandon`, it transitions the task to **`failed`** instead, and in `local_commit` mode
  also cleans up the item's worktree and `wip/<id>` branch (the `wi/<slug>` MR branch is left
  intact). Use this only when the human wants to drop the work entirely, not send it back:

  ```
  agentask reject <task-id> --note "<reason>" --abandon
  ```

## The approve = freeze footgun (local_commit mode)

`approve` does the board transition **first**, then the git freeze. If the freeze fails *after* the
transition has already moved the task to `done`, the task is `done` but the branch is **not** frozen
— and re-running plain `agentask approve <task-id>` will fail with `task is in "done" state, expected
approved`, because the transition guard no longer matches.

The most common cause is the MR branch being checked out somewhere, which surfaces as:

```
MR branch wi/<slug> is checked out at <path>; cd out or run 'git checkout --detach' there, then re-approve
```

**Recovery — `agentask approve <task-id> --freeze-only`.** Clear the cause the message names (e.g.
`cd` out of that worktree or run `git checkout --detach` there), then re-run approve with
`--freeze-only`. That flag **skips the already-done transition** and retries **only** the freeze, so
the WIP branch lands on the MR branch and the worktree/WIP branch are cleaned up. Do not try to
re-approve without it — the state guard will keep rejecting you.

## The gates you do not cross

- You never form or impose your own verdict. You run `approve`/`reject` only to record a decision
  the human has explicitly stated, and you never invent a `--note` reason — it comes from the human.
- In `pull_request` mode you never run the actual PR merge yourself; `approve` records the board
  state, the human merges the PR.
- You touch the repo only through the `agentask` verbs (`diff`, `approve`, `reject`); you never
  hand-edit branches, worktrees, or commits.
