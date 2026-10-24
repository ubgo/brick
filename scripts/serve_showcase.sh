# Sourced by the showcase's real-browser scripts (e2e_ui.sh, e2e_both.sh,
# a11y_ui.sh): builds examples/ui once and serves it in the background on a
# free port, with the server stopped when the calling script exits.
#
#   source "$root/scripts/serve_showcase.sh"
#   serve_showcase /ready/path [extra ./ui flags…]    sets $url
#
# A built binary, not `go run`, and `exec` in the subshell, so $server IS the
# server process and the EXIT trap stops it (killing a `go run` or a plain
# subshell would leave the real server running). The trap ends with
# `|| true`: under `set -e` the killed server's non-zero status would
# otherwise become the script's exit code and fail a passing run.

# PORT_BASE and PORT_SPAN pick the port at random, so two runs at once rarely
# collide (a collision fails loudly: the server does not start).
PORT_BASE=18300
PORT_SPAN=1500
# READY_TRIES × READY_STEP is how long the server may take to answer: a
# cold build plus the site's first index of every page.
READY_TRIES=150
READY_STEP=0.1

serve_showcase() {
  local ready="$1"; shift
  local bin log port
  bin="$(mktemp -d)/ui"
  (cd "$root/examples" && go build -o "$bin" ./ui)
  port=$((PORT_BASE + RANDOM % PORT_SPAN))
  log="$(mktemp)"
  (cd "$root/examples" && exec "$bin" -site ui -serve "127.0.0.1:$port" "$@" >"$log" 2>&1) &
  server=$!
  trap "kill $server 2>/dev/null; wait $server 2>/dev/null || true; rm -f '$log'" EXIT
  url="http://127.0.0.1:$port"
  for _ in $(seq 1 "$READY_TRIES"); do curl -sf -o /dev/null "$url$ready" && return 0; sleep "$READY_STEP"; done
  echo "the showcase did not start:"; cat "$log"
  exit 1
}
