---
name: review
description: Use to drive the human review gate from inside an interactive `claude` session — show the queue of tasks awaiting a decision and the diff for one of them, so a human can decide whether to approve, reject, or merge. READ-ONLY: it presents the pending queue (`agentask pending`) and a commit/PR diff (`agentask diff`) and makes NO decision and changes NO board state. Triggers on requests like "what's waiting for review?", "show me the review queue", "let me review the board", or "show me the diff for <task>".
---

# Agentask review (human gate)

A conversational wrapper for the **human review gate**. The sandbox's only interface is an
interactive `claude` session, so this skill is how a human inspects what is waiting and reads a
diff before deciding. It **presents information only** — it surfaces the queue and the diff, then
stops and lets the human make the call. It never approves, rejects, merges, or transitions
anything.

## The one rule that overrides everything

**READ-ONLY. Present, then stop.** This skill runs exactly two commands — `agentask pending` and
`agentask diff` — both of which only read. It **never** runs `agentask approve`, `reject`,
`merge`, or `transition`, and never edits the repo. The verdict is the human's; this skill exists
to inform that verdict, not to render it. After showing the diff, hand the decision back to the
human.

## When to use it

- The human wants to see what work is awaiting a decision ("what's in the review queue?").
- The human wants to read the diff for a specific pending task before deciding.
- As the human-facing companion to the model-pinned fleet: Haiku implements, the Opus reviewer
  worker approves/rejects review-kind tasks, and the human drains the `approved` lane by merging.
  This skill is how the human looks before they leap — it does not perform the merge itself.

## Phase 0 — Configuration

- `AGENTASK_URL` and `AGENTASK_TOKEN` must be set (the running Agentask instance + bearer token);
  the CLI reads them from the environment. If either is missing, ask the human.
- You need the **project id**. Use `$AGENTASK_PROJECT` if it is set; otherwise ask the human, or
  run `agentask projects` to list them and let the human pick.

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

## Phase 3 — Hand the decision back

Stop here. Summarize what is pending and what the diff shows if that helps, then **let the human
decide**. Acting on the decision — approve, reject, or merge — is a separate, human-initiated
step and is explicitly **not** part of this skill.

## The gates you do not cross

- You never approve, reject, merge, or transition a task — those mutate board state and belong to
  the human (or the reviewer worker), not to this read-only skill.
- You never edit the repository or run anything beyond `agentask pending` and `agentask diff`.
