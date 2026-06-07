#!/usr/bin/env bash
# Haiku implementation worker — thin wrapper over the unified engine (agent.sh).
# Usage: worker-haiku.sh [slot]   (slot defaults to "haiku-1"; use distinct slots for parallel workers)
set -uo pipefail
src="${BASH_SOURCE[0]}"; while [ -h "$src" ]; do d="$(cd -P "$(dirname "$src")" && pwd)"; src="$(readlink "$src")"; [[ $src != /* ]] && src="$d/$src"; done
exec "$(cd -P "$(dirname "$src")" && pwd)/agent.sh" --model haiku --kind implement "${1:-haiku-1}"
