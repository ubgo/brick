#!/bin/bash
# End-to-end check of the kit showcase in a real headless Chrome: serves
# examples/ui on a free port, runs runtime/test/e2e_ui_chrome.mjs against
# it, and stops the server on exit. Needs Go, node and Google Chrome (or
# CHROME=/path/to/chrome).
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
source "$root/scripts/serve_showcase.sh"
serve_showcase /
cd "$root/runtime/test" && node e2e_ui_chrome.mjs "$url"
