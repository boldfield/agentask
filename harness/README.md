# Agentask fleet harness

The headless pull-worker fleet that drains an Agentask board: model-pinned agents claim their own
work, run it via `claude -p`, and submit — Haiku implements, Opus reviews, the human merges.

## One engine

`agent.sh` is the whole loop, parameterized by `--model` + `--kind`. The wrappers are one-liners:

| Wrapper | Role | Claims |
|---|---|---|
| `worker-haiku.sh [slot]`  | Haiku implementer | `model=haiku, kind=implement` |
| `worker-opus.sh [slot]`   | Opus implementer (e.g. prompt-authoring) | `model=opus, kind=implement` |
| `reviewer-opus.sh [slot]` | Opus reviewer | `model=opus, kind=review` |

Run a few in separate terminals, each with a **distinct slot**:

    AGENTASK_PROJECT=<id> AGENTASK_REPO=~/projects/<repo> ./worker-haiku.sh haiku-1

Each agent takes a persistent id (per slot), stands up its own detached git worktree, polls for
claimable work of its `(model, kind)`, dispatches one `claude -p` task, then repeats. Ctrl-C is a
graceful stop (finishes the in-flight task; Ctrl-C again to force-quit).

## Code vs. state

- **Code + prompts** (`agent.sh`, the wrappers, `worker-prompt.md`, `reviewer-prompt.md`) are
  versioned here. The engine reads the prompt **fresh each dispatch**, so editing it applies to the
  next task with no restart.
- **State + config** lives under `$AGENTASK_HOME` (default `~/.agentask`), un-versioned: `env`
  (URL / token / project), `agents/<slot>.id` (persistent ids), `wt-<slot>` (worktrees). Copy
  `env.example` → `~/.agentask/env` and fill it in.

## Running from `~/.agentask` (symlinks)

To keep `cd ~/.agentask && ./worker-haiku.sh …` working, symlink the scripts + prompts to this
versioned copy (one source of truth, old paths still resolve):

    cd ~/.agentask
    for f in agent.sh worker-haiku.sh worker-opus.sh reviewer-opus.sh worker-prompt.md reviewer-prompt.md; do
      ln -sf "$HOME/projects/agentask/harness/$f" "$f"
    done

This checkout must then stay on a branch that contains `harness/` (i.e. `main` after this merges) —
otherwise the symlinks dangle.

## Roadmap — multi-project

Single-project today (`AGENTASK_PROJECT` pinned). The multi-project redesign — poll a
`projects-with-work` endpoint, shuffle the result, and stand up a worktree **per repo** (a worktree
can't span repositories) — slots into `agent.sh`'s discovery + worktree steps without touching the
wrappers. The repo-guard inverts from "refuse on mismatch" to "set up the project's repo."
