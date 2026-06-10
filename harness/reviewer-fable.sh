#!/usr/bin/env bash
# Fable reviewer — drains review-kind tasks assigned to Fable. Thin wrapper over the unified engine.
# (Tasks only spawn Fable reviews when their `review_models` includes "fable".)
# Usage: reviewer-fable.sh [slot]   (slot defaults to "fable-rev-1")
set -uo pipefail
src="${BASH_SOURCE[0]}"; while [ -h "$src" ]; do d="$(cd -P "$(dirname "$src")" && pwd)"; src="$(readlink "$src")"; [[ $src != /* ]] && src="$d/$src"; done
exec "$(cd -P "$(dirname "$src")" && pwd)/agent.sh" --model fable --kind review "${1:-fable-rev-1}"
