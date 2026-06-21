#!/usr/bin/env bash
# agent.sh — the unified Agentask fleet engine. One loop, parameterized:
#   --model <tier>            the model this agent claims + runs (e.g. haiku, opus)
#   --kind  <implement|review|merge>  implement = worker, review = reviewer, merge = merger (non-LLM).
#                                      LLM prompts resolve as prompts/<delivery_mode>/<track>/<kind>.md.
#   [slot]                    stable slot name -> persistent agent id + dedicated worktree(s)
#
# PROJECT SCOPE (from $AGENTASK_PROJECT):
#   a project uuid  -> SINGLE-project mode: pinned to that board + $AGENTASK_REPO (back-compat).
#   "all" or empty  -> MULTI-project mode: poll GET /projects?claimable=&model=&kind= (v0.4.0+),
#                      shuffle, and drain every project that has matching work — cloning each
#                      project's repo on demand and standing up a per-(slot,repo) worktree (a
#                      worktree can't span repositories). Optional $AGENTASK_PROJECTS (comma-sep
#                      ids) restricts which projects multi-mode will touch.
#
# Run it straight from the repo's harness/ dir. Code + prompts live next to this script (the dir is
# resolved from this script's own path, and still works if invoked via a symlink); the prompt is read
# FRESH each dispatch. STATE — env, agent ids, repo clones, worktrees — lives under $AGENTASK_HOME
# (~/.agentask) and is NOT versioned. Ctrl-C is a GRACEFUL stop (in-flight task finishes; again = force-quit).
#
# NOTE: assumes each repo's default branch is `main` (matches the implement prompt). master-default repos
# need the prompt parameterized — not supported yet.
#
# NOTE: requires `agentask` CLI to be on PATH for board discovery and polling.
set -uo pipefail
set -m

