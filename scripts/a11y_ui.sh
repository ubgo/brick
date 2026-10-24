#!/bin/bash
# Accessibility audit of the kit showcase with axe-core in a real headless
# Chrome: serves examples/ui on a free port, runs scripts/a11y_ui.mjs against
# it, and stops the server on exit. Pass a path to also write every finding
# as JSON. Needs Go, node (with fetch and WebSocket, 22+), Google Chrome (or
# CHROME=/path/to/chrome) and network access for axe-core (or
# AXE_FILE=/path/to/axe.min.js).
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
source "$root/scripts/serve_showcase.sh"
serve_showcase /
node "$root/scripts/a11y_ui.mjs" "$url" "${1:-}"
