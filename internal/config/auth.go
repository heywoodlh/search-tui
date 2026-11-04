package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// AuthVersion is the schema version written to auth.json.
const AuthVersion = 1

// AuthEntry is one provider's saved credential. Values is keyed by credential
// name (api_key, token, cx) because a provider may need more than one (Google
// needs an API key and a Programmable Search engine ID).
type AuthEntry struct {
	Method  string            `json:"method"` // api_key, oauth, token
	Values  map[string]string `json:"values,omitempty"`
	Account string            `json:"account,omitempty"` // e.g. the GitHub login
	Scopes  []string          `json:"scopes,omitempty"`
	Updated time.Time         `json:"updated,omitempty"`
}

// AuthFile is the on-disk credential store written by :login.
type AuthFile struct {
	Version   int                  `json:"version"`
	Providers map[string]AuthEntry `json:"providers"`
}

// AuthPath returns the credential file path. It honours $SEARCH_TUI_AUTH and
// otherwise sits next to config.toml.
func AuthPath() string {
	if p := os.Getenv("SEARCH_TUI_AUTH"); p != "" {
		return ExpandHome(p)
	}
	return filepath.Join(Dir(), "auth.json")
}

// LoadAuth reads the credential store, returning an empty one when the file
// does not exist yet.
func LoadAuth() (*AuthFile, error) {
	a := &AuthFile{Version: AuthVersion, Providers: map[string]AuthEntry{}}
	b, err := os.ReadFile(AuthPath())
	if errors.Is(err, fs.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return a, err
	}
	if err := json.Unmarshal(b, a); err != nil {
		return a, fmt.Errorf("%s: %w", AuthPath(), err)
	}
	if a.Providers == nil {
		a.Providers = map[string]AuthEntry{}
	}
	return a, nil
}

// Save writes the store atomically with 0600 permissions: it holds credentials.
func (a *AuthFile) Save() error {
	if a.Providers == nil {
		a.Providers = map[string]AuthEntry{}
	}
	a.Version = AuthVersion
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	path := AuthPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".auth-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Get returns the entry saved for a provider.
func (a *AuthFile) Get(provider string) (AuthEntry, bool) {
	e, ok := a.Providers[provider]
	return e, ok
}

// Set stores an entry and writes the file.
func (a *AuthFile) Set(provider string, e AuthEntry) error {
	if a.Providers == nil {
		a.Providers = map[string]AuthEntry{}
	}
	e.Updated = time.Now().UTC()
	a.Providers[provider] = e
	return a.Save()
}

// CredentialMethod returns how a provider's credential was obtained (api_key,
// session, oauth, token), or "" when nothing was saved by :login.
func CredentialMethod(provider string) string {
	if provider == "" {
		return ""
	}
	a, err := LoadAuth()
	if err != nil {
		return ""
	}
	e, ok := a.Get(provider)
	if !ok {
		return ""
	}
	return e.Method
}

// Credential returns the saved value for a credential kind (api_key, token,
// cx) of a provider, or "" when nothing is stored. Read errors are ignored:
// a broken auth file must not stop a search that has other credentials.
func Credential(provider, kind string) string {
	if provider == "" {
		return ""
	}
	a, err := LoadAuth()
	if err != nil {
		return ""
	}
	e, ok := a.Get(provider)
	if !ok {
		return ""
	}
	return e.Values[kind]
}
