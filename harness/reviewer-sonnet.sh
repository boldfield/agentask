#!/usr/bin/env bash
# Sonnet reviewer — drains review-kind tasks assigned to Sonnet. Thin wrapper over the unified engine.
# (Tasks only spawn Sonnet reviews when their `review_models` includes "sonnet".)
# Usage: reviewer-sonnet.sh [slot]   (slot defaults to "sonnet-1")
set -uo pipefail
src="${BASH_SOURCE[0]}"; while [ -h "$src" ]; do d="$(cd -P "$(dirname "$src")" && pwd)"; src="$(readlink "$src")"; [[ $src != /* ]] && src="$d/$src"; done
exec "$(cd -P "$(dirname "$src")" && pwd)/agent.sh" --model sonnet --kind review "${1:-sonnet-1}"