# --- resolve our REAL directory, even when invoked via a symlink in ~/.agentask ---
_src="${BASH_SOURCE[0]}"
while [ -h "$_src" ]; do
  _d="$(cd -P "$(dirname "$_src")" && pwd)"; _src="$(readlink "$_src")"; [[ $_src != /* ]] && _src="$_d/$_src"
done
HARNESS_DIR="$(cd -P "$(dirname "$_src")" && pwd)"

AGENTASK_HOME="${AGENTASK_HOME:-$HOME/.agentask}"
# Source the env file if present (local fleet). In k8s the config arrives via the container env and
# there is no file — so don't hard-fail when it's absent.
# shellcheck source=/dev/null
[ -f "$AGENTASK_HOME/env" ] && source "$AGENTASK_HOME/env"

# --- parse args ---
MODEL="" KIND="" SLOT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --model) MODEL="${2:?}"; shift 2 ;;
    --kind)  KIND="${2:?}";  shift 2 ;;
    -h|--help) echo "usage: agent.sh --model <tier> --kind <implement|review|merge> [slot]"; exit 0 ;;
    *) SLOT="$1"; shift ;;
  esac
done
case "${KIND:?--kind required}" in implement|review|merge) ;; *) echo "kind must be implement|review|merge" >&2; exit 1 ;; esac

export AGENT_MODEL="$MODEL"

# ============================== MERGE-KIND (REPO-LESS) ==============================
# Merge tasks are handled via REST API (internal/forge) and need NO local repo, NO worktree.
# Run as a separate early loop before any clone/worktree setup, then exit.
if [ "$KIND" = "merge" ]; then
  ROLE="merger"
  DEFAULT_SLOT="merger-1"
  SLOT="${SLOT:-${AGENT_SLOT:-$DEFAULT_SLOT}}"

  ID_DIR="$AGENTASK_HOME/agents"; ID_FILE="$ID_DIR/$SLOT.id"
  mkdir -p "$ID_DIR"
  [ -s "$ID_FILE" ] || echo "$SLOT-$(hostname -s)-$(od -An -N3 -tx1 /dev/urandom | tr -d ' ')" > "$ID_FILE"
  export AGENT_ID="$(cat "$ID_FILE")"

  MULTI=0
  case "${AGENTASK_PROJECT:-}" in ""|all|ALL) MULTI=1 ;; esac

  STOP=0
  request_stop() {
    [ "$STOP" -eq 1 ] && return
    STOP=1
    echo "[$AGENT_ID] stop requested — finishing the current $ROLE task, then exiting. Ctrl-C again to force-quit."
    trap - INT TERM
  }
  trap request_stop INT TERM

  nap() { sleep "$1" & wait $! 2>/dev/null; }

  merge_one() {
    local task_id="$1"
    agentask merge "$task_id"
  }

  # ---- SINGLE-PROJECT MERGE MODE ----
  if [ "$MULTI" = 0 ]; then
    echo "[$AGENT_ID] merger (merge/SINGLE) @ project $AGENTASK_PROJECT; polling"
    while true; do
      [ "$STOP" -eq 1 ] && break
      task_id=$(agentask next --project "$AGENTASK_PROJECT" --kind merge --claim 2>/dev/null)
      if [ -n "$task_id" ]; then
        echo "[$AGENT_ID] $(date '+%H:%M:%S') merging…"
        merge_one "$task_id"
      else
        echo "[$AGENT_ID] $(date '+%H:%M:%S') nothing claimable (merge); sleeping 30s"; nap 30
      fi
    done
  else
    # ---- MULTI-PROJECT MERGE MODE ----
    ALLOW="${AGENTASK_PROJECTS:-}"
    in_allow() { [ -z "$ALLOW" ] && return 0; case ",$ALLOW," in *",$1,"*) return 0 ;; *) return 1 ;; esac; }

    echo "[$AGENT_ID] merger (merge/MULTI) @ $AGENTASK_URL${ALLOW:+ (allow: $ALLOW)}; discovering work across projects"
    while true; do
      [ "$STOP" -eq 1 ] && break
      # Discover projects holding claimable merge work
      rows=()
      while IFS= read -r _row; do rows+=("$_row"); done < <(agentask projects --claimable --kind merge --json \
          | jq -r '.[] | .id' 2>/dev/null | sort -R)
      if [ "${#rows[@]}" -eq 0 ]; then
        echo "[$AGENT_ID] $(date '+%H:%M:%S') no claimable merge work in any project; sleeping 30s"; nap 30; continue
      fi

      worked=0
      for pid in "${rows[@]}"; do
        [ "$STOP" -eq 1 ] && break
        in_allow "$pid" || continue
        task_id=$(agentask next --project "$pid" --kind merge --claim 2>/dev/null)
        if [ -n "$task_id" ]; then
          echo "[$AGENT_ID] $(date '+%H:%M:%S') merging on project [${pid:0:8}]…"
          agentask merge "$task_id"
          worked=1
          break
        fi
      done
      [ "$STOP" -eq 1 ] && break
      [ "$worked" -eq 0 ] && { echo "[$AGENT_ID] $(date '+%H:%M:%S') candidate projects raced away; sleeping 10s"; nap 10; }
    done
  fi
  exit 0
fi

# ============================== IMPLEMENT/REVIEW KIND (WORKTREE-DEPENDENT) ==============================
if [ "$KIND" = "review" ]; then
  ROLE="reviewer"
  if [ -n "$MODEL" ]; then
    DEFAULT_SLOT="${MODEL}-rev-1"
  else
    DEFAULT_SLOT="reviewer-1"
  fi
  SLOT="${SLOT:-${AGENT_SLOT:-$DEFAULT_SLOT}}"
else
  ROLE="worker"
  if [ -n "$MODEL" ]; then
    DEFAULT_SLOT="${MODEL}-1"
  else
    DEFAULT_SLOT="worker-1"
  fi
  SLOT="${SLOT:-${AGENT_SLOT:-$DEFAULT_SLOT}}"
fi

# Delivery mode check
DELIVERY_MODE="${AGENTASK_DELIVERY_MODE:-pull_request}"
case "$DELIVERY_MODE" in pull_request|local_commit) ;; *) echo "delivery mode must be pull_request or local_commit" >&2; exit 1 ;; esac

# The prompt is keyed on all three axes — delivery_mode, track, kind — as PATH dimensions:
#   prompts/<delivery_mode>/<track>/<kind>.md
# No special-casing: a new mode/track/kind is just a file. A combo with no prompt (e.g.
# local_commit + design) resolves to a missing path, and the caller already skips on "prompt not
# found" — which is correct (no prompt = no such work).
get_prompt_file() {
  local track="${1:-build}"
  local kind="$2"
  echo "$HARNESS_DIR/prompts/$DELIVERY_MODE/$track/$kind.md"
}

ID_DIR="$AGENTASK_HOME/agents"; ID_FILE="$ID_DIR/$SLOT.id"
REPOS_DIR="$AGENTASK_HOME/repos"     # per-repo clones (multi-mode); intentional unbounded cache — prune by hand if it grows
mkdir -p "$ID_DIR"
[ -s "$ID_FILE" ] || echo "$SLOT-$(hostname -s)-$(od -An -N3 -tx1 /dev/urandom | tr -d ' ')" > "$ID_FILE"
export AGENT_ID="$(cat "$ID_FILE")"

# Mode: single (a uuid) vs multi (all/empty).
MULTI=0
case "${AGENTASK_PROJECT:-}" in ""|all|ALL) MULTI=1 ;; esac

# --- graceful stop ---
STOP=0
CLAUDE_PID=""   # pid (== pgid, via `set -m`) of the in-flight `claude -p`, if any
request_stop() {
  [ "$STOP" -eq 1 ] && return
  STOP=1
  echo "[$AGENT_ID] stop requested — stopping the in-flight $ROLE task and exiting. Ctrl-C again to force-quit."
  # The in-flight claude runs in its OWN process group (`set -m`, to shield it from the terminal's
  # Ctrl-C), so a group-kill aimed at the fleet's group never reaches it. TERM its group here so it
  # winds down WITH us — otherwise a force-kill of this agent would orphan the claude.
  [ -n "$CLAUDE_PID" ] && kill -TERM "-$CLAUDE_PID" 2>/dev/null || true
  trap - INT TERM
}
trap request_stop INT TERM

# --- helpers ---
norm_repo() { echo "$1" | sed -E 's#^(https://|git@)github\.com[:/]##; s#\.git$##'; }   # -> owner/repo
repo_slug() { norm_repo "$1" | tr '/' '-'; }                                            # -> owner-repo

# --- per-owner GitHub auth ---
# ~/.agentask/forge-tokens optionally pairs a repo OWNER with a PAT (lines: "owner=token"; # comments
# ok). The worker uses the matching token to clone/push/gh for that owner's repos; with no entry it
# falls back to the operator's default gh auth. Git creds stay in the worker, never in Agentask.
# (chmod 600 it — it holds tokens.)
FORGE_TOKENS="${FORGE_TOKENS:-$AGENTASK_HOME/forge-tokens}"
token_for_owner() {
  [ -f "$FORGE_TOKENS" ] || return 0
  # case-insensitive owner match — GitHub owners are case-insensitive (fAIctory == faictory), and the
  # owner derived from the repo URL may differ in case from the forge-tokens entry.
  local val
  val=$(sed -E 's/[[:space:]]*#.*$//' "$FORGE_TOKENS" 2>/dev/null \
        | grep -iE "^[[:space:]]*$1[[:space:]]*=" | head -1 | sed -E 's/^[^=]*=[[:space:]]*//; s/[[:space:]]*$//')
  # tolerate a token wrapped in surrounding quotes ("ghp_…" or 'ghp_…') — a common copy-paste slip
  # that yields HTTP 401 Bad credentials otherwise.
  val="${val#[\"\']}"; val="${val%[\"\']}"
  printf '%s' "$val"
}
# Set GH_TOKEN for a repo owner from the map, or fall back to the operator's default gh auth.
apply_owner_token() {
  local tok; tok="$(token_for_owner "$1")"
  if [ -n "$tok" ]; then export GH_TOKEN="$tok"; else unset GH_TOKEN 2>/dev/null || true; fi
}

# Ensure a local clone of a repo (clone on first sight, fetch otherwise); echo the clone dir.
ensure_clone() {
  local repo="$1" owner_repo slug clone
  owner_repo="$(norm_repo "$repo")"; slug="$(repo_slug "$repo")"; clone="$REPOS_DIR/$slug"
  if [ ! -d "$clone/.git" ]; then
    mkdir -p "$REPOS_DIR"
    gh repo clone "$owner_repo" "$clone" -- --quiet 2>/dev/null \
      || git clone --quiet "https://github.com/$owner_repo.git" "$clone" 2>/dev/null \
      || { echo "[$AGENT_ID] clone failed: $owner_repo" >&2; return 1; }
  fi
  git -C "$clone" fetch origin --quiet 2>/dev/null || true
  # If a per-owner token is set, tokenize the remote so the worker's git push/fetch authenticate as
  # that owner (gh commands read GH_TOKEN from the env). No token -> leave the plain remote (default auth).
  if [ -n "${GH_TOKEN:-}" ]; then
    git -C "$clone" remote set-url origin "https://x-access-token:${GH_TOKEN}@github.com/$owner_repo.git" 2>/dev/null || true
  fi
  echo "$clone"
}

# Ensure this slot's detached worktree for a given clone; echo the worktree dir.
ensure_worktree() {
  local clone="$1" wt
  wt="$AGENTASK_HOME/wt-$SLOT-$(basename "$clone")"
  git -C "$clone" worktree prune 2>/dev/null || true
  [ -e "$wt" ] && { git -C "$clone" worktree remove --force "$wt" 2>/dev/null || rm -rf "$wt"; }
  git -C "$clone" worktree add --detach "$wt" origin/main --quiet 2>/dev/null \
    || { echo "[$AGENT_ID] worktree add failed for $(basename "$clone")" >&2; return 1; }
  echo "$wt"
}

# Run one claude task, shielded from Ctrl-C, waiting until it truly finishes.
# Captures claude's exit code and backs off on failure: a non-zero exit means claude
# could not run (out of credits, auth, missing binary) — NOT that the task is bad. With
# no backoff the loop would re-dispatch into a failing claude instantly and spin. The
# backoff escalates with consecutive failures (capped) and self-heals when claude works
# again (credits returned), so an overnight credit lapse pauses rather than hammers.
CLAUDE_FAILS=0
dispatch() {
  local prompt; prompt="$(cat "$PROMPT_FILE")"

  # Check if AGENT_MODEL is in AGENT_CODEX_MODELS (comma-separated list)
  local use_codex=0
  if [ -n "${AGENT_CODEX_MODELS:-}" ]; then
    case ",$AGENT_CODEX_MODELS," in
      *",$AGENT_MODEL,"*) use_codex=1 ;;
    esac
  fi

  if [ "$use_codex" -eq 1 ]; then
    # shellcheck disable=SC2086
    codex exec -m "$AGENT_MODEL" --sandbox danger-full-access -c model_reasoning_effort=high ${AGENT_CODEX_FLAGS:-} "$prompt" &
  else
    # >>> remove --dangerously-skip-permissions if you want interactive permission prompts <<<
    # AGENT_CLAUDE_FLAGS appends extra flags to the nested claude (default empty, so the normal fleet
    # is unchanged). sbx.sh sets it to --allow-dangerously-skip-permissions, which a NESTED `claude -p`
    # requires inside an sbx sandbox; it is unquoted on purpose so multiple flags word-split.
    # shellcheck disable=SC2086
    claude -p --dangerously-skip-permissions ${AGENT_CLAUDE_FLAGS:-} "$prompt" --model "$AGENT_MODEL" &
  fi

  CLAUDE_PID=$!   # tracked so request_stop()/cleanup() can tear down this claude's process group
  local pid=$CLAUDE_PID rc=0
  while kill -0 "$pid" 2>/dev/null; do wait "$pid"; rc=$?; done
  CLAUDE_PID=""
  # If we're shutting down, the non-zero rc is our own TERM of claude — don't treat it as a credit
  # failure and don't back off; just unwind so the loop can exit promptly.
  [ "$STOP" -eq 1 ] && return "$rc"
  if [ "$rc" -ne 0 ]; then
    CLAUDE_FAILS=$((CLAUDE_FAILS + 1))
    local backoff=$((CLAUDE_FAILS * 30)); [ "$backoff" -gt 300 ] && backoff=300
    echo "[$AGENT_ID] $(date '+%H:%M:%S') dispatch exited rc=$rc (likely out of credits/auth); consecutive failures=$CLAUDE_FAILS, backing off ${backoff}s" >&2
    nap "$backoff"
  else
    CLAUDE_FAILS=0
  fi
  return "$rc"
}

