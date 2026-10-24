#!/bin/bash
# End-to-end check in a real headless Chrome: builds the demo host
# (examples/main.go), serves examples/site on a free port, runs runtime/test/e2e_chrome.mjs against it,
# and stops the server. Needs Google Chrome (or CHROME=/path/to/chrome).
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
bin="$(mktemp -d)/demo"
(cd "$root/examples" && go build -o "$bin" .)
port=$((18100 + RANDOM % 800))
(cd "$root/examples" && exec "$bin" -serve "127.0.0.1:$port" >/dev/null 2>&1) &
server=$!
trap 'kill $server 2>/dev/null || true' EXIT
for _ in $(seq 1 50); do curl -sf -o /dev/null "http://127.0.0.1:$port/" && break; sleep 0.1; done
cd "$root/runtime/test" && node e2e_chrome.mjs "http://127.0.0.1:$port"
