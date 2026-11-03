#!/usr/bin/env bash
# Types into the tmux session like a person would.
set -uo pipefail
T=(tmux -L demo)

say() { # type text with a human-ish cadence
  local s="$1" i
  for ((i = 0; i < ${#s}; i++)); do
    "${T[@]}" send-keys -t demo -l -- "${s:i:1}"
    sleep 0.045
  done
}
key() { "${T[@]}" send-keys -t demo "$@"; }
pause() { sleep "$1"; }

pause 1.5

# 1. Inline search with a !bang and the on-device summary.
say "search -n 3 --summary '!w nix package manager'"; pause 0.4; key Enter; pause 6
say 'clear'; key Enter; pause 0.5

# 2. The TUI.
say 'search'; key Enter; pause 2
say 'rust borrow checker !so'; pause 0.5; key Enter; pause 6
key j; pause 0.7; key j; pause 0.7; key k; pause 1
key Enter; pause 2                       # details
key Escape; pause 1                      # back to results

# 3. Switch engines with the tab picker and run a code search.
key Escape; pause 1.2                    # back to the start page
say 'bubbletea tui framework'; pause 0.4
key Tab; pause 1; key Tab; pause 0.8; key Tab; pause 0.8
key g; pause 1                           # jump to github
key Enter; pause 5                       # GitHub repositories
key j; pause 1
key Escape; pause 1.2                    # start page
say 'tea.NewProgram !ghc'; pause 0.4; key Enter; pause 6   # GitHub code search
key j; pause 1; key Enter; pause 3       # code result details
key Escape; pause 0.8

# 4. Help, then quit.
key '?'; pause 3
key '?'; pause 0.6
key q; pause 1

# 5. Themes.
say 'search --list-themes'; key Enter; pause 4
say 'exit'; key Enter
