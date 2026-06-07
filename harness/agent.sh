#!/usr/bin/env bash
# agent.sh — the unified Agentask fleet engine. One loop, parameterized:
#   --model <tier>            the model this agent claims + runs (e.g. haiku, opus)
#   --kind  <implement|review>  implement = worker (worker-prompt.md); review = reviewer (reviewer-prompt.md)
#   [slot]                    stable slot name -> persistent agent id + dedicated worktree
#
# Code + prompts live next to this script (resolved through symlinks), so editing a versioned
# prompt takes effect on the NEXT dispatch with no restart. STATE — env, agent ids, worktrees —
# lives under $AGENTASK_HOME (default ~/.agentask) and is NOT versioned.
#
# Ctrl-C / TERM is a GRACEFUL stop: the in-flight task finishes, THEN the loop exits. The `claude`
# run is backgrounded under job control (`set -m`) so it sits in its own process group and the
# terminal's Ctrl-C never reaches it. Ctrl-C a SECOND time to force-quit.
set -uo pipefail
set -m

# --- resolve our REAL directory, even when invoked via a symlink in ~/.agentask ---
_src="${BASH_SOURCE[0]}"
while [ -h "$_src" ]; do
  _d="$(cd -P "$(dirname "$_src")" && pwd)"; _src="$(readlink "$_src")"; [[ $_src != /* ]] && _src="$_d/$_src"
done
HARNESS_DIR="$(cd -P "$(dirname "$_src")" && pwd)"

AGENTASK_HOME="${AGENTASK_HOME:-$HOME/.agentask}"
# shellcheck source=/dev/null
source "$AGENTASK_HOME/env"

# --- parse args ---
MODEL="" KIND="" SLOT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --model) MODEL="${2:?}"; shift 2 ;;
    --kind)  KIND="${2:?}";  shift 2 ;;
    -h|--help) echo "usage: agent.sh --model <tier> --kind <implement|review> [slot]"; exit 0 ;;
    *) SLOT="$1"; shift ;;
  esac
done
: "${MODEL:?--model required}"
case "${KIND:?--kind required}" in implement|review) ;; *) echo "kind must be implement|review" >&2; exit 1 ;; esac

export AGENT_MODEL="$MODEL"
if [ "$KIND" = "review" ]; then
  ROLE="reviewer"; PROMPT_FILE="$HARNESS_DIR/reviewer-prompt.md"; SLOT="${SLOT:-${AGENT_SLOT:-${MODEL}-rev-1}}"
else
  ROLE="worker";   PROMPT_FILE="$HARNESS_DIR/worker-prompt.md";   SLOT="${SLOT:-${AGENT_SLOT:-${MODEL}-1}}"
fi
[ -f "$PROMPT_FILE" ] || { echo "prompt not found: $PROMPT_FILE" >&2; exit 1; }

MAIN_REPO="${AGENTASK_MAIN_REPO:-$AGENTASK_REPO}"   # the primary clone worktrees are added from
WT="$AGENTASK_HOME/wt-$SLOT"                          # this agent's dedicated worktree
ID_DIR="$AGENTASK_HOME/agents"; ID_FILE="$ID_DIR/$SLOT.id"

# Persistent agent id: generate once per slot, reuse on every restart.
mkdir -p "$ID_DIR"
[ -s "$ID_FILE" ] || echo "$SLOT-$(hostname -s)-$(od -An -N3 -tx1 /dev/urandom | tr -d ' ')" > "$ID_FILE"
export AGENT_ID="$(cat "$ID_FILE")"

# Graceful stop: first Ctrl-C drains the current task; second one force-quits.
STOP=0
request_stop() {
  [ "$STOP" -eq 1 ] && return
  STOP=1
  echo "[$AGENT_ID] stop requested — finishing the current $ROLE task, then exiting. Ctrl-C again to force-quit."
  trap - INT TERM
}
trap request_stop INT TERM

cleanup() {
  echo "[$AGENT_ID] cleaning up worktree $WT"
  cd "$MAIN_REPO" 2>/dev/null || true
  git -C "$MAIN_REPO" worktree remove --force "$WT" 2>/dev/null || rm -rf "$WT"
  git -C "$MAIN_REPO" worktree prune 2>/dev/null || true
}
trap cleanup EXIT

# Guard: refuse to run if MAIN_REPO doesn't match the project's repo (right project, wrong checkout).
# (In the multi-project redesign this inverts into "set up the project's repo" per claimed task.)
_norm() { echo "$1" | sed -E 's#^(https://|git@)github\.com[:/]##; s#\.git$##'; }
_proj_repo=$(curl -s --max-time 15 -H "Authorization: Bearer $AGENTASK_TOKEN" "$AGENTASK_URL/projects/$AGENTASK_PROJECT" | jq -r '.repo // ""')
_origin=$(git -C "$MAIN_REPO" remote get-url origin 2>/dev/null || echo "")
if [ -n "$_proj_repo" ] && [ "$(_norm "$_proj_repo")" != "$(_norm "$_origin")" ]; then
  echo "[$AGENT_ID] REFUSING: project $AGENTASK_PROJECT repo is '$(_norm "$_proj_repo")' but AGENTASK_REPO ($MAIN_REPO) points at '$(_norm "$_origin")'. Point AGENTASK_REPO at the matching checkout." >&2
  exit 1
fi

# Fresh detached worktree on startup (the agent branches/checks-out per task from origin/main).
git -C "$MAIN_REPO" fetch origin --quiet || true
git -C "$MAIN_REPO" worktree prune
[ -e "$WT" ] && { git -C "$MAIN_REPO" worktree remove --force "$WT" 2>/dev/null || rm -rf "$WT"; }
git -C "$MAIN_REPO" worktree add --detach "$WT" origin/main
export AGENTASK_REPO="$WT"
cd "$WT" || { echo "worktree cd failed"; exit 1; }

# Run one claude task, shielded from the terminal's Ctrl-C, waiting until it truly finishes.
# The prompt is read FRESH each dispatch so edits apply to the next task without a restart.
dispatch() {
  local prompt; prompt="$(cat "$PROMPT_FILE")"
  # >>> remove --dangerously-skip-permissions if you want interactive permission prompts <<<
  claude -p --dangerously-skip-permissions "$prompt" --model "$AGENT_MODEL" &
  local pid=$!
  while kill -0 "$pid" 2>/dev/null; do wait "$pid"; done
}
nap() { sleep "$1" & wait $! 2>/dev/null; }

echo "[$AGENT_ID] $ROLE ($MODEL/$KIND) @ $WT @ $(git rev-parse --short HEAD); polling $AGENTASK_URL"
while true; do
  [ "$STOP" -eq 1 ] && break
  # Count ONLY tasks of this agent's kind: a shared model tier (e.g. opus) returns both implement
  # and review tasks, and dispatching against the wrong kind would busy-loop.
  n=$(curl -s --max-time 15 -H "Authorization: Bearer $AGENTASK_TOKEN" \
        "$AGENTASK_URL/projects/$AGENTASK_PROJECT/tasks?model=$AGENT_MODEL&claimable=true" \
        | jq --arg k "$KIND" '[.[] | select(.kind == $k)] | length' 2>/dev/null || echo 0)
  if [ "${n:-0}" -gt 0 ]; then
    echo "[$AGENT_ID] $(date '+%H:%M:%S') $n claimable $KIND; dispatching claude ($MODEL)…"
    dispatch
    # Hygiene: return the worktree to a detached origin/main between tasks so the just-worked branch
    # isn't left checked out here (a held branch can't be checked out by another worktree).
    git -C "$WT" fetch origin --quiet 2>/dev/null || true
    git -C "$WT" checkout --detach --force origin/main --quiet 2>/dev/null || true
    [ "$STOP" -eq 1 ] && break
  else
    echo "[$AGENT_ID] $(date '+%H:%M:%S') nothing claimable ($KIND); sleeping 30s"
    nap 30
  fi
done
