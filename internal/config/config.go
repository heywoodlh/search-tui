// Package config loads and resolves the TOML configuration for search-tui.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

// AppName is used for config/cache directory names.
const AppName = "search-tui"

// Config is the top-level configuration file structure.
type Config struct {
	DefaultEngine     string `toml:"default_engine"`
	DefaultCodeEngine string `toml:"default_code_engine"`
	FallbackEngine    string `toml:"fallback_engine"`
	DefaultType       string `toml:"default_type"`
	Results           int    `toml:"results"`
	Timeout           string `toml:"timeout"`
	LowPower          bool   `toml:"low_power"`
	UserAgent         string `toml:"user_agent"`
	MaxBodyKB         int    `toml:"max_body_kb"`
	Region            string `toml:"region"`
	SafeSearch        string `toml:"safe_search"`
	Browser           string `toml:"browser"`
	Editor            string `toml:"editor"`
	Clipboard         string `toml:"clipboard"`
	ClipboardCommand  string `toml:"clipboard_command"`

	Summary Summary             `toml:"summary"`
	UI      UI                  `toml:"ui"`
	Keys    map[string][]string `toml:"keys"`
	// Bangs maps a bang (without "!") to either an engine name or a URL
	// template containing {query}.
	Bangs   map[string]string  `toml:"bangs"`
	Engines map[string]*Engine `toml:"engines"`

	path string
}

// Summary controls on-device result summarisation.
type Summary struct {
	Enabled   *bool `toml:"enabled"`
	Sentences int   `toml:"sentences"`
	// Command is an optional external summariser (for example a local LLM
	// such as "ollama run llama3.2:1b"). The prompt is written to stdin.
	Command string `toml:"command"`
	// CommandTimeout bounds the external summariser.
	CommandTimeout string `toml:"command_timeout"`
}

// UI controls the terminal interface.
type UI struct {
	Mouse         *bool  `toml:"mouse"`
	Layout        string `toml:"layout"` // auto | split | stacked
	SplitMinWidth int    `toml:"split_min_width"`
	SnippetLines  int    `toml:"snippet_lines"`
	// HintChars are the characters used for f/F result hint labels.
	HintChars string `toml:"hint_chars"`
	StatusBar *bool  `toml:"status_bar"`
	// Theme is a built-in palette name (see `search --list-themes`).
	Theme string `toml:"theme"`
	// Colors overrides individual theme colours (hex or ANSI 0-255).
	Colors map[string]string `toml:"colors"`
	// AccentColor and MutedColor are shorthands for colors.accent / muted.
	AccentColor string `toml:"accent_color"`
	MutedColor  string `toml:"muted_color"`
	ASCII       bool   `toml:"ascii"`
}

// Engine is the configuration of a single (built-in or custom) engine.
type Engine struct {
	// Type selects the implementation. Built-in engines default to their own
	// name. Custom engines use one of: url, searxng, json, html, gitea,
	// gitlab, github, sourcegraph, ...
	Type     string   `toml:"type"`
	Disabled bool     `toml:"disabled"`
	Bangs    []string `toml:"bangs"`
	Name     string   `toml:"name"` // display name
	BaseURL  string   `toml:"base_url"`
	URL      string   `toml:"url"` // query URL template ({query}, {page}, {limit})
	Method   string   `toml:"method"`
	Body     string   `toml:"body"`
	Region   string   `toml:"region"`
	Language string   `toml:"language"`
	Kinds    []string `toml:"kinds"`

	// Credentials. Each may also be supplied via <name>_env, <name>_file and
	// <name>_command.
	APIKey          string `toml:"api_key"`
	APIKeyEnv       string `toml:"api_key_env"`
	APIKeyFile      string `toml:"api_key_file"`
	APIKeyCommand   string `toml:"api_key_command"`
	Token           string `toml:"token"`
	TokenEnv        string `toml:"token_env"`
	TokenFile       string `toml:"token_file"`
	TokenCommand    string `toml:"token_command"`
	Username        string `toml:"username"`
	Password        string `toml:"password"`
	PasswordEnv     string `toml:"password_env"`
	PasswordFile    string `toml:"password_file"`
	PasswordCommand string `toml:"password_command"`
	// CX is the Google Programmable Search engine ID.
	CX        string `toml:"cx"`
	CXEnv     string `toml:"cx_env"`
	CXFile    string `toml:"cx_file"`
	CXCommand string `toml:"cx_command"`

	Headers map[string]string `toml:"headers"`

	// JSON engines.
	ResultsPath  string `toml:"results_path"`
	TitleField   string `toml:"title_field"`
	URLField     string `toml:"url_field"`
	SnippetField string `toml:"snippet_field"`
	// HTML engines.
	ResultSelector  string `toml:"result_selector"`
	TitleSelector   string `toml:"title_selector"`
	LinkSelector    string `toml:"link_selector"`
	LinkAttr        string `toml:"link_attr"`
	SnippetSelector string `toml:"snippet_selector"`

	secrets sync.Map
	// key is the name this engine is registered under in Config.Engines; it
	// links the engine to credentials saved by :login.
	key string
}

