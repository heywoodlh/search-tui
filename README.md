<img src="assets/icon.svg" width="64" height="64" alt="">

# tui-search

Website: <https://heywoodlh.github.io/tui-search/> (also readable with `curl`).

`search` is a fast terminal client for web and code search. It has a full
TUI and an inline CLI, DuckDuckGo-style `!bangs`, and summaries computed on
your device. It also integrates with `$EDITOR`. It is a single static Go
binary with no runtime dependencies, so it runs well on a Raspberry Pi Zero,
a phone (Termux), over SSH or in tmux, and on a full-size monitor.

```sh
search                                   # open the TUI
search "nixos flakes tutorial"           # inline results + summary
search '!w alan turing'                  # bangs work inline (single-quote ! in bash/zsh)
search --engine gh --code --lang go "tea.NewProgram"
search -E                                # write the query in $EDITOR
```

## Features

- **Two modes:** the TUI opens when you run `search` with no arguments.
  `search <query>` prints results inline, and `--json` or `--markdown` give
  output for scripts.
- **Engines:** DuckDuckGo (the default, no key needed), Google, Bing, Kagi,
  Brave, Mojeek, SearXNG, Wikipedia, Stack Overflow, Hacker News, GitHub,
  GitLab, Sourcegraph and Codeberg/Gitea/Forgejo. You can add custom
  URL, JSON-API or HTML-scraping engines. If a web search fails, a fallback
  engine is tried.
- **Bangs:** `!g`, `!ddg`, `!b`, `!k`, `!w`, `!so`, `!gh`, `!ghc` (GitHub
  code), `!sg` and so on. URL bangs (`!yt`, `!aw`, `!nix`, `!hm`, `!mdn` and
  about 40 more) open in your browser. A bare `!` is "I'm feeling lucky". In
  the TUI, Tab completes a bang. Run `search --list-bangs` to see them all.
- **Code search:** search GitHub, GitLab and Sourcegraph (Sourcegraph needs
  no key) with `--lang` and `--repo` filters. Results show line-numbered
  fragments. `Y` copies the code, and `e` opens the **full file** in
  `$EDITOR` at the matching line.
- **Summaries on your device:** extractive multi-document summarisation that
  finds the consensus across results and removes redundant sentences. It uses
  no model and no network, and runs in milliseconds on weak hardware. Opening
  a web result's details automatically fetches and summarises its page, with
  readable text extraction. You can optionally pipe results to a local LLM
  (`summary.command = "ollama run llama3.2:1b"`).
- **`$EDITOR` integration:** compose queries in a temporary Markdown file
  (`ctrl+e`, `search -E`). View the whole result set (`E`, `--edit`) or a
  fetched page as Markdown (`e`), or open source files (`e` on code results).
- **Navigation:** vim keys, arrow keys and the mouse (wheel, click, click
  again for details). All keybindings can be changed, and a status bar shows
  hints that fit the terminal width.
- **Responsive layout:** a compact selected-result peek stays pinned above
  the scrolling list, with a divider separating it from results. Opening a
  result shows the full-width page view on demand; it reflows on phones,
  tablets and wide terminals.
- **Credentials:** set them literally or with `*_env`, `*_file` or
  `*_command` (works with `pass`, `op`, `gh auth token`, agenix, sops-nix and
  so on).
- **Themes:** about 20 built-in colour themes (Catppuccin, Dracula, Nord,
  Gruvbox, Solarized, Tokyo Night, One Dark, Rosé Pine, Kanagawa, GitHub and
  more), plus `ansi`, which follows your terminal's own palette. Themes only
  change foreground colours and never paint the background.
- **Browser choice:** set `browser`, or pass `--browser`, to pick the browser
  for opening links. If it's unset, `$BROWSER` and then the system default
  are used.
- **Low-power mode:** `low_power = true` uses fewer results, smaller download
  caps, no spinner animation and the summary bar is off by default.
- **Packaging:** a Nix flake and home-manager module for x86_64/aarch64 Linux
  and aarch64 macOS, plus Windows builds.

## Install

### Nix

