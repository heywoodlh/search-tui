#!/usr/bin/env bash
# Runs inside the asciinema pty: start a tmux session with a plain shell,
# drive it from the background, and attach so tmux renders into the
# recording. Exits when the driven shell exits.
set -euo pipefail
T=(tmux -L demo -f "$DEMO_DIR/tmux.conf")
"${T[@]}" new-session -d -s demo -x 100 -y 28 \
  "env PS1='\[\e[38;2;234;88;12m\]❯\[\e[0m\] ' bash --norc --noprofile"
"$DEMO_DIR/driver.sh" &
exec "${T[@]}" attach -t demo
