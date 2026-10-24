#!/bin/bash
# Headless Chrome check of the v22 runtime features (Modelable, server
# transition control, brick:keydown.norepeat, brick:click.details): serves
# the kit showcase on a free port, runs runtime/test/v22_chrome.mjs against
# its /runtime-extras/ demos, and stops the server on exit. Needs Go, node
# and Google Chrome (or CHROME=/path/to/chrome).
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
source "$root/scripts/serve_showcase.sh"
serve_showcase /
cd "$root/runtime/test" && node v22_chrome.mjs "$url"