// Default returns a configuration with all defaults applied.
func Default() *Config {
	c := &Config{}
	c.applyDefaults()
	return c
}

func boolp(b bool) *bool { return &b }

func (c *Config) applyDefaults() {
	if c.DefaultEngine == "" {
		c.DefaultEngine = "duckduckgo"
	}
	if c.FallbackEngine == "" {
		c.FallbackEngine = "bing"
	}
	if c.DefaultType == "" {
		c.DefaultType = "web"
	}
	if c.Results <= 0 {
		c.Results = 20
		if c.LowPower {
			c.Results = 10
		}
	}
	if c.Timeout == "" {
		c.Timeout = "15s"
	}
	if c.MaxBodyKB <= 0 {
		c.MaxBodyKB = 2048
		if c.LowPower {
			c.MaxBodyKB = 512
		}
	}
	if c.UserAgent == "" {
		c.UserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0"
	}
	if c.Clipboard == "" {
		c.Clipboard = "auto"
	}
	if c.Summary.Enabled == nil {
		c.Summary.Enabled = boolp(true)
	}
	if c.Summary.Sentences <= 0 {
		c.Summary.Sentences = 3
	}
	if c.Summary.CommandTimeout == "" {
		c.Summary.CommandTimeout = "60s"
	}
	if c.UI.Mouse == nil {
		c.UI.Mouse = boolp(true)
	}
	if c.UI.Layout == "" {
		c.UI.Layout = "auto"
	}
	if c.UI.SplitMinWidth <= 0 {
		c.UI.SplitMinWidth = 110
	}
	if c.UI.SnippetLines <= 0 {
		c.UI.SnippetLines = 2
	}
	if c.UI.StatusBar == nil {
		c.UI.StatusBar = boolp(true)
	}
	if c.Keys == nil {
		c.Keys = map[string][]string{}
	}
	if c.Bangs == nil {
		c.Bangs = map[string]string{}
	}
	if c.Engines == nil {
		c.Engines = map[string]*Engine{}
	}
}

// TimeoutDuration parses the configured request timeout.
func (c *Config) TimeoutDuration() time.Duration {
	d, err := time.ParseDuration(c.Timeout)
	if err != nil || d <= 0 {
		return 15 * time.Second
	}
	return d
}

// SummaryEnabled reports whether summaries are enabled.
func (c *Config) SummaryEnabled() bool { return c.Summary.Enabled != nil && *c.Summary.Enabled }

// Path returns the file the configuration was loaded from ("" if defaults).
func (c *Config) Path() string { return c.path }

// Dir returns the configuration directory. XDG_CONFIG_HOME is honoured on
// every platform so home-manager managed files work on macOS too.
func Dir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, AppName)
	}
	if runtime.GOOS == "windows" {
		if d, err := os.UserConfigDir(); err == nil {
			return filepath.Join(d, AppName)
		}
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".config", AppName)
	}
	return AppName
}

