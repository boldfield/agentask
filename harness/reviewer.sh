#!/usr/bin/env bash
# Generic reviewer — drains review-kind tasks. Thin wrapper over the unified engine (agent.sh).
# Selects model from the task (not hardcoded).
# Usage: reviewer.sh [slot]   (slot defaults to "reviewer-1")
set -uo pipefail
src="${BASH_SOURCE[0]}"; while [ -h "$src" ]; do d="$(cd -P "$(dirname "$src")" && pwd)"; src="$(readlink "$src")"; [[ $src != /* ]] && src="$d/$src"; done
exec "$(cd -P "$(dirname "$src")" && pwd)/agent.sh" --kind review "${1:-reviewer-1}"