```sh
nix run github:heywoodlh/tui-search -- "hello world"
nix profile install github:heywoodlh/tui-search
```

The flake provides `packages.<system>.default`, `overlays.default`,
`homeManagerModules.default` (also exported as `homeModules.default`), and a
`devShells.default`.

### home-manager

```nix
{
  inputs.tui-search.url = "github:heywoodlh/tui-search";
  # ...
  home-manager.users.me = {
    imports = [ inputs.tui-search.homeManagerModules.default ];
    programs.tui-search = {
      enable = true;
      settings = {
        default_engine = "kagi";
        default_code_engine = "github";
        summary.sentences = 4;
        ui.layout = "auto";
        ui.theme = "catppuccin-mocha";
        browser = "firefox";
        keys.open = [ "o" "enter" ];
        bangs.nw = "https://wiki.nixos.org/w/index.php?search={query}";
        engines = {
          kagi.token_file = "~/.config/kagi/session-token";
          github.token_command = "gh auth token";
        };
      };
    };
  };
}
```

`settings` accepts every key of the TOML config below. It is written to
`~/.config/tui-search/config.toml` on both Linux and macOS. Keep secrets out
of `settings` because they would end up in the Nix store. Use `*_file`,
`*_command` or `*_env` instead.

### Go, including Windows

```sh
# private repo: export GOPRIVATE=github.com/heywoodlh/*
go install github.com/heywoodlh/tui-search/cmd/search@latest
```

On Windows the config file lives at `%AppData%\tui-search\config.toml`. The
default editor is `notepad`, `clip` handles the clipboard, and links open in
the default browser. Windows Terminal supports the mouse and colours.

### Prebuilt binaries

