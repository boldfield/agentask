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
| `reviewer-sonnet.sh [slot]` | Sonnet reviewer | `model=sonnet, kind=review` |

Run a few in separate terminals, each with a **distinct slot**:

    AGENTASK_PROJECT=<id> AGENTASK_REPO=~/projects/<repo> ./worker-haiku.sh haiku-1

Each agent takes a persistent id (per slot), stands up its own detached git worktree, polls for
claimable work of its `(model, kind)`, dispatches one `claude -p` task, then repeats. Ctrl-C is a
graceful stop (finishes the in-flight task; Ctrl-C again to force-quit).

## Code vs. state

The engine keeps **code** and **state** in separate trees, so they never mix:

- **Code + prompts** (`agent.sh`, the wrappers, `worker-prompt.md`, `reviewer-prompt.md`) — versioned
  *here*, in the repo. The engine finds them via its own location, and reads the prompt **fresh each
  dispatch**, so editing it applies to the next task with no restart.
- **State + config** — lives under `$AGENTASK_HOME` (default `~/.agentask`), un-versioned: `env`
  (URL / token / project), `agents/<slot>.id` (persistent ids), `wt-*` (worktrees), `repos/`
  (on-demand repo clones in multi-project mode), and optionally `forge-tokens` (per-owner PATs).
  Copy `env.example` → `~/.agentask/env` and fill it in.

## Per-owner GitHub auth (`forge-tokens`)

A multi-project fleet may span repos owned by **different GitHub identities** (e.g. `boldfield/*`
and the dynamically-created `fAIctory/*`). A single `gh` login can't push to both, and you can't
pre-assign a "fAIctory fleet" because those repos appear at runtime — so the worker resolves the
right token **from the repo owner** (which Agentask already exposes via each project's `repo`).

Optional `~/.agentask/forge-tokens` pairs an owner with a PAT (`owner=token` per line; `#` comments
ok). The worker derives the owner from the project's repo URL, exports that owner's token as
`GH_TOKEN` for the clone + the dispatched worker's `git push`/`gh`, and **falls back to your default
`gh` auth** when an owner has no entry. Agentask never holds git creds — they live with the worker.

    cp harness/forge-tokens.example ~/.agentask/forge-tokens
    # add e.g.  fAIctory=ghp_…   then:
    chmod 600 ~/.agentask/forge-tokens

**Run the scripts straight from the repo — no symlinks.** Because the two trees are independent, you
invoke the code from `harness/` and it uses `~/.agentask` purely for state:

    cd ~/projects/agentask/harness
    AGENTASK_PROJECT=all ./worker-haiku.sh haiku-1

(Symlinking the wrappers *into* `~/.agentask` would drop code into the state tree next to `repos/`
and `wt-*` — don't. If you want to invoke from anywhere, add `harness/` to `PATH` or alias the
wrappers; `$AGENTASK_HOME` still points the engine at your state.)

## Project scope: single vs. multi

Set by `AGENTASK_PROJECT`:

- **A project uuid → single-project** (the default; back-compat). The slot is pinned to that board
  and `AGENTASK_REPO`, with one worktree — exactly as before, including the repo-guard.
- **`all` (or empty) → multi-project.** The slot polls `GET /projects?claimable=true&model=&kind=`
  (the v0.4.0 work-discovery filter), shuffles the projects that have its work, and drains them all —
  cloning each project's repo on demand into `~/.agentask/repos/<owner-repo>/` and standing up a
  per-`(slot, repo)` worktree `wt-<slot>-<owner-repo>` (a worktree can't span repositories).
  `AGENTASK_PROJECTS=<id,id,…>` optionally restricts which projects multi-mode will touch.

      AGENTASK_PROJECT=all ./worker-haiku.sh haiku-1     # one slot, all boards

  One `claude -p` task per discovery pass, then re-poll with a fresh shuffle — so N parallel slots
  spread across all work-bearing projects instead of serializing on one board. The repo-guard
  inverts from "refuse on mismatch" to "set up the project's repo." Assumes each repo's default
  branch is `main` (master-default repos need the prompt parameterized — not yet supported).
