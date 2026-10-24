#!/bin/bash
# Headless Chrome check of brick:method links (v23): builds
# examples/methodlinks, serves it twice on free ports (navigation off and
# on), runs runtime/test/v23_chrome.mjs against both, and stops the
# servers on exit. Needs Go, node and Google Chrome (or
# CHROME=/path/to/chrome).
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
bin="$(mktemp -d)/methodlinks"
(cd "$root/examples" && go build -o "$bin" ./methodlinks)
# Ports away from the shared dev stack (7001-7011) and the showcase (8002).
p1=$((18900 + RANDOM % 400))
p2=$((p1 + 400))
"$bin" -addr "127.0.0.1:$p1" -theme "$root/examples/methodlinks/theme" >/dev/null 2>&1 &
s1=$!
"$bin" -addr "127.0.0.1:$p2" -theme "$root/examples/methodlinks/theme" -nav >/dev/null 2>&1 &
s2=$!
trap 'kill $s1 $s2 2>/dev/null || true' EXIT
for p in $p1 $p2; do
  for _ in $(seq 1 100); do curl -sf -o /dev/null "http://127.0.0.1:$p/" && break; sleep 0.1; done
done
cd "$root/runtime/test" && node v23_chrome.mjs "http://127.0.0.1:$p1" "http://127.0.0.1:$p2"
