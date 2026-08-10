# Feature: sbx agent bootstrap (codex + claude + board config inside the sandbox)

Status: feature spec, 2026-08-10
Kind: feature_spec (for project Agentask)

## What this is

`harness/sbx.sh` already boots the whole Agentask stack — server + worker/reviewer fleet — inside an
`sbx` sandbox container with all state under `/tmp/agentask`. What it does **not** do is provision
the *agents* that container runs. Today a fresh sandbox has `claude` (sbx installs and authenticates
it) but **no `codex`, no `~/.codex`, no Agentask skills wired into `~/.claude/`, and no board
config**. So the sandbox cannot run a two-reviewer board: the second reviewer is a `codex exec`
model that isn't installed, isn't authenticated, isn't in the sandbox server's model allowlist, and
isn't routed.

This feature closes that gap with a bootstrap script (plus a host-side auth-seeding step) so that
after `sbx.sh` comes up, an interactive `claude` session **inside** the container knows how to drive
the local board and creates tasks that are reviewed by **both `opus` and `gpt-5.5`**.

Scope is the **sandbox only**. The cluster fleet already installs both CLIs
(`deploy/fleet/Dockerfile.fleet`), already routes `gpt-5.5` through `codex exec`
(`reviewer-deployment.yaml`), and already allows `gpt-5.5` in its model allowlist. None of that
changes here.

## Measured starting state (verified in the running `claude-agentask-sbx` container)

| Thing | State |
|---|---|
| container user / home | `agent`, `HOME=/home/agent` |
| `claude` | present at `/home/agent/.local/bin/claude`, authenticated via `~/.claude/.credentials.json` (sbx-provided) |
| `codex` | **absent**; `~/.codex` does not exist |
| `node`, `npm`, `git`, `gh`, `jq`, `go` | present |
| npm global prefix | `/usr/local/share/npm-global`, root-owned (not writable by `agent`) |
| `sudo -n` | works — passwordless |
| `~/.claude/skills` | **does not exist** |
| repo mounts | bind-mounted at the **same absolute paths as the host**, e.g. `/Users/boldfield/projects/agentask` |

## The four gaps

1. **`codex` is not installed.** The npm global prefix is root-owned, but passwordless `sudo` is
   available, so a global install is the simple correct path — no per-user prefix gymnastics.

2. **`codex` has no credentials, and `codex login` is an interactive browser flow** that cannot
   complete headless in a container. Auth must be *seeded* by copying the host's
   `~/.codex/auth.json` in. This is the same move `make codex-auth` already makes for the cluster
   (which seeds a k8s secret from that file and validates `.tokens.refresh_token` first). Because
   the copy originates on the host, this half **cannot run inside the container** — it needs
   `sbx cp`, and is therefore a separate host-side entry point.

3. **The sandbox server rejects `gpt-5.5`.** `harness/sbx.sh` starts the server with
   `AGENTASK_MODELS="haiku,sonnet,opus,fable"` — no `gpt-5.5`. Creating a task with
   `review_models: ["opus","gpt-5.5"]` therefore fails at create time with `UNKNOWN_MODEL`. And even
   if it were allowed, `sbx.sh` never sets `AGENT_CODEX_MODELS`, so `harness/agent.sh` would dispatch
   the review as `claude -p --model gpt-5.5` — which is not a claude model — instead of routing it
   through `codex exec`.

   `sbx.sh`'s escalation config has also drifted from production: it still carries the original
   `AGENTASK_ESCALATION_THRESHOLDS="haiku=6,sonnet=4,opus=2,fable=1"` and predates
   `AGENTASK_ESCALATION_LADDER` existing at all. Production is now
   `haiku=3,sonnet=2,opus=2,fable=1` with a separate ladder that deliberately **excludes**
   `gpt-5.5` so a review-only model never becomes an escalation tier. The sandbox should mirror
   that, otherwise adding `gpt-5.5` to the allowlist silently makes it an escalation target.

4. **Nothing teaches the in-container Claude the two-reviewer default.** The server's fallback when
   `review_models` is empty is hardcoded `["opus"]`, and
   `skills/agentask-breakdown/scripts/agentask.sh` documents exactly that in its usage text
   (*"review_models is optional, defaults to `["opus"]`"*). An interactive Claude reading that will
   keep creating single-reviewer tasks.

## Model choice: `gpt-5.5`, not `gpt-5.6`

`gpt-5.6` was requested but is **not available on this account**. Verified directly against
codex-cli 0.147.0:

- `codex exec -m gpt-5.6` → `warning: Model metadata for 'gpt-5.6' not found`, then
  `400 invalid_request_error: The 'gpt-5.6' model is not supported when using Codex with a ChatGPT
  account.`
- `codex exec -m gpt-5.5` → succeeds.

`gpt-5.5` is also what the production allowlist and reviewer deployment already use. Every task in
this feature uses `gpt-5.5`. If the account later gains `gpt-5.6`, it is a one-constant swap in each
of the places this feature touches.

## Scope

### Deliverable 1 — `harness/sbx-agent-setup.sh` (runs **inside** the container)

Idempotent provisioning of the agent tooling and board config. Re-running must be a clean no-op, not
an error or a duplicate.

