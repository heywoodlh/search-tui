#!/usr/bin/env bash
# Render the website demo with VHS from demo/demo.tape.
#
# Usage: demo/record.sh OUT_DIR
# Produces OUT_DIR/demo.webm, demo.mp4 and demo.png (poster).
# Needs on PATH: search and vhs (which brings ttyd, ffmpeg and Chromium).
# Engines used (Wikipedia, Stack Overflow, Hacker News) need no
# credentials and give the demo a real results page.
set -euo pipefail

out="${1:?usage: record.sh OUT_DIR}"
here="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Isolated, deterministic environment for the recorded shell.
mkdir -p "$work/config/search-tui" "$work/out"
cp "$here/config.toml" "$work/config/search-tui/config.toml"
cp "$here/demo.tape" "$work/demo.tape"
export XDG_CONFIG_HOME="$work/config" COLORTERM=truecolor

# Pin the font when provided (CI passes nixpkgs#jetbrains-mono) so renders
# don't depend on the fonts installed on the machine.
if [[ -n "${DEMO_FONT_DIR:-}" ]]; then
  cat >"$work/fonts.conf" <<XML
<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "fonts.dtd">
<fontconfig>
  <dir>$DEMO_FONT_DIR</dir>
  <include ignore_missing="yes">/etc/fonts/fonts.conf</include>
  <cachedir>$work/fontconfig</cachedir>
</fontconfig>
XML
  export FONTCONFIG_FILE="$work/fonts.conf"
fi
# termenv (lipgloss) disables colour whenever $CI is set, as it is on CI
# runners; the recorded terminal is a real, colour-capable TTY.
unset DISPLAY WAYLAND_DISPLAY BROWSER CI

(cd "$work" && vhs demo.tape)

mkdir -p "$out"
cp "$work"/out/demo.{webm,mp4,png} "$out/"
ls -l "$out"/demo.*
