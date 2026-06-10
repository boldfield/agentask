#!/usr/bin/env bash
# Sonnet implementation worker — thin wrapper over the unified engine (agent.sh).
# The middle tier of the escalation ladder (haiku->sonnet->opus): claims implement-kind, model=sonnet
# tasks, e.g. work re-dispatched off haiku after a reject loop. Distinct from the sonnet REVIEWER
# (kind=review), so the two never collide.
# Usage: worker-sonnet.sh [slot]   (slot defaults to "sonnet-worker-1"; use distinct slots for parallel workers)
set -uo pipefail
src="${BASH_SOURCE[0]}"; while [ -h "$src" ]; do d="$(cd -P "$(dirname "$src")" && pwd)"; src="$(readlink "$src")"; [[ $src != /* ]] && src="$d/$src"; done
exec "$(cd -P "$(dirname "$src")" && pwd)/agent.sh" --model sonnet --kind implement "${1:-sonnet-worker-1}"