Every [release](https://github.com/heywoodlh/tui-search/releases) ships these
binaries, plus a `SHA256SUMS` file:

| OS | Architectures | Linking |
| --- | --- | --- |
| Linux | amd64, arm64, armv6 (runs on armv6/v7, e.g. Raspberry Pi), riscv64 | static: runs on any distribution (glibc, musl, NixOS, ...) |
| FreeBSD | amd64, arm64 | static |
| NetBSD | amd64, arm64 | static |
| OpenBSD | amd64, arm64 | base-system `libc`/`libpthread` only (OpenBSD requires system calls to go through libc, so Go can't build a static binary there) |
| macOS | arm64 | `libSystem`, like every macOS program |
| Windows | amd64, arm64 | `.exe` |

```sh
curl -LO https://github.com/heywoodlh/tui-search/releases/latest/download/search-linux-amd64
chmod +x search-linux-amd64 && mv search-linux-amd64 ~/.local/bin/search
```

CI builds the same binaries on every pull request and fails if any Linux,
FreeBSD or NetBSD binary isn't statically linked, or if an OpenBSD binary
links anything beyond libc (`scripts/build-release.sh`).

## Usage

```text
search                       open the TUI
search [flags] <query...>    search inline

  -e, --engine NAME     engine name or bang alias (ddg, g, b, k, gh, sg, ...)
  -c, --code            code search (default_code_engine unless --engine supports code)
  -t, --type TYPE       web | code | repos | issues
  -l, --lang LANG       code language filter        -r, --repo OWNER/NAME
  -n, --limit N         number of results           -p, --page N
  -s, --summary         print the summary           --no-summary
  -j, --json            JSON output                 -m, --markdown
      --result N        select the Nth result (1-based)
      --field FIELD     print url, title, snippet or code from one result
  -o, --open            open the search page in a browser
  -1, --lucky           open the first result
  -u, --url             print the engine's search URL
  -i, --tui             open the TUI with the query
  -E, --compose         write the query in $EDITOR  --edit (results in $EDITOR)
      --theme NAME      colour theme                --list-themes
      --browser CMD     browser for opening URLs (overrides config)
      --list-engines    --list-bangs                --example-config
```

Queries can also come from stdin: `echo "rust lifetimes" | search -m`.
For scripts, `search --field url --result 2 'query'` prints just one URL;
`--result` also filters `--json` and `--markdown` output. Invalid or missing
result numbers fail rather than printing an empty value.

### TUI keybindings (defaults)

| Keys | Action |
| --- | --- |
| `/`, `i`, `ctrl+l` | jump to the search bar to edit the query (like a browser URL bar) |
| `enter`, `l`, `→`, `space` | show details; web pages are fetched and summarised automatically |
| `j`/`k`, `↓`/`↑` | move down/up, or scroll |
| `gg`/`G`, `home`/`end` | first/last result; with a count (`5G`, `5gg`), result N |
| `ctrl+d`/`ctrl+u`, `pgdn`/`pgup` | page down/up |
| `esc` | in the search bar: back to normal mode only. In normal mode: back one level, details → results → start page |
| `h`, `←` | close the details view |
| `o` / `O` | open the selected URL / the engine's search page in the browser (`o` also works in VIEW) |
| `y` / `Y` / `M` | copy the URL / snippet or code / Markdown link |
| `e` | open the result in `$EDITOR` (page as Markdown, or the full code file at the matching line) |
| `E` | open all results (with summary) as Markdown in `$EDITOR` |
| `S` | toggle the results summary |
| `n`/`p`, `]`/`[` | next/previous results page (`N` also goes back) |
| `tab` / `shift+tab` | engine picker dropdown (see below) |
| `t` | cycle search type (web → code → repos → issues) |
| `ctrl+e`, `c` | compose the query in `$EDITOR` |
| `r` | re-run the search |
| `?` | help (all bindings, commands and bangs) |
| `:` | command line (see below) |
| `v` / `V` | visual mode: select text in the details, `y` copies it |
| `f` / `F` | hints: label visible results; type a label to jump to it / open it |
| `gt` / `gT` | next / previous tab (`Ngt`: tab N) |
| `ctrl+w` … | pane commands (see below) |
| `q` / `ctrl+c` | close the pane (quit when it is the last one) / quit |

In insert mode, `enter` searches, `esc` returns to the results, `tab`
completes a `!bang` (or opens the engine picker), `↑`/`↓` move through
history and `ctrl+e` opens `$EDITOR`. New panes and tabs start in normal mode;
press `/` to edit their queries. The active mode appears once at the bottom
left of the whole application; the focused pane's full border is highlighted.
Bing sometimes serves the same HTML results for a requested next page; the
TUI reports this rather than relabeling repeated results as a new page.

Result and fetched-page links are numbered `[1]`, `[2]`, … in the TUI.
`:open-link 2` opens a link in the browser and `:copy-link 2` copies it;
both also accept a full HTTP(S) URL. After either command and a space, Tab
lists the available numbered links and cycles through them (Shift+Tab goes
backwards). Fetched-page links become available after `s` or `:summarise`.

**Engine picker:** `tab` opens a dropdown under the search bar listing every
usable engine with its bang and search types. Move with `↑`/`↓`, `j`/`k`, or
by pressing `tab` repeatedly (`shift+tab` moves up). A letter jumps to the
next engine starting with it, the mouse wheel and clicks work, `enter`
switches engine and re-runs the query, and `esc` closes it.

**Panes:** split the screen like vim to run several searches side by side,
for example the same query on two engines. Each pane is independent, with
its own query, engine, results, details and status line. The focused pane's
status line is highlighted.

| Keys | Action |
| --- | --- |
| `ctrl+w s` | split: new pane below (stacked) |
| `ctrl+w v` | vsplit: new pane to the right (side by side) |
| `ctrl+w h`/`j`/`k`/`l` (or arrows) | focus the pane in that direction |
| `ctrl+w w` / `ctrl+w W` | focus the next / previous pane |
| `ctrl+w q`, `ctrl+w c` | close the pane (`q` quits on the last pane) |
| `ctrl+w o` | keep only the focused pane |

- **New panes** start as a copy of the current search.
- **Splits** in the same direction share the space equally.
- **Too small:** a split is refused if a pane would get narrower than 24 columns or shorter than 6 rows.
- **Search bar:** pane commands work outside it; inside, `ctrl+w` deletes a word as in vim's insert mode.
- **Mouse:** clicking a pane focuses it.
- **Keys:** the `ctrl+w` prefix is the `window` action in `[keys]`. You can also bind `split_horizontal`, `split_vertical`, `focus_next_pane` and `close_pane` directly.
- **More `ctrl+w` keys:** `n` new empty pane, `T` move the pane to a new tab,
  `+`/`-` and `<`/`>` resize, `_`/`|` maximise, `=` equal sizes.

**Counts:** like vim, a number before a key repeats or targets it: `5j`
moves five results, `3G` goes to result 3, `2gt` goes to tab 2. The status
bar shows the count and any pending keys. Keys in `[keys]` can be
sequences, e.g. `top = ["gg"]`.

**Hints (`f`/`F`):** as in Vimium or qutebrowser, `f` labels each visible
result with home-row letters (`hint_chars` in `[ui]`). Type a label to jump
to that result and show its details; `F` opens it in the browser instead.

**Visual mode (`v`/`V`):** select text in the selected result's details,
including its title, URL, snippet or code, and the fetched page. Use vim
motions:

- `h` `j` `k` `l`, `w` `b` `e`, `0` `^` `$`
- `gg` `G`, `ctrl+d` `ctrl+u`
- `f` `F` `t` `T` + a character, with `;` and `,` to repeat
- counts, and `o` to jump to the other end of the selection

`y` copies the selection and `Y` copies whole lines. `v` and `V` switch
between selecting characters and lines. The selection is shown with
underlined accent-coloured text, not a background.

**Command line (`:`):** vim/[iamb](https://iamb.chat)-style commands. Tab
completes command names and arguments, `↑`/`↓` recall history, and
abbreviations work (`:sp`, `:vs`, `:tabn`). `:N` jumps to result N.
`:help commands` lists everything.

| Command | Action |
| --- | --- |
| `:q[uit]`, `:qa[ll]` | close the pane (the tab, or quit, when it's the last) / quit everything |
| `:clo[se]`, `:on[ly]` | close the pane (never the last) / close every other pane |
| `:sp[lit] [query]`, `:vs[plit] [query]` | split below / to the right, copying this search or running query |
| `:new [query]`, `:vne[w] [query]` | new empty pane below / to the right |
| `:res[ize] [+-]N` | pane height (with `:vertical`, the width) |
| `:vert[ical]`, `:abo[veleft]`, `:bel[owright]`, `:tab` | modifiers: `:vert new rust`, `:abo split`, `:tab split` |
| `:tabnew [query]`, `:tabc[lose]`, `:tabo[nly]` | new tab / close tab / close other tabs |
| `:tabn[ext] [N]`, `:tabp[revious]`, `:tabfir[st]`, `:tabl[ast]`, `:tabm[ove] [N]` | tab navigation and ordering |
| `:s[earch] {query}`, `:eng[ine] [name]`, `:ty[pe] [kind]` | search / switch engine (picker without a name) / set the type |
| `:pa[ge] N`, `:n[ext]`, `:prev[ious]`, `:ref[resh]`, `:wel[come]` | pages / re-run / back to the start page |
| `:o[pen] [N]`, `:ya[nk] [N]`, `:co[py] [N]`, `:e[dit] [N]`, `:sum[marise] [N]` | act on result N (default: the selected one) |
| `:exp[ort]`, `:comp[ose]` | all results as Markdown in `$EDITOR` / write the query in `$EDITOR` |
| `:h[elp] [topic]`, `:the[me] [name]`, `:se[t] {option}` | help, live theme switching, options (`summary`, `nosummary`, `layout=split`, `results=N`, ...) |
| `:engines`, `:ban[gs]`, `:his[tory]`, `:ve[rsion]`, `:redr[aw]` | lists and utilities |

Mouse: the wheel scrolls, a click selects a result, a second click opens its
details, and clicking the query line edits it.

Rebind any action under `[keys]`:

```toml
[keys]
down = ["j", "down", "ctrl+n"]
up = ["k", "up", "ctrl+p"]
open = ["o", "enter"]
details = ["l", "right"]
```

## Engines and credentials

| Engine | Bangs | Types | Auth |
| --- | --- | --- | --- |
| duckduckgo (default) | `!ddg` `!d` | web | none (HTML endpoint) |
| google | `!g` | web | `api_key` + `cx` ([Programmable Search](https://programmablesearchengine.google.com/)) |
| bing | `!b` | web | none (HTML, best effort) |
| kagi | `!k` | web | `api_key` (Search API) **or** `token` (Session Link token) |
| brave | `!br` | web | `api_key` |
| mojeek | `!m` | web | `api_key` (scraping is captcha'd) |
| searxng | `!sx` | web | `base_url`; optional `username`/`password` or `token` |
| wikipedia | `!w` | web | none (`language = "de"` etc.) |
| stackoverflow | `!so` | web | none (optional `api_key` for quota) |
| hackernews | `!hn` | web | none |
| github | `!gh` `!ghc` `!ghi` | repos, code, issues | code search needs a token: `token*`, `$GITHUB_TOKEN`/`$GH_TOKEN`, or `gh auth token` (automatic) |
| gitlab | `!gl` `!glc` `!gli` | repos, code, issues | code/issues need `token*` or `$GITLAB_TOKEN` |
| sourcegraph | `!sg` `!sgc` | code, repos | none for sourcegraph.com; `token` for private instances |
| codeberg | `!cb` | repos | optional `token`; any Gitea/Forgejo via `base_url` |

Engines that need configuration (Google, Kagi, Brave, Mojeek, SearXNG and
custom instances of them) stay hidden until it is set. They don't appear in
the engine picker, in `--list-engines` (which shows what to set at the end),
or as fallbacks. Their bangs still work: `!g` opens Google in your browser
until you add an `api_key` and `cx`. If `default_engine` names an unconfigured
engine, DuckDuckGo is used and a warning is shown. A credential only counts as
configured when its source is set; the `*_command` itself isn't run until you
search.

Each credential (`api_key`, `token`, `password`, `cx`) can be set as:

```toml
[engines.brave]
api_key = "literal"                      # or
api_key_env = "BRAVE_API_KEY"            # or
api_key_file = "~/.secrets/brave"        # or (agenix / sops-nix paths work too)
api_key_command = "pass show brave/api"  # or op read ..., gh auth token, ...
```

Google, Kagi, Brave and paid APIs fail with a clear message when credentials
are missing. Press `O` (or use `--open`) to run the same search in your
browser instead. When the default web engine fails for another reason (rate
limiting, captcha), `fallback_engine` (Bing by default, `"none"` disables it)
is tried and the status bar says so.

### Custom engines

```toml
# Browser-only
[engines.nixwiki]
type = "url"
url = "https://wiki.nixos.org/w/index.php?search={query}"
bangs = ["nw"]

# Another instance of a built-in type (gitlab, github, searxng, sourcegraph, gitea/forgejo)
[engines.work]
type = "gitlab"
base_url = "https://gitlab.example.com"
token_command = "pass show work/gitlab"
bangs = ["work"]

# Any JSON API
[engines.myapi]
type = "json"
url = "https://api.example.com/search?q={query}&page={page}&n={limit}"
headers = { Authorization = "Bearer {token}" }
token_env = "MYAPI_TOKEN"
results_path = "data.items"
title_field = "name"
url_field = "link"
snippet_field = "description"

# Any HTML page
[engines.lobsters]
type = "html"
url = "https://lobste.rs/search?q={query}&what=stories"
result_selector = "li.story"
link_selector = "a.u-url"
bangs = ["lob"]
```

URL templates support `{query}`, `{query_path}`, `{query_raw}`, `{page}`,
`{page0}`, `{limit}`, `{offset}` and `{offset1}`. The `[bangs]` table can
also map a bang to an engine (`"bing"`), an engine and type (`"gitlab:code"`),
a type alone (`":code"`), or a URL template.

## Configuration

The config file is `$XDG_CONFIG_HOME/tui-search/config.toml` (by default
`~/.config/tui-search/config.toml`). Override it with `--config` or
`$TUI_SEARCH_CONFIG`. Every key is optional. `search --example-config` prints
a fully commented example ([source](internal/config/example.toml)).

```toml
default_engine = "duckduckgo"
default_code_engine = "github"   # default: github with a token, else sourcegraph
fallback_engine = "bing"         # "none" to disable
results = 20
timeout = "15s"
low_power = false
browser = ""                     # see "Browser" below; unset = $BROWSER, then the system default
editor = ""                      # $VISUAL / $EDITOR
clipboard = "auto"               # auto | osc52 | tmux | command | none
# clipboard_command = "tmux load-buffer -"  # runs through shell; copied text on stdin

[summary]
enabled = true
sentences = 3
# command = "ollama run llama3.2:1b"

[ui]
mouse = true
layout = "auto"                  # legacy setting; details now open full-width on demand
split_min_width = 110
snippet_lines = 2
theme = "default"                # search --list-themes
# colors = { accent = "#f5c2e7" } # per-colour overrides
ascii = false
```

Clipboard support: `auto` uses `wl-copy`, `xclip`/`xsel`, `pbcopy`, `clip`
or `termux-clipboard-set` when it finds one. Otherwise it falls back to OSC 52,
which works over SSH and inside tmux and screen. `clipboard = "tmux"` loads the
tmux paste buffer directly. Set `clipboard_command` to a custom shell command
or pipeline reading the text on stdin (for example `tmux load-buffer -`);
it takes precedence in `auto`, `command` and `tmux` modes.

## Themes

Set `ui.theme` (or pass `--theme`). In the TUI, `:theme NAME` changes the
palette for the current session only; Tab completes built-in names and the
config file is not edited. `search --list-themes` prints every theme
with a colour preview:

`default`, `ansi` (uses your terminal's 16 colours, so it matches whatever
theme your terminal emulator uses), `mono`, `catppuccin-mocha`,
`catppuccin-macchiato`, `catppuccin-frappe`, `catppuccin-latte`, `dracula`,
`nord`, `gruvbox-dark`, `gruvbox-light`, `solarized-dark`, `solarized-light`,
`tokyo-night`, `tokyo-night-day`, `one-dark`, `one-light`, `monokai`,
`everforest`, `rose-pine`, `rose-pine-dawn`, `kanagawa`, `github-dark` and
`github-light`.

Themes only set foreground colours and text attributes. The TUI never paints
a background: the status bar, engine label, mode indicator, selection and
engine picker all use coloured text, and the cursor is an underlined
character rather than a reverse-video block. Your terminal's background,
transparency and blur stay as they are. Pick a light theme (`*-latte`,
`*-light`, `*-day`, `*-dawn`) on a light terminal background.

You can override individual colours with hex or ANSI 0-255 values; an empty
string means the terminal default:

```toml
[ui]
theme = "nord"
colors = { accent = "#ebcb8b", url = "4", text = "" }
# keys: accent title text url muted border code key error success insert normal view
```

The older `accent_color` and `muted_color` keys still work as shorthands.

## Browser

Links open in, from highest priority:

1. `--browser CMD`, then `browser = "..."` in the config. This is a command
   line, with an optional `%s` or `{url}` placeholder (otherwise the URL is
   appended), e.g. `"firefox --new-tab"` or `"chromium --incognito %s"`. On
   macOS a bare application name also works: `browser = "Google Chrome"` runs
   `open -a "Google Chrome"`. On Windows, give the browser's path, e.g.
   `'C:\Program Files\Mozilla Firefox\firefox.exe'`.
   `browser = "default"` forces the system default.
   Only `http` and `https` links are ever opened.
2. `$BROWSER`, which may be a colon-separated list; the first one found is used.
3. The system default: `xdg-open` on Linux and BSD, `open` on macOS, the
   default browser on Windows, `termux-open-url` on Android/Termux and
   `wslview` under WSL.

Terminal browsers (`w3m`, `lynx`, `links`, `elinks`, `browsh`, `carbonyl`,
`chawan`) take over the terminal and return you to the TUI when they exit.

## How summaries work

`internal/summary` is a centroid-based multi-document summariser:

1. Snippets are split into sentences. Boilerplate such as date prefixes and
   ellipses is stripped, then words are tokenised, stop words removed and the
   rest stemmed.
2. A weighted "centroid" of the result set is built. Terms that appear in many
   *different* results score higher (consensus × IDF).
3. Each sentence is scored by cosine similarity to the centroid, plus query
   overlap, plus a small boost for higher-ranked results. Code-heavy,
   truncated and fragmentary sentences are penalised.
4. Sentences are chosen with maximal marginal relevance, so near-duplicates
   are skipped.

The work is linear in input size, needs no model files, and allocates little
memory. Fetched pages (`s`) go through the same pipeline after a readability
pass that drops navigation, scripts and footers. Downloads are capped by
`max_body_kb`.

## Development

```sh
nix develop          # go, gopls, gotools
go test ./...
go run ./cmd/search
nix flake check      # builds the package (runs tests) and checks the home-manager module
nix develop .#ci -c scripts/check.sh   # every lint/security gate CI enforces
```

Layout: `cmd/search` (entry point), `internal/cli` (flags, inline output),
`internal/tui` (Bubble Tea UI), `internal/engine` (backends),
`internal/bang`, `internal/summary`, `internal/app` (shared core),
`internal/sys` (clipboard/browser/editor), `internal/config`, and `nix/`
(package and home-manager module).

### Website and demo

The site lives in `site/`. Pages are plain-text fragments in `site/pages/`, so
`curl` output stays readable, and `site/build.sh` wraps them in a small
template. The demo video is rendered by [VHS](https://github.com/charmbracelet/vhs)
from `demo/demo.tape`, which drives the real CLI and TUI:

```sh
nix shell .#default nixpkgs#vhs -c demo/record.sh /tmp/demo   # demo.webm, demo.mp4, demo.png
site/build.sh /tmp/_site http://localhost:8000/ /tmp/demo
```

The `Pages` workflow does both on every push to `main` and deploys to the
`gh-pages` branch. For every pull request it deploys a preview to
`pr-preview/pr-N/` and comments the URL on the PR.

Pull requests must pass the `posture` check, which aggregates tests, Nix,
golangci-lint (with gosec), govulncheck, actionlint and zizmor, shellcheck,
gitleaks and nixfmt (see [SECURITY.md](SECURITY.md)).

### Versioning and releases

`search` follows strict [Semantic Versioning 2.0.0](https://semver.org). The
version lives in [`VERSION`](VERSION), which feeds the Nix package, the
release binaries and `search --version`.

- **PR titles** must be [Conventional Commits](https://www.conventionalcommits.org)
  (`type(scope)!: description`), because PRs are squash-merged and the title
  becomes the commit on `main`.
- **The bump is fixed by the type.** Each PR must change `VERSION` by exactly
  this step; the required `semver` check (`tools/semver`) fails otherwise:

  | PR title | VERSION change |
  | --- | --- |
  | `feat!: …`, `fix(cli)!: …`, or a `BREAKING CHANGE:` footer | major (minor while `0.y.z`, or straight to `1.0.0`) |
  | `feat: …` | minor (`1.4.2` → `1.5.0`) |
  | `fix: …`, `perf: …`, `revert: …` | patch (`1.4.2` → `1.4.3`) |
  | `build`, `chore`, `ci`, `docs`, `refactor`, `style`, `test` | none |

- **Releases are automatic.** When a version bump reaches `main`, the
  `Release` workflow tags `vX.Y.Z` and publishes the binaries. Versions with a
  pre-release suffix (`1.0.0-rc.1`) are published as pre-releases.
- **Dependabot PRs** are titled `chore(deps): …` and don't release. To ship a
  dependency fix, for example a security update, retitle the PR `fix(deps): …`
  and bump the patch version.

Check locally with `go run ./tools/semver check -title "feat: x" -base 0.1.0 -head "$(cat VERSION)"`.

When you update Go dependencies, refresh `vendorHash` in `nix/package.nix`
(build once and copy the `got:` hash).

## Security

[SECURITY.md](SECURITY.md) explains how to report a vulnerability, the threat
model (terminal-escape sanitising, link and redirect safety, editor
hardening) and the CI posture every pull request must pass.

## License

[GPL-3.0-only](LICENSE). Copyright (C) 2026 Spencer Heywood.
