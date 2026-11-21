#!/bin/sh
# Starts a throwaway server (separate config dir), runs the e2e checks, stops it.
set -u
here=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
XDG_CONFIG_HOME="$tmp" "$here/dist/overspan" serve --port "${PORT:-7940}" >"$tmp/server.log" 2>&1 &
pid=$!
sleep 1
timeout 150 node "$here/tests/e2e.mjs" "${PORT:-7940}"
rc=$?
kill "$pid"; wait "$pid" 2>/dev/null
timeout 60 node "$here/tests/shutdown-leak.mjs" || rc=1
echo "--- server log"; sed -n '/connected\|Created\|Removed\|Resized\|Reusing\|error\|warn\|DATA RACE/p' "$tmp/server.log"
rm -rf "$tmp"
exit $rc
