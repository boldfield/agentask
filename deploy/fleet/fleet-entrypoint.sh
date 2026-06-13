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

# Slot = pod hostname: stable for this pod, unique across replicas, so each agent gets a distinct id.
exec /harness/agent.sh "$@" "$(hostname)"
