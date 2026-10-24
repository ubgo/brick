#!/bin/bash
# End-to-end check of sub-path hosting in a real headless Chrome: one server
# (go run ./ui -serve ADDR -engine site) mounts the showcase at /ui/ and the
# engine demo at /engine/. Runs the showcase suite with E2E_BASE=/ui, the
# engine suite with E2E_BASE=/engine, and the cross-site checks
# (runtime/test/e2e_both_chrome.mjs) against it, then stops the server.
# Needs Go, node and Google Chrome (or CHROME=/path/to/chrome).
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
source "$root/scripts/serve_showcase.sh"
serve_showcase /ui/ -engine site
cd "$root/runtime/test"
status=0
echo "== showcase at /ui"
E2E_BASE=/ui node e2e_ui_chrome.mjs "$url" || status=1
echo "== engine demo at /engine"
E2E_BASE=/engine node e2e_chrome.mjs "$url" || status=1
echo "== across the two"
node e2e_both_chrome.mjs "$url" || status=1
exit $status
