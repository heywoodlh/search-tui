#!/usr/bin/env bash
# Build the static website.
#
# Usage: site/build.sh OUT_DIR BASE_URL [DEMO_DIR]
#
# DEMO_DIR holds demo.webm, demo.mp4 and demo.png from demo/record.sh.
#
# Pages in site/pages/*.html are plain-text fragments (only links, a few
# headings and the demo container are markup). They are wrapped in a tiny
# template and laid out with `white-space: pre-wrap`, so `curl` output reads
# like a man page. A tag-free .txt twin of every page is published too.
set -euo pipefail

out="${1:?usage: build.sh OUT_DIR BASE_URL [DEMO_DIR]}"
base="${2:?usage: build.sh OUT_DIR BASE_URL [DEMO_DIR]}"
demo="${3:-}"
base="${base%/}/"
here="$(cd "$(dirname "$0")" && pwd)"
repo="https://github.com/heywoodlh/tui-search"

rm -rf "$out"
mkdir -p "$out"
cp "$here/style.css" "$out/"
cp "$here/../assets/icon.svg" "$out/icon.svg"
touch "$out/.nojekyll"

have_demo=false
if [[ -n "$demo" && -s "$demo/demo.mp4" && -s "$demo/demo.webm" ]]; then
  cp "$demo"/demo.{webm,mp4,png} "$out/"
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
      body="${body//\{\{DEMO\}\}/<video class=\"demo\" autoplay loop muted playsinline controls preload=\"metadata\" poster=\"demo.png\" width=\"1200\" height=\"720\" aria-label=\"search demo\"><source src=\"demo.webm\" type=\"video\/webm\"><source src=\"demo.mp4\" type=\"video\/mp4\"><\/video>}"
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
