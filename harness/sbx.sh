#!/usr/bin/env bash
# sbx.sh — boot the WHOLE Agentask stack (server + worker/reviewer fleet) inside an `sbx` sandbox,
# with ALL harness state under /tmp/agentask. It starts the agentask HTTP server (local SQLite DB,
# fixed local token), then N implement workers + N reviewers via fleet.sh, draining the project you
# point it at. Nested `claude -p` needs --allow-dangerously-skip-permissions alongside
# --dangerously-skip-permissions inside a sandbox; it's passed via AGENT_CLAUDE_FLAGS (agent.sh).
#
# Usage:
#   bash harness/sbx.sh [start] --project <uuid> --repo <path> [common opts]  # local_commit (default)
#   bash harness/sbx.sh [start] --project all --delivery-mode pull_request     # drain all boards (needs GitHub)
#   bash harness/sbx.sh [start] --seed-demo [common opts]                      # throwaway self-contained demo
#   bash harness/sbx.sh stop   [--port P]    # stop the running stack cleanly (no Ctrl-C needed)
#   bash harness/sbx.sh status [--port P]    # report server + fleet state
#
#   --project <uuid|all>   what the fleet drains (required unless --seed-demo). 'all' = pull_request multi.
#   --repo <path>          local git repo the CLI commits into (required for local_commit / pull_request single).
#   --seed-demo            create+use a throwaway local repo + project + board (no GitHub) — for a smoke test.
#   --worktree-home <path> CLI per-task worktree root (local_commit; default /tmp/agentask/worktrees).
#   --reviewer-model TIER  PIN reviewers to a model tier (default: empty = dynamic, i.e. review with the
#                          task's own model — like the workers). Workers are always dynamic.
#   common opts: --workers N (2)  --reviewers N (2)  --port P (8080)
#                --delivery-mode pull_request|local_commit (local_commit)
#
# Ctrl-C / TERM gracefully stops the fleet, then the server. Re-runnable (reuses DB; --seed-demo reuses repo/project).
set -uo pipefail

