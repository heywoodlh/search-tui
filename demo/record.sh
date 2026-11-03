#!/usr/bin/env bash
# Record the website demo: drives the real `search` TUI inside tmux and
# captures it with asciinema (headless, fixed size).
#
# Usage: demo/record.sh OUTPUT.cast
# Needs on PATH: search, tmux, asciinema (3.x). A GitHub token in
# GITHUB_TOKEN/GH_TOKEN enables the code-search part of the demo.
set -euo pipefail

out="${1:?usage: record.sh OUTPUT.cast}"
here="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"
trap 'tmux -L demo kill-server 2>/dev/null || true; rm -rf "$work"' EXIT

# Isolated, deterministic environment for the recorded session.
mkdir -p "$work/config/tui-search"
cp "$here/config.toml" "$work/config/tui-search/config.toml"
export XDG_CONFIG_HOME="$work/config" HOME="$work" TERM=xterm-256color
export COLORTERM=truecolor LANG=C.UTF-8 LC_ALL=C.UTF-8
unset DISPLAY WAYLAND_DISPLAY BROWSER
export DEMO_DIR="$here"

asciinema rec --headless --overwrite --quiet \
  --window-size 100x28 \
  --idle-time-limit 2 \
  --title "search: web & code search in your terminal" \
  --command "$here/session.sh" \
  "$out"

# Drop tmux's "[exited]" line from the end of the recording.
grep -v '\[exited\]' "$out" >"$work/cast" && cat "$work/cast" >"$out"

echo "recorded $(wc -c <"$out") bytes -> $out"
