#!/usr/bin/env bash
# Opus reviewer — drains review-kind tasks. Thin wrapper over the unified engine (agent.sh).
# Usage: reviewer-opus.sh [slot]   (slot defaults to "opus-1")
set -uo pipefail
src="${BASH_SOURCE[0]}"; while [ -h "$src" ]; do d="$(cd -P "$(dirname "$src")" && pwd)"; src="$(readlink "$src")"; [[ $src != /* ]] && src="$d/$src"; done
exec "$(cd -P "$(dirname "$src")" && pwd)/agent.sh" --model opus --kind review "${1:-opus-1}"