- **Install `codex`** globally via `sudo npm install -g @openai/codex` when absent. Install `claude`
  the same way only if it is missing — in the standard sbx image it is already present and
  authenticated, and the script must not disturb the existing install or its credentials.
- **Verify, don't assume.** After install, confirm both binaries resolve on `PATH` and fail loudly
  with an actionable message if either does not.
- **Wire the Agentask skills in.** The repo's `skills/` directory ships four skills —
  `agentask-board`, `agentask-breakdown`, `agentask-ops`, `review`. Make them visible to the
  in-container Claude by linking them into `~/.claude/skills/`. Symlink rather than copy, so editing
  a skill in the mounted repo takes effect immediately with no re-provision — this mirrors the
  harness's existing "code lives in the repo, state lives in `$AGENTASK_HOME`" split. Resolve the
  repo path from the script's own location, not a hardcoded absolute path.
- **Write board config for the in-container Claude** — a `CLAUDE.md` telling it: the local board is
  at `http://localhost:<port>` with the fixed sandbox token, this is a throwaway local board (not the
  cluster), and **every task it creates must carry `review_models: ["opus","gpt-5.5"]`** unless the
  human explicitly says otherwise. State the reason (two independent reviewers: one Claude, one
  Codex) so the rule survives paraphrase.
- **Write `~/.claude/settings.json`** with a permission allowlist covering the local board's normal
  traffic — `curl` against `localhost:<port>`, the `agentask` CLI, and git — so routine board work
  does not prompt. Merge into any existing settings rather than clobbering them.
- **Take the port** as an argument defaulting to `8080`, matching `sbx.sh`.

### Deliverable 2 — host-side codex auth seeding

An entry point that runs **on the host** and copies `~/.codex/auth.json` into the container at
`/home/agent/.codex/auth.json`, mode `0600`, owned by `agent`.

- **Preflight before copying:** the file must exist and must contain `.tokens.refresh_token`, with
  the same two distinct, actionable failure messages `make codex-auth` already emits (missing file →
  run `codex login`; no refresh token → re-run `codex login`).
- Accept the sandbox name as an argument.
- **Verify after seeding** by running a trivial `codex exec -m gpt-5.5` inside the container and
  reporting success or failure. A silent copy that leaves codex unusable is the failure mode this
  step exists to prevent.
- Deliverable 1 is useless without this, so make the dependency explicit: if Deliverable 1 finds
  `codex` installed but unauthenticated, it should say so and name this step.

### Deliverable 3 — `harness/sbx.sh` config alignment

- Add `gpt-5.5` to the server's `AGENTASK_MODELS` so `review_models: ["opus","gpt-5.5"]` validates.
- Set `AGENT_CODEX_MODELS="gpt-5.5"` in the generated `/tmp/agentask/env` **and** in the exported
  environment the fleet children inherit — matching how the file already handles the other fleet
  variables, so `agent.sh` routes `gpt-5.5` review dispatches through `codex exec`.
- Realign escalation with production: adopt the separate `AGENTASK_ESCALATION_LADDER` that excludes
  `gpt-5.5`, and update the thresholds to the current production values.
- Carry the same rationale comments production's manifest carries, so the next reader knows
  `gpt-5.5` is review-only by design.

### Deliverable 4 — correct the documented review default

`skills/agentask-breakdown/scripts/agentask.sh` states the default is `["opus"]`. Update the usage
text and the example task object so the two-reviewer pair is what a reader copies. This is
documentation inside a script, not a behavior change — the server's fallback is unchanged and out of
scope.

## Non-scope

- **No changes to the cluster fleet**, its Dockerfile, its deployments, or the `manifests` repo. The
  cluster already installs both CLIs and routes `gpt-5.5`.
- **No change to the server's hardcoded `["opus"]` fallback.** Making that default configurable is a
  reasonable follow-up, but it affects every deployment including production, and this feature is
  sandbox-only.
- **Foreman** is untouched.
- **No `gpt-5.6`** until the account supports it.

## Acceptance criteria

1. On a fresh `claude-agentask-sbx` sandbox, running the host-side auth seeding followed by
   `harness/sbx-agent-setup.sh` inside the container leaves both `claude` and `codex` resolvable on
   `PATH`, with `codex exec -m gpt-5.5` completing successfully inside the container.
2. Re-running both steps is a clean no-op — no errors, no duplicated symlinks, no clobbered claude
   credentials or pre-existing settings.
3. All four skills (`agentask-board`, `agentask-breakdown`, `agentask-ops`, `review`) are visible to
   an in-container `claude` session, and editing a skill in the mounted repo is reflected without
   re-provisioning.
4. After `harness/sbx.sh` boots, creating a task on the sandbox board with
   `review_models: ["opus","gpt-5.5"]` succeeds — no `UNKNOWN_MODEL`.
5. When that task reaches review, the server spawns **two** review tasks (one per model), the
   `opus` one dispatches via `claude -p`, and the `gpt-5.5` one dispatches via `codex exec` — not
   `claude -p --model gpt-5.5`.
6. `gpt-5.5` is in the sandbox allowlist but **not** in the escalation ladder: a repeatedly-rejected
   implement task escalates `haiku → sonnet → opus → fable` and never to `gpt-5.5`.
7. `harness/harness_test.sh` still passes.
