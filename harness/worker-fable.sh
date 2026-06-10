#!/usr/bin/env bash
# Fable implementation worker — thin wrapper over the unified engine (agent.sh).
# The TOP tier of the escalation ladder (haiku->sonnet->opus->fable): claims implement-kind,
# model=fable tasks — the last-resort model for work that survived escalation through opus.
# Distinct from the fable REVIEWER (kind=review), so the two never collide.
# Usage: worker-fable.sh [slot]   (slot defaults to "fable-worker-1"; use distinct slots for parallel workers)
set -uo pipefail
src="${BASH_SOURCE[0]}"; while [ -h "$src" ]; do d="$(cd -P "$(dirname "$src")" && pwd)"; src="$(readlink "$src")"; [[ $src != /* ]] && src="$d/$src"; done
exec "$(cd -P "$(dirname "$src")" && pwd)/agent.sh" --model fable --kind implement "${1:-fable-worker-1}"
