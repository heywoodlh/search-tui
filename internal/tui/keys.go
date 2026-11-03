package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

// Action identifies a bindable command. Names match the [keys] config table.
type Action string

const (
	ActUp             Action = "up"
	ActDown           Action = "down"
	ActTop            Action = "top"
	ActBottom         Action = "bottom"
	ActPageDown       Action = "page_down"
	ActPageUp         Action = "page_up"
	ActDetails        Action = "details"
	ActBack           Action = "back"
	ActOpen           Action = "open"
	ActOpenSearchPage Action = "open_search_page"
	ActSearch         Action = "search"
	ActYankURL        Action = "yank_url"
	ActYankSnippet    Action = "yank_snippet"
	ActYankMarkdown   Action = "yank_markdown"
	ActEdit           Action = "edit"
	ActEditResults    Action = "edit_results"
	ActCompose        Action = "compose"
	ActSummarizePage  Action = "summarize_page"
	ActToggleSummary  Action = "toggle_summary"
	ActNextPage       Action = "next_page"
	ActPrevPage       Action = "prev_page"
	ActNextEngine     Action = "next_engine"
	ActPrevEngine     Action = "prev_engine"
	ActCycleType      Action = "cycle_type"
	ActRefresh        Action = "refresh"
	ActHelp           Action = "help"
	ActQuit           Action = "quit"
)

type actionDef struct {
	act  Action
	keys []string
	desc string
	// short is the label used in the status bar; empty hides it there.
	short string
}

// actionDefs is ordered by status-bar priority.
var actionDefs = []actionDef{
	{ActSearch, []string{"/", "i"}, "edit query (insert mode)", "search"},
	{ActDetails, []string{"enter", "l", "right", " "}, "show result details / focus preview", "details"},
	{ActOpen, []string{"o"}, "open result in browser", "open"},
	{ActYankURL, []string{"y"}, "copy result URL", "yank"},
	{ActEdit, []string{"e"}, "open result (page or code file) in $EDITOR", "edit"},
	{ActSummarizePage, []string{"s"}, "fetch & summarise the result page", "summarise"},
	{ActHelp, []string{"?"}, "toggle help", "help"},
	{ActQuit, []string{"q", "ctrl+c"}, "quit", "quit"},
	{ActUp, []string{"k", "up"}, "previous result / scroll up", ""},
	{ActDown, []string{"j", "down"}, "next result / scroll down", ""},
	{ActTop, []string{"g", "home"}, "first result", ""},
	{ActBottom, []string{"G", "end"}, "last result", ""},
	{ActPageDown, []string{"ctrl+d", "pgdown", "ctrl+f"}, "page down", ""},
	{ActPageUp, []string{"ctrl+u", "pgup", "ctrl+b"}, "page up", ""},
	{ActBack, []string{"esc", "h", "left"}, "back", "back"},
	{ActYankSnippet, []string{"Y"}, "copy snippet / code fragment", "copy code"},
	{ActYankMarkdown, []string{"M"}, "copy result as Markdown link", ""},
	{ActEditResults, []string{"E"}, "open all results as Markdown in $EDITOR", ""},
	{ActCompose, []string{"ctrl+e", "c"}, "compose query in $EDITOR", ""},
	{ActOpenSearchPage, []string{"O"}, "open the engine's search page in browser", ""},
	{ActToggleSummary, []string{"S"}, "toggle results summary", ""},
	{ActNextPage, []string{"n", "]"}, "next results page", "next pg"},
	{ActPrevPage, []string{"N", "["}, "previous results page", ""},
	{ActNextEngine, []string{"tab"}, "next engine (re-runs query)", "engine"},
	{ActPrevEngine, []string{"shift+tab"}, "previous engine", ""},
	{ActCycleType, []string{"t"}, "cycle search type (web/code/repos/issues)", "type"},
	{ActRefresh, []string{"r", "ctrl+r"}, "re-run search", ""},
}

// keyMap holds resolved bindings.
type keyMap struct {
	b     map[Action]key.Binding
	order []Action
}

// newKeyMap applies user overrides. Unknown action names are reported.
func newKeyMap(overrides map[string][]string) (keyMap, []string) {
	km := keyMap{b: map[Action]key.Binding{}}
	known := map[Action]bool{}
	for _, d := range actionDefs {
		keys := d.keys
		if o, ok := overrides[string(d.act)]; ok {
			keys = o
		}
		km.b[d.act] = key.NewBinding(key.WithKeys(keys...), key.WithHelp(displayKeys(keys), d.desc))
		if len(keys) == 0 {
			km.b[d.act] = key.NewBinding(key.WithDisabled())
		}
		km.order = append(km.order, d.act)
		known[d.act] = true
	}
	var unknown []string
	for k := range overrides {
		if !known[Action(k)] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	return km, unknown
}

func displayKeys(keys []string) string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		switch k {
		case " ":
			k = "space"
		case "up":
			k = "↑"
		case "down":
			k = "↓"
		case "left":
			k = "←"
		case "right":
			k = "→"
		}
		out = append(out, k)
	}
	return strings.Join(out, "/")
}

func (km keyMap) is(msg fmt.Stringer, a Action) bool {
	b, ok := km.b[a]
	if !ok || !b.Enabled() {
		return false
	}
	s := msg.String()
	for _, k := range b.Keys() {
		if k == s {
			return true
		}
	}
	return false
}

// first returns the first key of an action for hints.
func (km keyMap) first(a Action) string {
	b := km.b[a]
	if ks := b.Keys(); len(ks) > 0 {
		return displayKeys(ks[:1])
	}
	return ""
}

// hints returns (key, label) pairs for the status bar in priority order.
func (km keyMap) hints(acts ...Action) [][2]string {
	var out [][2]string
	for _, a := range acts {
		var label string
		for _, d := range actionDefs {
			if d.act == a {
				label = d.short
			}
		}
		if k := km.first(a); k != "" && label != "" {
			out = append(out, [2]string{k, label})
		}
	}
	return out
}

// helpLines renders the full binding table.
func (km keyMap) helpLines() [][2]string {
	var out [][2]string
	for _, a := range km.order {
		b := km.b[a]
		if !b.Enabled() {
			continue
		}
		out = append(out, [2]string{b.Help().Key, b.Help().Desc})
	}
	return out
}
