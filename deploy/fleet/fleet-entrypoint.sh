#!/usr/bin/env bash
# Entrypoint for every Agentask fleet pod (merger now; worker/reviewer later — same wrapper).
# k8s gives us config via the container env (no ~/.agentask/env file) and tokens via mounts; this
# stages them where the harness + CLI expect, then hands off to agent.sh with a per-pod slot.
set -uo pipefail

: "${AGENTASK_HOME:=$HOME/.agentask}"
mkdir -p "$AGENTASK_HOME"

# Stage forge tokens where `agentask merge` (and the worker's gh auth) read them — the CLI's
# forge.OwnerToken always looks at $HOME/.agentask/forge-tokens, so copy from the read-only secret
# mount. (AGENTASK_HOME should be $HOME/.agentask for that to line up.)
if [ -f /etc/fleet/forge-tokens ]; then
  install -m 600 /etc/fleet/forge-tokens "$AGENTASK_HOME/forge-tokens"
fi

# Git identity + safe-dir for the worker/reviewer paths (no-op for the merger, which never commits).
# A pod has no global git config, so clones/commits would fail without these; safe.directory '*'
# avoids "dubious ownership" on worktrees in the (differently-owned) emptyDir.
git config --global user.name  "${GIT_AUTHOR_NAME:-agentask fleet}"          2>/dev/null || true
git config --global user.email "${GIT_AUTHOR_EMAIL:-fleet@agentask.local}"   2>/dev/null || true
git config --global init.defaultBranch main                                  2>/dev/null || true
git config --global --add safe.directory '*'                                 2>/dev/null || true

# Slot = pod hostname: stable for this pod, unique across replicas, so each agent gets a distinct id.
exec /harness/agent.sh "$@" "$(hostname)"
