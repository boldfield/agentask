#!/usr/bin/env bash
# Generic implementation worker — thin wrapper over the unified engine (agent.sh).
# Selects model from the task (not hardcoded).
# Usage: worker.sh [slot]   (slot defaults to "worker-1"; use distinct slots for parallel workers)
set -uo pipefail
src="${BASH_SOURCE[0]}"; while [ -h "$src" ]; do d="$(cd -P "$(dirname "$src")" && pwd)"; src="$(readlink "$src")"; [[ $src != /* ]] && src="$d/$src"; done
exec "$(cd -P "$(dirname "$src")" && pwd)/agent.sh" --kind implement "${1:-worker-1}"