// DefaultPath returns the default configuration file path.
func DefaultPath() string {
	if p := os.Getenv("SEARCH_TUI_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(Dir(), "config.toml")
}

// Load reads the configuration at path. A missing file at the default path is
// not an error; defaults are returned instead.
func Load(path string) (*Config, error) {
	explicit := path != ""
	if !explicit {
		path = DefaultPath()
	}
	c := &Config{}
	md, err := toml.DecodeFile(path, c)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !explicit {
			return Default(), nil
		}
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if undec := md.Undecoded(); len(undec) > 0 {
		keys := make([]string, 0, len(undec))
		for _, k := range undec {
			keys = append(keys, k.String())
		}
		fmt.Fprintf(os.Stderr, "search-tui: warning: unknown config keys in %s: %s\n", path, strings.Join(keys, ", "))
	}
	c.path = path
	c.applyDefaults()
	if w := c.secretPermissionWarning(); w != "" {
		fmt.Fprintln(os.Stderr, "search-tui: warning:", w)
	}
	return c, nil
}

// secretPermissionWarning flags literal credentials in a config file that
// other users can read (for example a home-manager file in the Nix store).
func (c *Config) secretPermissionWarning() string {
	if runtime.GOOS == "windows" || c.path == "" {
		return ""
	}
	var names []string
	for name, e := range c.Engines {
		if e != nil && (e.APIKey != "" || e.Token != "" || e.Password != "" || e.CX != "") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	fi, err := os.Stat(c.path)
	if err != nil || fi.Mode().Perm()&0o077 == 0 {
		return ""
	}
	sort.Strings(names)
	return fmt.Sprintf("%s contains credentials for %s but is readable by other users (mode %o); run chmod 600 or use *_file / *_command / *_env instead",
		c.path, strings.Join(names, ", "), fi.Mode().Perm())
}

// Parse decodes configuration from a TOML string (used in tests).
func Parse(s string) (*Config, error) {
	c := &Config{}
	if _, err := toml.Decode(s, c); err != nil {
		return nil, err
	}
	c.applyDefaults()
	return c, nil
}

// EngineConfig returns the configuration for the named engine, never nil.
func (c *Config) EngineConfig(name string) *Engine {
	if e, ok := c.Engines[name]; ok && e != nil {
		e.key = name
		return e
	}
	e := &Engine{key: name}
	c.Engines[name] = e
	return e
}

// Secret resolves a credential from its literal value, environment variable,
// file, or command (in that order). Results are cached per engine.
func (e *Engine) Secret(ctx context.Context, name string) (string, error) {
	if e == nil {
		return "", nil
	}
	if v, ok := e.secrets.Load(name); ok {
		return v.(string), nil
	}
	var lit, env, file, cmd string
	switch name {
	case "api_key":
		lit, env, file, cmd = e.APIKey, e.APIKeyEnv, e.APIKeyFile, e.APIKeyCommand
	case "token":
		lit, env, file, cmd = e.Token, e.TokenEnv, e.TokenFile, e.TokenCommand
	case "password":
		lit, env, file, cmd = e.Password, e.PasswordEnv, e.PasswordFile, e.PasswordCommand
	case "cx":
		lit, env, file, cmd = e.CX, e.CXEnv, e.CXFile, e.CXCommand
	default:
		return "", fmt.Errorf("unknown credential %q", name)
	}
	v, err := resolveSecret(ctx, lit, env, file, cmd)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	if v == "" {
		// Credentials saved by :login are the last resort, so an explicit
		// config value always wins over the central auth file.
		v = Credential(e.key, name)
	}
	e.secrets.Store(name, v)
	return v, nil
}

// ClearSecrets drops the cached credentials so the next search re-resolves
// them (used after :login updates the auth file).
func (e *Engine) ClearSecrets() {
	if e == nil {
		return
	}
	e.secrets = sync.Map{}
}

// AuthMethod reports how this engine's credential was obtained (a Session Link
// token, an API key, ...), so providers can skip probing the wrong endpoint.
func (e *Engine) AuthMethod() string {
	if e == nil {
		return ""
	}
	return CredentialMethod(e.key)
}

func resolveSecret(ctx context.Context, lit, env, file, cmd string) (string, error) {
	if lit != "" {
		return lit, nil
	}
	if env != "" {
		if v := os.Getenv(env); v != "" {
			return v, nil
		}
	}
	if file != "" {
		b, err := os.ReadFile(ExpandHome(file))
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	if cmd != "" {
		return RunCommand(ctx, cmd)
	}
	return "", nil
}

// RunCommand runs a shell command and returns its trimmed stdout.
func RunCommand(ctx context.Context, command string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// command is a *_command credential helper from the user's own config
	// (e.g. "pass show ..."), so running it through the shell is intended.
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/C", command) //nolint:gosec // G204: user-configured credential command
	} else {
		c = exec.CommandContext(ctx, "sh", "-c", command) //nolint:gosec // G204: user-configured credential command
	}
	c.Stdin = nil
	out, err := c.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("command %q: %w: %s", command, err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("command %q: %w", command, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ExpandHome expands a leading "~/".
func ExpandHome(p string) string {
	if strings.HasPrefix(p, "~/") || p == "~" {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// HasSecret reports whether a credential source is configured, without
// resolving it (so password managers are not invoked just to check).
func (e *Engine) HasSecret(name string) bool {
	if e == nil {
		return false
	}
	var lit, env, file, cmd string
	switch name {
	case "api_key":
		lit, env, file, cmd = e.APIKey, e.APIKeyEnv, e.APIKeyFile, e.APIKeyCommand
	case "token":
		lit, env, file, cmd = e.Token, e.TokenEnv, e.TokenFile, e.TokenCommand
	case "password":
		lit, env, file, cmd = e.Password, e.PasswordEnv, e.PasswordFile, e.PasswordCommand
	case "cx":
		lit, env, file, cmd = e.CX, e.CXEnv, e.CXFile, e.CXCommand
	}
	return lit != "" || (env != "" && os.Getenv(env) != "") || file != "" || cmd != "" || Credential(e.key, name) != ""
}