# --- resolve our REAL directory, even when invoked via a symlink ---
_src="${BASH_SOURCE[0]}"
while [ -h "$_src" ]; do
  _d="$(cd -P "$(dirname "$_src")" && pwd)"; _src="$(readlink "$_src")"; [[ $_src != /* ]] && _src="$_d/$_src"
done
HARNESS_DIR="$(cd -P "$(dirname "$_src")" && pwd)"
REPO_ROOT="$(cd -P "$HARNESS_DIR/.." && pwd)"

# --- defaults / args ---
# By default the fleet drains the project YOU point it at (--project, + --repo for local_commit).
# Pass --seed-demo for a fully self-contained throwaway repo+project+board (no GitHub) — handy for a
# smoke test, but NOT created on a normal boot.
# Workers AND reviewers are model-DYNAMIC by default: each claims any task of its kind and runs claude
# with whatever model the task specifies. --reviewer-model only PINS reviewer slots to a tier if you
# want that (empty = dynamic).
WORKERS=2 REVIEWERS=2 REVIEWER_MODEL="" PORT=8080 DELIVERY_MODE="local_commit"
SEED_DEMO=0 PROJECT_ARG="" REPO_ARG="" WORKTREE_HOME_ARG=""

# Subcommand (optional leading word): start (default) | stop | status. `stop`/`status` only need
# --port; running sbx.sh from inside Claude (backgrounded) can't take Ctrl-C, so `sbx.sh stop` is the
# clean way to bring the stack down.
SUBCMD="start"
case "${1:-}" in
  start|stop|status) SUBCMD="$1"; shift ;;
  --stop)            SUBCMD="stop";   shift ;;
  --status)          SUBCMD="status"; shift ;;
esac

while [ $# -gt 0 ]; do
  case "$1" in
    --workers)        WORKERS="${2:?}";          shift 2 ;;
    --reviewers)      REVIEWERS="${2:?}";         shift 2 ;;
    --reviewer-model) REVIEWER_MODEL="${2:?}";    shift 2 ;;
    --port)           PORT="${2:?}";             shift 2 ;;
    --delivery-mode)  DELIVERY_MODE="${2:?}";     shift 2 ;;
    --project)        PROJECT_ARG="${2:?}";       shift 2 ;;   # <uuid> or "all" (pull_request multi)
    --repo)           REPO_ARG="${2:?}";          shift 2 ;;   # local repo for local_commit / single
    --worktree-home)  WORKTREE_HOME_ARG="${2:?}"; shift 2 ;;   # override CLI worktree root (local_commit)
    --seed-demo)      SEED_DEMO=1;                shift ;;     # create+use a throwaway local demo
    -h|--help)
      sed -n '2,24p' "$_src"; exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 1 ;;
  esac
done
case "$DELIVERY_MODE" in pull_request|local_commit) ;; *) echo "delivery mode must be pull_request or local_commit" >&2; exit 1 ;; esac

# --- fixed local config (never leaves the container) ---
export AGENTASK_HOME=/tmp/agentask
LOCAL_TOKEN="sbx-local-token"
AGENTASK_URL="http://localhost:$PORT"
DB_PATH="$AGENTASK_HOME/agentask.db"
LOG_DIR="$AGENTASK_HOME/logs"
SEED_REPO="$AGENTASK_HOME/repo"                 # the demo local repo (only with --seed-demo)
SEED_ORIGIN="$AGENTASK_HOME/repo-origin.git"    # bare "origin" so origin/main resolves (demo only)
WORKTREE_HOME="${WORKTREE_HOME_ARG:-$AGENTASK_HOME/worktrees}"  # CLI per-task worktrees (local_commit)
SERVER_LOG="$LOG_DIR/server.log"
PROJECT_NAME="sbx-local"

mkdir -p "$AGENTASK_HOME" "$LOG_DIR"
[ "$DELIVERY_MODE" = "local_commit" ] && mkdir -p "$WORKTREE_HOME"

# Put the freshly-built (or existing) binary first on PATH — agent.sh/CLI call `agentask` by name.
export PATH="$REPO_ROOT/bin:$PATH"

say() { echo "[sbx] $*"; }
die() { echo "[sbx] ERROR: $*" >&2; exit 1; }

# Auth header array for curl calls to our local server.
AUTH=(-H "Authorization: Bearer $LOCAL_TOKEN" -H "Content-Type: application/json")

# One pidfile per port records the running `sbx.sh start` PID, so `sbx.sh stop` can TERM it (which
# fires its graceful trap: stop the fleet groups, then the server).
PIDFILE="$AGENTASK_HOME/sbx-$PORT.pid"

# ============================== stop / status subcommands ==============================
do_status() {
  local code
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "$AGENTASK_URL/healthz" 2>/dev/null || echo 000)"
  if [ "$code" = "200" ]; then say "server: UP on :$PORT (/healthz 200)"; else say "server: DOWN on :$PORT"; fi
  if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE" 2>/dev/null)" 2>/dev/null; then
    say "sbx.sh: running (pid $(cat "$PIDFILE"))"
  else
    say "sbx.sh: not tracked as running on :$PORT"
  fi
  local agents
  agents="$(pgrep -f "$HARNESS_DIR/agent.sh" 2>/dev/null | wc -l | tr -d ' ')"
  say "fleet agents: ${agents:-0}"
}

do_stop() {
  if [ -f "$PIDFILE" ]; then
    local pid; pid="$(cat "$PIDFILE" 2>/dev/null)"
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      say "stopping sbx.sh (pid $pid) on :$PORT — graceful…"
      kill -TERM "$pid" 2>/dev/null || true
      for _ in $(seq 1 25); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
      if kill -0 "$pid" 2>/dev/null; then say "did not exit; SIGKILL"; kill -KILL "$pid" 2>/dev/null || true; fi
      say "stopped."
    else
      say "pidfile present but process not running; cleaning up."
    fi
    rm -f "$PIDFILE"
  else
    say "no tracked sbx.sh on :$PORT (pidfile $PIDFILE absent)."
  fi
  # Fallback: if the tracked sbx.sh was gone (crashed) but fleet agents linger, reap them directly.
  # Path-scoped to THIS harness so we never touch another checkout's fleet — and we NEVER `pkill
  # claude` (that would kill the sandbox's own outer Claude). TERM first so each agent's trap tears
  # down its in-flight claude (see agent.sh request_stop), brief grace, then KILL any straggler.
  if pgrep -f "$HARNESS_DIR/agent.sh" >/dev/null 2>&1; then
    say "reaping lingering fleet agents (path-scoped)…"
    pkill -TERM -f "$HARNESS_DIR/agent.sh" 2>/dev/null || true
    pkill -TERM -f "$HARNESS_DIR/fleet.sh" 2>/dev/null || true
    for _ in $(seq 1 12); do pgrep -f "$HARNESS_DIR/agent.sh" >/dev/null 2>&1 || break; sleep 1; done
    pkill -KILL -f "$HARNESS_DIR/agent.sh" 2>/dev/null || true
    pkill -KILL -f "$HARNESS_DIR/fleet.sh" 2>/dev/null || true
  fi
  # Safety net: if a server we started is still answering AND nothing tracks it, leave it alone unless
  # it's clearly ours and orphaned. We only report; we never kill an untracked server blindly.
  if curl -s -o /dev/null --max-time 3 "$AGENTASK_URL/healthz" 2>/dev/null; then
    say "note: a server still answers on :$PORT (another sbx.sh instance may be reusing it)."
  fi
}

case "$SUBCMD" in
  stop)   do_stop;   exit 0 ;;
  status) do_status; exit 0 ;;
esac

# --- validate targeting flags (fail fast, before we build or start anything) ---
if [ "$SEED_DEMO" -eq 1 ]; then
  [ -n "$PROJECT_ARG" ] && die "--seed-demo creates its own project; don't also pass --project"
  [ -n "$REPO_ARG" ]    && die "--seed-demo creates its own repo; don't also pass --repo"
else
  [ -n "$PROJECT_ARG" ] || die "no target: pass --project <uuid> (and --repo <path> for local_commit), or --seed-demo for a throwaway demo"
  if [ "$DELIVERY_MODE" = "local_commit" ]; then
    case "$PROJECT_ARG" in
      all|ALL|"") die "local_commit needs a SINGLE project: pass --project <uuid> (not 'all'), or --seed-demo" ;;
    esac
    [ -n "$REPO_ARG" ] || die "local_commit needs the local repo: pass --repo <path> (the git repo the CLI commits into), or --seed-demo"
    [ -d "$REPO_ARG/.git" ] || die "--repo '$REPO_ARG' is not a git repository"
    REPO_ARG="$(cd -P "$REPO_ARG" && pwd)"   # normalize to absolute
  else
    # pull_request: single (uuid) needs a local checkout; multi ("all") clones on demand.
    case "$PROJECT_ARG" in
      all|ALL) : ;;
      *) [ -n "$REPO_ARG" ] || die "pull_request single-project needs --repo <path> (local checkout), or use --project all"
         [ -d "$REPO_ARG/.git" ] || die "--repo '$REPO_ARG' is not a git repository"
         REPO_ARG="$(cd -P "$REPO_ARG" && pwd)" ;;
    esac
  fi
fi

# ============================== 1. build the binary if missing ==============================
if [ ! -x "$REPO_ROOT/bin/agentask" ]; then
  say "building agentask binary (make build)…"
  ( cd "$REPO_ROOT" && make build ) >>"$LOG_DIR/build.log" 2>&1 || die "make build failed (see $LOG_DIR/build.log)"
fi
command -v agentask >/dev/null 2>&1 || die "agentask not on PATH after build"
say "agentask: $(command -v agentask)"

# ============================== 2. handle a stale / bound port ==============================
# If our server is already up on this port and healthy, reuse it; if something else holds the port, error.
SERVER_PID=""
if curl -fsS "$AGENTASK_URL/healthz" >/dev/null 2>&1; then
  say "a healthy agentask already answers on :$PORT — reusing it (will NOT stop it on exit)"
  REUSE_SERVER=1
else
  REUSE_SERVER=0
  if command -v ss >/dev/null 2>&1 && ss -ltn 2>/dev/null | grep -q ":$PORT "; then
    die "port $PORT is bound by another process and /healthz is not green — pick another --port"
  fi
fi

# ============================== 3. start the server ==============================
if [ "$REUSE_SERVER" -eq 0 ]; then
  say "starting agentask server on :$PORT (db: $DB_PATH)…"
  AGENTASK_DB="$DB_PATH" \
  AGENTASK_ADDR=":$PORT" \
  AGENTASK_TOKEN="$LOCAL_TOKEN" \
  AGENTASK_MODELS="haiku,sonnet,opus,fable" \
  AGENTASK_ESCALATION_THRESHOLDS="haiku=6,sonnet=4,opus=2,fable=1" \
    agentask server >>"$SERVER_LOG" 2>&1 &
  SERVER_PID=$!

  # Poll /healthz until 200 (or fail loudly).
  say "waiting for /healthz…"
  ok=0
  for _ in $(seq 1 50); do
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
      die "server process exited during startup (see $SERVER_LOG)"
    fi
    if curl -fsS "$AGENTASK_URL/healthz" >/dev/null 2>&1; then ok=1; break; fi
    sleep 0.3
  done
  [ "$ok" -eq 1 ] || die "server did not become healthy within ~15s (see $SERVER_LOG)"
  say "server healthy (pid $SERVER_PID)"
fi

# ============================== 4 & 5. resolve the fleet's target project + repo ==============================
# Two paths: --seed-demo stands up a throwaway local repo + project (self-contained, no GitHub);
# otherwise the fleet drains YOUR --project (+ --repo for local_commit), which already exists.
if [ "$SEED_DEMO" -eq 1 ]; then
  # 4. Seed a throwaway git repo (no-op Makefile so `make check`/`make test` pass) with a bare origin
  #    so `origin/main` resolves for the CLI's local_commit worktrees. Idempotent.
  if [ ! -d "$SEED_REPO/.git" ]; then
    say "seeding demo git repo at $SEED_REPO…"
    rm -rf "$SEED_REPO" "$SEED_ORIGIN"
    git init -q -b main "$SEED_REPO"
    git -C "$SEED_REPO" config user.email "sbx@local" >/dev/null 2>&1
    git -C "$SEED_REPO" config user.name "sbx" >/dev/null 2>&1
    cat > "$SEED_REPO/Makefile" <<'MK'
.PHONY: check test
check:
	@echo "check: ok"
test:
	@echo "test: ok"
MK
    printf '# sbx demo repo\n\nA throwaway local repo for the self-contained Agentask fleet.\n' > "$SEED_REPO/README.md"
    printf 'hello\n' > "$SEED_REPO/GREETINGS.md"
    git -C "$SEED_REPO" add -A
    git -C "$SEED_REPO" commit -q -m "seed: initial demo repo"
    git init -q --bare "$SEED_ORIGIN"
    git -C "$SEED_REPO" remote add origin "$SEED_ORIGIN" 2>/dev/null || \
      git -C "$SEED_REPO" remote set-url origin "$SEED_ORIGIN"
    git -C "$SEED_REPO" push -q -u origin main
  fi
  git -C "$SEED_REPO" fetch -q origin 2>/dev/null || true

  # 5. Ensure the demo project (idempotent by name).
  PROJECT_ID="$(curl -fsS "${AUTH[@]}" "$AGENTASK_URL/projects" 2>/dev/null \
    | jq -r --arg n "$PROJECT_NAME" '.[] | select(.name == $n) | .id' 2>/dev/null | head -1)"
  if [ -z "$PROJECT_ID" ] || [ "$PROJECT_ID" = "null" ]; then
    say "creating demo project '$PROJECT_NAME'…"
    PROJECT_ID="$(curl -fsS "${AUTH[@]}" -X POST "$AGENTASK_URL/projects" \
      -d "$(jq -n --arg name "$PROJECT_NAME" --arg repo "$SEED_REPO" '{name:$name, repo:$repo}')" \
      | jq -r '.id')"
  fi
  [ -n "$PROJECT_ID" ] && [ "$PROJECT_ID" != "null" ] || die "failed to resolve demo project id"
  FLEET_REPO="$SEED_REPO"
  say "demo project: $PROJECT_ID  (repo: $SEED_REPO)"
else
  # Drain the caller's project. Soft-check it exists (the board may legitimately be empty for now).
  PROJECT_ID="$PROJECT_ARG"
  FLEET_REPO="$REPO_ARG"
  case "$PROJECT_ID" in
    all|ALL) say "fleet target: ALL projects (pull_request multi-project)" ;;
    *)
      if curl -fsS "${AUTH[@]}" "$AGENTASK_URL/projects/$PROJECT_ID" >/dev/null 2>&1; then
        say "fleet target: project $PROJECT_ID${FLEET_REPO:+  (repo: $FLEET_REPO)}"
      else
        say "WARNING: project $PROJECT_ID not found on the server yet — the fleet will idle until it exists"
      fi ;;
  esac
fi

# ============================== 6. write the fleet env file ==============================
# agent.sh reads AGENTASK_HOME from the ENVIRONMENT (before sourcing this file), so we also export it
# in this script and let fleet.sh/agent.sh inherit it. Everything else the fleet needs lives here.
# AGENTASK_REPO / AGENTASK_WORKTREE_HOME are only meaningful for local_commit + pull_request single
# mode; harmless (ignored) for pull_request multi-project.
say "writing fleet env -> $AGENTASK_HOME/env"
cat > "$AGENTASK_HOME/env" <<EOF
# Generated by harness/sbx.sh — sandbox fleet config. Do not commit.
export AGENTASK_URL="$AGENTASK_URL"
export AGENTASK_TOKEN="$LOCAL_TOKEN"
export AGENTASK_PROJECT="$PROJECT_ID"
export AGENTASK_HOME="$AGENTASK_HOME"
export AGENTASK_REPO="$FLEET_REPO"
export AGENTASK_WORKTREE_HOME="$WORKTREE_HOME"
export AGENTASK_DELIVERY_MODE="$DELIVERY_MODE"
# Nested claude -p inside a sandbox needs this alongside --dangerously-skip-permissions:
export AGENT_CLAUDE_FLAGS="--allow-dangerously-skip-permissions"
EOF

# Export for fleet.sh/agent.sh children of THIS process too (env file is the source of truth, but
# AGENTASK_HOME in particular must be in the environment before agent.sh sources the env file).
export AGENTASK_URL AGENTASK_TOKEN AGENTASK_WORKTREE_HOME
export AGENTASK_REPO="$FLEET_REPO"
export AGENTASK_PROJECT="$PROJECT_ID"
export AGENTASK_DELIVERY_MODE="$DELIVERY_MODE"
export AGENT_CLAUDE_FLAGS="--allow-dangerously-skip-permissions"

# ============================== 7. graceful shutdown ==============================
# Each fleet is launched as its OWN process-group leader (job control, set -m, in §8), so its pid ==
# its pgid. We signal the WHOLE group (kill -SIG -pgid) — fleet.sh + every agent.sh + any nested
# claude — directly, rather than trusting fleet.sh to propagate. TERM first (graceful: agents drop
# their in-flight dispatch and exit), then a hard KILL fallback so NOTHING is ever orphaned.
FLEET_PIDS=()
STOPPED=0
group_alive() { kill -0 "-$1" 2>/dev/null; }
stop_all() {
  [ "$STOPPED" -eq 1 ] && return
  STOPPED=1
  echo
  say "shutting down…"
  # TERM each fleet's process group.
  for pgid in "${FLEET_PIDS[@]:-}"; do
    [ -n "$pgid" ] && kill -TERM "-$pgid" 2>/dev/null || true
  done
  # Wait up to ~12s for the groups to drain, then KILL whatever is left.
  for _ in $(seq 1 12); do
    local any=0
    for pgid in "${FLEET_PIDS[@]:-}"; do
      [ -n "$pgid" ] && group_alive "$pgid" && any=1
    done
    [ "$any" -eq 0 ] && break
    sleep 1
  done
  for pgid in "${FLEET_PIDS[@]:-}"; do
    [ -n "$pgid" ] && group_alive "$pgid" && kill -KILL "-$pgid" 2>/dev/null || true
  done
  # Reap the (now-dead) fleet job leaders so they aren't left as zombies.
  for pgid in "${FLEET_PIDS[@]:-}"; do
    [ -n "$pgid" ] && wait "$pgid" 2>/dev/null || true
  done
  # Then the server (only if WE started it).
  if [ "$REUSE_SERVER" -eq 0 ] && [ -n "$SERVER_PID" ]; then
    kill -TERM "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
    say "server stopped"
  fi
  # Drop our pidfile only if it still points at us (a newer instance may have taken it over).
  [ -f "$PIDFILE" ] && [ "$(cat "$PIDFILE" 2>/dev/null)" = "$$" ] && rm -f "$PIDFILE"
  say "done. state + logs under $AGENTASK_HOME (logs: $LOG_DIR)"
}
trap 'stop_all; exit 0' INT TERM
trap 'stop_all' EXIT

# ============================== 8. start the fleet ==============================
# `set -m` (job control) makes each backgrounded fleet its OWN process-group leader, so $! == its
# pgid and stop_all can kill the whole group (fleet + agents + nested claude) — see §7. Each fleet's
# combined output goes to a per-kind log file; every line is already prefixed with the agent's slot
# id by agent.sh (e.g. "[worker-1-…] …"), so one file per kind tells you which agent did what. Follow
# them live with: tail -f "$LOG_DIR"/workers.log "$LOG_DIR"/reviewers.log
# Workers are always model-dynamic (no --model). Reviewers are too BY DEFAULT — pass --model only if
# --reviewer-model pinned a tier; otherwise reviewers claim any review task and run claude with the
# task's model, exactly like the workers.
REVIEWER_MODEL_ARG=()
[ -n "$REVIEWER_MODEL" ] && REVIEWER_MODEL_ARG=(--model "$REVIEWER_MODEL")
RMODEL_LABEL="${REVIEWER_MODEL:-dynamic (task-specified)}"

say "starting fleet: $WORKERS worker(s, dynamic) + $REVIEWERS reviewer(s, model: $RMODEL_LABEL)  [delivery: $DELIVERY_MODE]"

# Record our PID so `sbx.sh stop --port $PORT` can bring this instance down cleanly (no Ctrl-C needed).
echo "$$" > "$PIDFILE"

set -m
"$HARNESS_DIR/fleet.sh" --kind implement --count "$WORKERS" --delivery-mode "$DELIVERY_MODE" \
  >> "$LOG_DIR/workers.log" 2>&1 &
FLEET_PIDS+=($!)

# ${arr[@]+"${arr[@]}"} expands to nothing when the array is empty — empty-safe under `set -u` on
# bash 3.2 (macOS), and (unlike "${arr[@]:-}") never passes a spurious empty "" arg to fleet.sh.
"$HARNESS_DIR/fleet.sh" --kind review --count "$REVIEWERS" ${REVIEWER_MODEL_ARG[@]+"${REVIEWER_MODEL_ARG[@]}"} --delivery-mode "$DELIVERY_MODE" \
  >> "$LOG_DIR/reviewers.log" 2>&1 &
FLEET_PIDS+=($!)
set +m

# NOTE: no merger. In local_commit mode the human owns the merge gate (agent_merge is a pull_request
# concern); a merge-kind agent has nothing to do here.

cat <<EOF
[sbx] ──────────────────────────────────────────────────────────────────────
[sbx] Agentask fleet is UP.
[sbx]   server     : $AGENTASK_URL   (token: $LOCAL_TOKEN)
[sbx]   project    : $PROJECT_ID${FLEET_REPO:+   (repo: $FLEET_REPO)}
[sbx]   mode       : $DELIVERY_MODE
[sbx]   fleet      : $WORKERS worker(s) [dynamic], $REVIEWERS reviewer(s) [model: $RMODEL_LABEL]
[sbx]   state/logs : $AGENTASK_HOME  /  $LOG_DIR
[sbx]   follow     : tail -f $LOG_DIR/workers.log $LOG_DIR/reviewers.log
EOF
if [ "$SEED_DEMO" -eq 1 ]; then
cat <<EOF
[sbx]
[sbx] Demo board — put a task on it (a worker then claims + dispatches claude).
[sbx] NOTE: tasks need a document_id, so create a document first:
[sbx]   A=(-H "Authorization: Bearer $LOCAL_TOKEN" -H "Content-Type: application/json")
[sbx]   DID=\$(curl -s "\${A[@]}" -X POST $AGENTASK_URL/projects/$PROJECT_ID/documents \\
[sbx]     -d '{"kind":"feature_spec","title":"demo","ref":"README.md"}' | jq -r '.id')
[sbx]   TID=\$(curl -s "\${A[@]}" -X POST $AGENTASK_URL/projects/$PROJECT_ID/tasks \\
[sbx]     -d "\$(jq -n --arg d "\$DID" '[{title:"demo",spec:"Append a line to GREETINGS.md",model:"haiku",document_id:\$d}]')" | jq -r '.[0].id')
[sbx]   curl -s "\${A[@]}" -X POST $AGENTASK_URL/tasks/\$TID/promote
EOF
fi
cat <<EOF
[sbx]
[sbx] Stop cleanly with:  bash harness/sbx.sh stop --port $PORT   (or Ctrl-C if running in a terminal)
[sbx] Check status with:  bash harness/sbx.sh status --port $PORT
[sbx] ──────────────────────────────────────────────────────────────────────
EOF

# Wait on the fleet; the EXIT/INT/TERM trap handles cleanup.
for pid in "${FLEET_PIDS[@]}"; do
  wait "$pid" 2>/dev/null || true
done
