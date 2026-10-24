#!/bin/bash
# Headless Chrome check of the v21 runtime features: builds
# the brick CLI from this tree (it embeds runtime.js and the kit), serves
# runtime/test/v21site on a free port, runs runtime/test/v21_chrome.mjs
# against it, and stops the server on exit. Needs Go, node and Google
# Chrome (or CHROME=/path/to/chrome).
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
bin="$(mktemp -d)/brick"
(cd "$root/cmd/brick" && go build -o "$bin" .)
# A port away from the shared dev stack (7001-7011) and the showcase (8002).
port=$((18900 + RANDOM % 800))
"$bin" serve -addr "127.0.0.1:$port" "$root/runtime/test/v21site" >/dev/null 2>&1 &
server=$!
trap 'kill $server 2>/dev/null || true' EXIT
for _ in $(seq 1 100); do curl -sf -o /dev/null "http://127.0.0.1:$port/" && break; sleep 0.1; done
cd "$root/runtime/test" && node v21_chrome.mjs "http://127.0.0.1:$port"
