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
	ActCloseDetails   Action = "close_details"
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

	// Panes. ActWindow is a vim-style prefix (ctrl+w) followed by s, v, w,
	// h/j/k/l, q, c or o. The direct actions have no default keys.
	ActWindow     Action = "window"
	ActSplitHoriz Action = "split_horizontal"
	ActSplitVert  Action = "split_vertical"
	ActFocusNext  Action = "focus_next_pane"
	ActClosePane  Action = "close_pane"

	// Vim-style extras.
	ActCommand    Action = "command"     // ":" command line
	ActVisual     Action = "visual"      // characterwise visual mode on the details text
	ActVisualLine Action = "visual_line" // linewise visual mode
	ActHint       Action = "hint"        // label visible results; type a label to jump
	ActHintOpen   Action = "hint_open"   // like hint, but open the result in the browser
	ActNextTab    Action = "next_tab"
	ActPrevTab    Action = "prev_tab"
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
	{ActSearch, []string{"/", "i", "ctrl+l"}, "edit query in the search bar (insert mode)", "search"},
	{ActDetails, []string{"enter", "l", "right", " "}, "show result details / focus preview", "details"},
	{ActOpen, []string{"o"}, "open result in browser", "open"},
	{ActYankURL, []string{"y"}, "copy result URL", "yank"},
	{ActEdit, []string{"e"}, "open result (page or code file) in $EDITOR", "edit"},
	{ActSummarizePage, []string{"s"}, "fetch & summarise the result page", "summarise"},
	{ActHelp, []string{"?"}, "toggle help", "help"},
	{ActQuit, []string{"q", "ctrl+c"}, "quit", "quit"},
	{ActUp, []string{"k", "up"}, "previous result / scroll up", ""},
	{ActDown, []string{"j", "down"}, "next result / scroll down", ""},
	{ActTop, []string{"gg", "home"}, "first result (with a count: result N)", ""},
	{ActBottom, []string{"G", "end"}, "last result (with a count: result N)", ""},
	{ActPageDown, []string{"ctrl+d", "pgdown", "ctrl+f"}, "page down", ""},
	{ActPageUp, []string{"ctrl+u", "pgup", "ctrl+b"}, "page up", ""},
	{ActBack, []string{"esc"}, "back: details → results → start page", "back"},
	{ActCloseDetails, []string{"h", "left"}, "close details", ""},
	{ActYankSnippet, []string{"Y"}, "copy snippet / code fragment", "copy code"},
	{ActYankMarkdown, []string{"M"}, "copy result as Markdown link", ""},
	{ActEditResults, []string{"E"}, "open all results as Markdown in $EDITOR", ""},
	{ActCompose, []string{"ctrl+e", "c"}, "compose query in $EDITOR", ""},
	{ActOpenSearchPage, []string{"O"}, "open the engine's search page in browser", ""},
	{ActToggleSummary, []string{"S"}, "toggle results summary", ""},
	{ActNextPage, []string{"n", "]"}, "next results page", "next pg"},
	{ActPrevPage, []string{"N", "["}, "previous results page", ""},
	{ActNextEngine, []string{"tab"}, "engine picker (press again to move down)", "engines"},
	{ActPrevEngine, []string{"shift+tab"}, "engine picker (move up)", ""},
	{ActCycleType, []string{"t"}, "cycle search type (web/code/repos/issues)", "type"},
	{ActRefresh, []string{"r", "ctrl+r"}, "re-run search", ""},
	{ActCommand, []string{":"}, "command line (:help commands)", ""},
	{ActVisual, []string{"v"}, "visual mode: select text in the details (y copies)", ""},
	{ActVisualLine, []string{"V"}, "linewise visual mode", ""},
	{ActHint, []string{"f"}, "hints: label results, type a label to jump to it", ""},
	{ActHintOpen, []string{"F"}, "hints: type a label to open that result in the browser", ""},
	{ActNextTab, []string{"gt"}, "next tab", ""},
	{ActPrevTab, []string{"gT"}, "previous tab", ""},
	{ActWindow, []string{"ctrl+w"}, "pane prefix: then s/v split, h/j/k/l/w focus, q/c close, o only", "panes"},
	{ActSplitHoriz, nil, "split the pane horizontally (stacked)", ""},
	{ActSplitVert, nil, "split the pane vertically (side by side)", ""},
	{ActFocusNext, nil, "focus the next pane", ""},
	{ActClosePane, nil, "close the pane", ""},
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
		keys = canonicalKeys(keys)
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

// namedKeys are multi-character key names that are a single key press.
var namedKeys = map[string]bool{
	"up": true, "down": true, "left": true, "right": true, "home": true, "end": true,
	"pgup": true, "pgdown": true, "enter": true, "esc": true, "tab": true, "backspace": true,
	"delete": true, "insert": true,
}

// canonicalKeys turns vim-style sequences ("gg", "gT") into space-separated
// key tokens ("g g"). Named keys ("pgdown", "ctrl+w", "f5") stay single tokens.
func canonicalKeys(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		r := []rune(k)
		switch {
		case k == "space":
			out = append(out, " ")
		case len(r) <= 1, strings.Contains(k, "+"), strings.Contains(k, " "), namedKeys[k], isFunctionKey(k):
			out = append(out, k)
		default:
			toks := make([]string, len(r))
			for i, c := range r {
				toks[i] = string(c)
			}
			out = append(out, strings.Join(toks, " "))
		}
	}
	return out
}

func isFunctionKey(k string) bool {
	return len(k) >= 2 && len(k) <= 3 && k[0] == 'f' && strings.Trim(k[1:], "0123456789") == ""
}

// isExact reports whether a key sequence ("g t") is bound to anything.
func (km keyMap) isExact(seq string) bool {
	for _, b := range km.b {
		if !b.Enabled() {
			continue
		}
		for _, k := range b.Keys() {
			if k == seq {
				return true
			}
		}
	}
	return false
}

// isPrefix reports whether seq starts a longer bound sequence.
func (km keyMap) isPrefix(seq string) bool {
	for _, b := range km.b {
		if !b.Enabled() {
			continue
		}
		for _, k := range b.Keys() {
			if strings.HasPrefix(k, seq+" ") {
				return true
			}
		}
	}
	return false
}

// seqKey is a resolved key sequence, matched like a key press.
type seqKey string

func (s seqKey) String() string { return string(s) }

func displayKeys(keys []string) string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if k != " " {
			k = strings.ReplaceAll(k, " ", "")
		}
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