nap() { sleep "$1" & wait $! 2>/dev/null; }

# Check if a project has claimable tasks for THIS agent's kind (any model).
# Returns 0 if claimable work exists, 1 if not.
has_claimable_work() {
  agentask next --project "$1" --kind "$KIND" >/dev/null 2>&1
}

# Cleanup: drop ALL of this slot's worktrees (single wt-$SLOT and multi wt-$SLOT-*), prune clones.
cleanup() {
  # Backstop: if a claude dispatch is still tracked when we exit (force-quit, error), KILL its
  # process group so it can't outlive us as an orphan. (Cannot run if WE are SIGKILLed — that's why
  # request_stop TERMs it on the graceful path.)
  [ -n "$CLAUDE_PID" ] && kill -KILL "-$CLAUDE_PID" 2>/dev/null || true
  echo "[$AGENT_ID] cleaning up worktrees for slot $SLOT"
  for wt in "$AGENTASK_HOME/wt-$SLOT" "$AGENTASK_HOME"/wt-"$SLOT"-*; do
    [ -e "$wt" ] && rm -rf "$wt"
  done
  for clone in "$REPOS_DIR"/* "${AGENTASK_MAIN_REPO:-${AGENTASK_REPO:-/nonexistent}}"; do
    [ -d "$clone/.git" ] && git -C "$clone" worktree prune 2>/dev/null || true
  done
}
trap cleanup EXIT

# ============================== SINGLE-PROJECT MODE ==============================
if [ "$MULTI" = 0 ]; then
  WT=""  # Initialize WT; set only in pull_request mode
  # local_commit mode: use AGENTASK_REPO directly (CLI-managed worktree), skip clone
  if [ "$DELIVERY_MODE" = "local_commit" ]; then
    : "${AGENTASK_WORKTREE_HOME:?AGENTASK_WORKTREE_HOME required for local_commit mode}"
    MAIN_REPO="$AGENTASK_REPO"
    export AGENTASK_WORKTREE_HOME
    cd "$MAIN_REPO" || { echo "[$AGENT_ID] failed to cd to AGENTASK_REPO ($MAIN_REPO)" >&2; exit 1; }
    AGENT_MODEL_STR="${MODEL:+$MODEL/}$KIND"
    echo "[$AGENT_ID] $ROLE ($AGENT_MODEL_STR) SINGLE (local_commit) @ project $AGENTASK_PROJECT @ $MAIN_REPO; polling"
  else
    # pull_request mode: standard clone + worktree setup
    MAIN_REPO="${AGENTASK_MAIN_REPO:-$AGENTASK_REPO}"
    WT="$AGENTASK_HOME/wt-$SLOT"
    # Guard: refuse if MAIN_REPO doesn't match the pinned project's repo.
    _proj_repo=$(agentask project "$AGENTASK_PROJECT" --json | jq -r '.repo // ""')
    _origin=$(git -C "$MAIN_REPO" remote get-url origin 2>/dev/null || echo "")
    if [ -n "$_proj_repo" ] && [ "$(norm_repo "$_proj_repo")" != "$(norm_repo "$_origin")" ]; then
      echo "[$AGENT_ID] REFUSING: project $AGENTASK_PROJECT repo is '$(norm_repo "$_proj_repo")' but AGENTASK_REPO ($MAIN_REPO) points at '$(norm_repo "$_origin")'." >&2
      exit 1
    fi
    [ -n "$_proj_repo" ] && apply_owner_token "$(norm_repo "$_proj_repo" | cut -d/ -f1)"   # gh auth for the pinned project's owner
    git -C "$MAIN_REPO" fetch origin --quiet || true
    git -C "$MAIN_REPO" worktree prune
    [ -e "$WT" ] && { git -C "$MAIN_REPO" worktree remove --force "$WT" 2>/dev/null || rm -rf "$WT"; }
    git -C "$MAIN_REPO" worktree add --detach "$WT" origin/main
    export AGENTASK_REPO="$WT"; cd "$WT" || { echo "worktree cd failed"; exit 1; }
    AGENT_MODEL_STR="${MODEL:+$MODEL/}$KIND"
    echo "[$AGENT_ID] $ROLE ($AGENT_MODEL_STR) SINGLE @ project $AGENTASK_PROJECT @ $WT; polling"
  fi
  while true; do
    [ "$STOP" -eq 1 ] && break
    if has_claimable_work "$AGENTASK_PROJECT"; then
      # Find the next claimable task (any model) and get its model
      task_id=$(agentask next --project "$AGENTASK_PROJECT" --kind "$KIND" 2>/dev/null)
      if [ -z "$task_id" ]; then
        echo "[$AGENT_ID] $(date '+%H:%M:%S') nothing claimable ($KIND); sleeping 30s"; nap 30; continue
      fi
      task_json=$(agentask show "$task_id" --json 2>/dev/null)
      task_model=$(echo "$task_json" | jq -r '.model // ""')
      if [ -z "$task_model" ]; then
        echo "[$AGENT_ID] $(date '+%H:%M:%S') failed to read task model for $task_id; sleeping 30s"; nap 30; continue
      fi
      # Read track from task, default to 'build' if absent
      task_track=$(echo "$task_json" | jq -r '.track // "build"')
      PROMPT_FILE="$(get_prompt_file "$task_track" "$KIND")"
      if [ ! -f "$PROMPT_FILE" ]; then
        echo "[$AGENT_ID] $(date '+%H:%M:%S') prompt not found: $PROMPT_FILE; skipping task $task_id"; continue
      fi
      echo "[$AGENT_ID] $(date '+%H:%M:%S') claimable $KIND; dispatching ($task_model/$task_track)…"
      export AGENT_MODEL="$task_model"
      dispatch
      [ -n "$MODEL" ] && export AGENT_MODEL="$MODEL"   # restore original model for slot identity (if initially provided)
      [ -n "$WT" ] && git -C "$WT" fetch origin --quiet 2>/dev/null || true
      [ -n "$WT" ] && git -C "$WT" checkout --detach --force origin/main --quiet 2>/dev/null || true
      [ "$STOP" -eq 1 ] && break
    else
      echo "[$AGENT_ID] $(date '+%H:%M:%S') nothing claimable ($KIND); sleeping 30s"; nap 30
    fi
  done
  exit 0
fi

# ============================== MULTI-PROJECT MODE ==============================
# local_commit mode requires single-project (works with a pre-set worktree); multi-project clones multiple repos.
if [ "$DELIVERY_MODE" = "local_commit" ]; then
  echo "[$AGENT_ID] local_commit mode requires SINGLE-project mode; AGENTASK_PROJECT must be set" >&2
  exit 1
fi

ALLOW="${AGENTASK_PROJECTS:-}"   # optional comma-separated id allowlist
in_allow() { [ -z "$ALLOW" ] && return 0; case ",$ALLOW," in *",$1,"*) return 0 ;; *) return 1 ;; esac; }

AGENT_MODEL_STR="${MODEL:+$MODEL/}$KIND"
echo "[$AGENT_ID] $ROLE ($AGENT_MODEL_STR) MULTI @ $AGENTASK_URL${ALLOW:+ (allow: $ALLOW)}; discovering work across projects"
while true; do
  [ "$STOP" -eq 1 ] && break
  # Discover projects holding my kind claimable work (any model) — one call (v0.4.0 filter).
  # (while-read, not mapfile: macOS ships bash 3.2.) sort -R shuffles so projects drain fairly.
  rows=()
  while IFS= read -r _row; do rows+=("$_row"); done < <(agentask projects --claimable --kind "$KIND" --json \
      | jq -r '.[] | select(.repo != null and .repo != "") | "\(.id)\t\(.repo)"' 2>/dev/null \
      | sort -R)
  if [ "${#rows[@]}" -eq 0 ]; then
    echo "[$AGENT_ID] $(date '+%H:%M:%S') no claimable $KIND work in any project; sleeping 30s"; nap 30; continue
  fi

  worked=0
  for row in "${rows[@]}"; do
    [ "$STOP" -eq 1 ] && break
    pid="${row%%$'\t'*}"; prepo="${row#*$'\t'}"
    [ -z "$pid" ] && continue
    in_allow "$pid" || continue
    # Re-check claimable (the listing can race another worker); skip if it emptied out.
    has_claimable_work "$pid" || continue
    apply_owner_token "$(norm_repo "$prepo" | cut -d/ -f1)"   # auth as the repo's owner (default auth if unmapped)
    clone="$(ensure_clone "$prepo")" || continue
    wt="$(ensure_worktree "$clone")" || continue
    export AGENTASK_PROJECT="$pid" AGENTASK_REPO="$wt"
    cd "$wt" || continue
    # Find the next claimable task (any model) and get its model
    task_id=$(agentask next --project "$pid" --kind "$KIND" 2>/dev/null)
    if [ -z "$task_id" ]; then
      continue   # task raced away, try next project
    fi
    task_json=$(agentask show "$task_id" --json 2>/dev/null)
    task_model=$(echo "$task_json" | jq -r '.model // ""')
    if [ -z "$task_model" ]; then
      continue   # couldn't read task model, try next project
    fi
    # Read track from task, default to 'build' if absent
    task_track=$(echo "$task_json" | jq -r '.track // "build"')
    PROMPT_FILE="$(get_prompt_file "$task_track" "$KIND")"
    if [ ! -f "$PROMPT_FILE" ]; then
      continue   # prompt file not found, try next project
    fi
    echo "[$AGENT_ID] $(date '+%H:%M:%S') dispatching ($task_model/$task_track/$KIND) on $(norm_repo "$prepo") [${pid:0:8}]…"
    export AGENT_MODEL="$task_model"
    dispatch
    [ -n "$MODEL" ] && export AGENT_MODEL="$MODEL"   # restore original model for slot identity (if initially provided)
    git -C "$wt" checkout --detach --force origin/main --quiet 2>/dev/null || true
    worked=1
    break   # one task per discovery pass, then re-poll fresh (keeps the shuffle honest)
  done
  [ "$STOP" -eq 1 ] && break
  # rows existed but every candidate raced away / failed setup — brief sleep, then re-poll.
  [ "$worked" -eq 0 ] && { echo "[$AGENT_ID] $(date '+%H:%M:%S') candidate projects raced away; sleeping 10s"; nap 10; }
done
