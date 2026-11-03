#!/usr/bin/env bash
# Build the static website.
#
# Usage: site/build.sh OUT_DIR BASE_URL [DEMO.cast]
#
# Pages in site/pages/*.html are plain-text fragments (only links, a few
# headings and the demo container are markup). They are wrapped in a tiny
# template and laid out with `white-space: pre-wrap`, so `curl` output reads
# like a man page. A tag-free .txt twin of every page is published too.
set -euo pipefail

out="${1:?usage: build.sh OUT_DIR BASE_URL [DEMO.cast]}"
base="${2:?usage: build.sh OUT_DIR BASE_URL [DEMO.cast]}"
cast="${3:-}"
base="${base%/}/"
here="$(cd "$(dirname "$0")" && pwd)"
repo="https://github.com/heywoodlh-bot/tui-search"

player_version=3.17.0
player_js_sha=a13c37632e1b5c49fe9128417b9319a9b5bc64cb457dd5ae52cbba8a3aceb880
player_css_sha=f619fe17597043564f03b2c6918b3daf890ee8b912fb408542fba11afade4fdb

rm -rf "$out"
mkdir -p "$out"
cp "$here/style.css" "$out/"
cp "$here/../assets/icon.svg" "$out/icon.svg"
touch "$out/.nojekyll"

# Self-host the asciinema player (pinned and checksummed).
fetch() {
  curl -fsSL --retry 3 -o "$out/$1" \
    "https://cdn.jsdelivr.net/npm/asciinema-player@${player_version}/dist/bundle/$1"
  echo "$2  $out/$1" | sha256sum -c --quiet -
}
have_demo=false
if [[ -n "$cast" && -s "$cast" ]]; then
  cp "$cast" "$out/demo.cast"
  fetch asciinema-player.min.js "$player_js_sha"
  fetch asciinema-player.css "$player_css_sha"
  have_demo=true
fi

# render NAME TITLE DESCRIPTION: site/pages/NAME.html -> OUT/(NAME/)index.html
render() {
  local name="$1" title="$2" desc="$3" dir root file
  case "$name" in
  index) dir="$out" root="./" ;;
  404) dir="$out" root="$base" ;; # served at arbitrary paths
  *) dir="$out/$name" root="../" ;;
  esac
  file="$dir/index.html"
  [[ "$name" == 404 ]] && file="$out/404.html"
  mkdir -p "$dir"
  local body
  # Indented (4+ space) blocks become <pre> so they scroll instead of wrapping
  # on narrow screens; everything else reflows.
  body="$(sed "s|{{BASE}}|$base|g; s|{{REPO}}|$repo|g" "$here/pages/$name.html" | awk '
    /^    / { if (!inpre) { printf "<pre>"; inpre = 1 } else if (blank) { printf "\n" }
             blank = 0; printf "%s%s", (started ? "\n" : ""), $0; started = 1; next }
    /^$/ && inpre { blank = 1; next }
    { if (inpre) { printf "</pre>\n"; if (blank) print ""; inpre = 0; started = 0; blank = 0 }
      print }
    END { if (inpre) printf "</pre>\n" }
  ')"
  if [[ "$name" == index ]]; then
    if $have_demo; then
      body="${body//\{\{DEMO\}\}/<div id=\"demo\" class=\"demo\"></div>}"
    else
      body="${body//\{\{DEMO\}\}/(demo recording unavailable for this build)}"
    fi
  fi
  {
    cat <<HTML
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>$title</title>
<meta name="description" content="$desc">
<link rel="icon" href="${root}icon.svg" type="image/svg+xml">
<link rel="stylesheet" href="${root}style.css">
HTML
    if [[ "$name" == index ]] && $have_demo; then
      echo '<link rel="stylesheet" href="asciinema-player.css">'
    fi
    cat <<HTML
</head>
<body>
<nav>
<a class="logo" href="${root}" aria-label="search: home"><img src="${root}icon.svg" alt="search" width="36" height="36"></a>
<a href="${root}install/"$( [[ $name == install ]] && echo ' aria-current="page"')>install</a>
<a href="${root}docs/"$( [[ $name == docs ]] && echo ' aria-current="page"')>docs</a>
<a href="$repo">github</a>
</nav>
<main>$body</main>
<footer>
search is free software under the <a href="$repo/blob/main/LICENSE">GPL-3.0</a>.
This site is plain text: <a href="${root}$name.txt">$name.txt</a> or curl ${base}$( [[ $name == index ]] || echo "$name/")
</footer>
HTML
    if [[ "$name" == index ]] && $have_demo; then
      cat <<'HTML'
<script src="asciinema-player.min.js"></script>
<script>
var still = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
AsciinemaPlayer.create("demo.cast", document.getElementById("demo"), {
  autoPlay: !still, loop: true, preload: true, idleTimeLimit: 2,
  fit: "width", terminalFontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
  poster: "npt:0:20"
});
</script>
HTML
    fi
    echo '</body>'
    echo '</html>'
  } >"$file"

  # Plain-text twin: strip tags and decode the few entities used.
  printf '%s\n' "$body" | sed -e 's/<[^>]*>//g' -e 's/&lt;/</g; s/&gt;/>/g; s/&quot;/"/g; s/&amp;/\&/g' >"$out/$name.txt"
}

render index "search: web and code search in your terminal" \
  "A fast terminal UI and CLI for web and code search engines, with !bangs, code search and on-device summaries."
render install "search: install" "Install search with Nix, home-manager, Go or prebuilt binaries."
render docs "search: docs" "Usage, keybindings, engines, configuration and themes for search."

render 404 "search: not found" "Page not found."

echo "built site in $out ($base)"
