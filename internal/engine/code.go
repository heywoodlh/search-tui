package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/heywoodlh-bot/tui-search/internal/config"
)

func codeQuery(q Query, langKey, repoKey string) string {
	s := q.Text
	if q.Lang != "" {
		s += " " + langKey + ":" + q.Lang
	}
	if q.Repo != "" {
		s += " " + repoKey + ":" + q.Repo
	}
	return s
}

func firstLine(s string, n int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// -------------------------------------------------------------------- GitHub

// GitHub searches code, repositories and issues via the REST API.
type GitHub struct {
	base
	once     sync.Once
	tokenVal string
}

func (g *GitHub) apiURL() string {
	if g.cfg.BaseURL != "" {
		return strings.TrimRight(g.cfg.BaseURL, "/")
	}
	return "https://api.github.com"
}

func (g *GitHub) webURL() string {
	if g.cfg.BaseURL == "" {
		return "https://github.com"
	}
	return strings.TrimSuffix(strings.TrimRight(g.cfg.BaseURL, "/"), "/api/v3")
}

// token resolves a token from config, GITHUB_TOKEN/GH_TOKEN, or `gh auth token`.
func (g *GitHub) token(ctx context.Context) string {
	g.once.Do(func() {
		if t, err := g.cfg.Secret(ctx, "token"); err == nil && t != "" {
			g.tokenVal = t
			return
		}
		for _, env := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
			if t := os.Getenv(env); t != "" {
				g.tokenVal = t
				return
			}
		}
		if g.cfg.BaseURL == "" {
			if _, err := exec.LookPath("gh"); err == nil {
				if t, err := config.RunCommand(ctx, "gh auth token"); err == nil {
					g.tokenVal = t
				}
			}
		}
	})
	return g.tokenVal
}

func (g *GitHub) hasToken(ctx context.Context) bool { return g.token(ctx) != "" }

func (g *GitHub) SearchURL(q Query) string {
	t := map[Kind]string{KindCode: "code", KindRepos: "repositories", KindIssues: "issues"}[q.Kind]
	if t == "" {
		t = "repositories"
	}
	return g.webURL() + "/search?type=" + t + "&q=" + url.QueryEscape(codeQuery(q, "language", "repo"))
}

func (g *GitHub) authHeaders(ctx context.Context, accept string) map[string]string {
	h := g.headers(map[string]string{"Accept": accept, "X-GitHub-Api-Version": "2022-11-28"})
	if t := g.token(ctx); t != "" {
		h["Authorization"] = "Bearer " + t
	}
	return h
}

func (g *GitHub) Search(ctx context.Context, q Query) ([]Result, error) {
	n := limitOf(q, 20)
	if n > 100 {
		n = 100
	}
	v := url.Values{"q": {codeQuery(q, "language", "repo")}, "per_page": {fmt.Sprint(n)}, "page": {fmt.Sprint(pageOf(q))}}
	switch q.Kind {
	case KindCode:
		if !g.hasToken(ctx) {
			return nil, &AuthError{Engine: "github", Hint: "code search needs a token: set [engines.github] token / token_command, GITHUB_TOKEN, or log in with `gh auth login`"}
		}
		return g.code(ctx, v)
	case KindIssues:
		return g.issues(ctx, v)
	default:
		return g.repos(ctx, v)
	}
}

func (g *GitHub) code(ctx context.Context, v url.Values) ([]Result, error) {
	var resp struct {
		Items []struct {
			Name       string `json:"name"`
			Path       string `json:"path"`
			SHA        string `json:"sha"`
			URL        string `json:"url"`
			HTMLURL    string `json:"html_url"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			TextMatches []struct {
				Fragment string `json:"fragment"`
				Property string `json:"property"`
			} `json:"text_matches"`
		} `json:"items"`
	}
	err := g.client.JSON(ctx, Request{URL: g.apiURL() + "/search/code?" + v.Encode(), Headers: g.authHeaders(ctx, "application/vnd.github.text-match+json")}, &resp)
	if err != nil {
		return nil, err
	}
	rawHeaders := g.authHeaders(ctx, "application/vnd.github.raw")
	var rs []Result
	for _, it := range resp.Items {
		cm := &CodeMatch{Repo: it.Repository.FullName, Path: it.Path, Language: langFromPath(it.Path), RawURL: it.URL, RawHeaders: rawHeaders}
		if i := strings.Index(it.HTMLURL, "/blob/"); i >= 0 {
			rest := it.HTMLURL[i+len("/blob/"):]
			if j := strings.IndexByte(rest, '/'); j >= 0 {
				cm.Ref = rest[:j]
			}
		}
		for _, tm := range it.TextMatches {
			if tm.Property == "content" {
				cm.Fragments = append(cm.Fragments, Fragment{Text: strings.Trim(tm.Fragment, "\n")})
			}
		}
		snip := ""
		if len(cm.Fragments) > 0 {
			snip = firstLine(cm.Fragments[0].Text, 160)
		}
		rs = append(rs, Result{Title: it.Repository.FullName + "/" + it.Path, URL: it.HTMLURL, Snippet: snip, Engine: g.name, Code: cm})
	}
	return rs, nil
}

func (g *GitHub) repos(ctx context.Context, v url.Values) ([]Result, error) {
	var resp struct {
		Items []struct {
			FullName    string `json:"full_name"`
			HTMLURL     string `json:"html_url"`
			Description string `json:"description"`
			Stars       int    `json:"stargazers_count"`
			Language    string `json:"language"`
			UpdatedAt   string `json:"updated_at"`
			Archived    bool   `json:"archived"`
		} `json:"items"`
	}
	if err := g.client.JSON(ctx, Request{URL: g.apiURL() + "/search/repositories?" + v.Encode(), Headers: g.authHeaders(ctx, "application/vnd.github+json")}, &resp); err != nil {
		return nil, err
	}
	var rs []Result
	for _, it := range resp.Items {
		meta := fmt.Sprintf("★ %d", it.Stars)
		if it.Language != "" {
			meta += " · " + it.Language
		}
		if len(it.UpdatedAt) >= 10 {
			meta += " · updated " + it.UpdatedAt[:10]
		}
		if it.Archived {
			meta += " · archived"
		}
		rs = append(rs, Result{Title: it.FullName, URL: it.HTMLURL, Snippet: it.Description, Meta: meta, Engine: g.name})
	}
	return rs, nil
}

func (g *GitHub) issues(ctx context.Context, v url.Values) ([]Result, error) {
	var resp struct {
		Items []struct {
			Title       string    `json:"title"`
			HTMLURL     string    `json:"html_url"`
			Body        string    `json:"body"`
			State       string    `json:"state"`
			Comments    int       `json:"comments"`
			PullRequest *struct{} `json:"pull_request"`
			User        struct {
				Login string `json:"login"`
			} `json:"user"`
		} `json:"items"`
	}
	if err := g.client.JSON(ctx, Request{URL: g.apiURL() + "/search/issues?" + v.Encode(), Headers: g.authHeaders(ctx, "application/vnd.github+json")}, &resp); err != nil {
		return nil, err
	}
	var rs []Result
	for _, it := range resp.Items {
		kind := "issue"
		if it.PullRequest != nil {
			kind = "PR"
		}
		repo := ""
		if parts := strings.Split(it.HTMLURL, "/"); len(parts) > 4 {
			repo = parts[3] + "/" + parts[4] + " · "
		}
		rs = append(rs, Result{
			Title:   it.Title,
			URL:     it.HTMLURL,
			Snippet: trunc(CleanText(it.Body), 400),
			Meta:    fmt.Sprintf("%s%s %s · %d comments · @%s", repo, it.State, kind, it.Comments, it.User.Login),
			Engine:  g.name,
		})
	}
	return rs, nil
}

// -------------------------------------------------------------------- GitLab

// GitLab searches projects, code (blobs) and issues via the REST API.
type GitLab struct {
	base
	mu       sync.Mutex
	projects map[int]string
}

func (g *GitLab) baseURL() string {
	if g.cfg.BaseURL != "" {
		return strings.TrimRight(g.cfg.BaseURL, "/")
	}
	return "https://gitlab.com"
}

func (g *GitLab) SearchURL(q Query) string {
	scope := map[Kind]string{KindCode: "blobs", KindIssues: "issues"}[q.Kind]
	if scope == "" {
		scope = "projects"
	}
	return g.baseURL() + "/search?scope=" + scope + "&search=" + url.QueryEscape(q.Text)
}

func (g *GitLab) authHeaders(ctx context.Context) (map[string]string, bool) {
	h := g.headers(nil)
	t, _ := g.cfg.Secret(ctx, "token")
	if t == "" {
		t = os.Getenv("GITLAB_TOKEN")
	}
	if t != "" {
		h["PRIVATE-TOKEN"] = t
	}
	return h, t != ""
}

func (g *GitLab) Search(ctx context.Context, q Query) ([]Result, error) {
	h, authed := g.authHeaders(ctx)
	n := limitOf(q, 20)
	v := url.Values{"per_page": {fmt.Sprint(n)}, "page": {fmt.Sprint(pageOf(q))}}
	switch q.Kind {
	case KindCode, KindIssues:
		if !authed {
			return nil, &AuthError{Engine: g.name, Hint: "code/issue search needs [engines." + g.name + "] token (or GITLAB_TOKEN)"}
		}
	}
	switch q.Kind {
	case KindCode:
		s := q.Text
		if q.Lang != "" {
			s += " extension:" + q.Lang
		}
		v.Set("scope", "blobs")
		v.Set("search", s)
		var items []struct {
			Basename  string `json:"basename"`
			Data      string `json:"data"`
			Path      string `json:"path"`
			Ref       string `json:"ref"`
			Startline int    `json:"startline"`
			ProjectID int    `json:"project_id"`
		}
		endpoint := "/api/v4/search?"
		if q.Repo != "" {
			endpoint = "/api/v4/projects/" + url.PathEscape(q.Repo) + "/search?"
		}
		if err := g.client.JSON(ctx, Request{URL: g.baseURL() + endpoint + v.Encode(), Headers: h}, &items); err != nil {
			return nil, err
		}
		var rs []Result
		for _, it := range items {
			proj := g.projectPath(ctx, it.ProjectID, h)
			web := fmt.Sprintf("%s/%s/-/blob/%s/%s#L%d", g.baseURL(), proj, it.Ref, it.Path, it.Startline)
			raw := fmt.Sprintf("%s/api/v4/projects/%d/repository/files/%s/raw?ref=%s", g.baseURL(), it.ProjectID, url.PathEscape(it.Path), url.QueryEscape(it.Ref))
			cm := &CodeMatch{
				Repo: proj, Path: it.Path, Ref: it.Ref, Line: it.Startline, Language: langFromPath(it.Path),
				Fragments: []Fragment{{Line: it.Startline, Text: strings.Trim(it.Data, "\n")}},
				RawURL:    raw, RawHeaders: h,
			}
			rs = append(rs, Result{Title: proj + "/" + it.Path, URL: web, Snippet: firstLine(it.Data, 160), Engine: g.name, Code: cm})
		}
		return rs, nil
	case KindIssues:
		v.Set("scope", "issues")
		v.Set("search", q.Text)
		var items []struct {
			Title       string `json:"title"`
			WebURL      string `json:"web_url"`
			Description string `json:"description"`
			State       string `json:"state"`
		}
		if err := g.client.JSON(ctx, Request{URL: g.baseURL() + "/api/v4/search?" + v.Encode(), Headers: h}, &items); err != nil {
			return nil, err
		}
		var rs []Result
		for _, it := range items {
			rs = append(rs, Result{Title: it.Title, URL: it.WebURL, Snippet: trunc(CleanText(it.Description), 400), Meta: it.State, Engine: g.name})
		}
		return rs, nil
	default:
		v.Set("search", q.Text)
		v.Set("order_by", "star_count")
		var items []struct {
			PathWithNamespace string `json:"path_with_namespace"`
			WebURL            string `json:"web_url"`
			Description       string `json:"description"`
			StarCount         int    `json:"star_count"`
			LastActivityAt    string `json:"last_activity_at"`
		}
		if err := g.client.JSON(ctx, Request{URL: g.baseURL() + "/api/v4/projects?" + v.Encode(), Headers: h}, &items); err != nil {
			return nil, err
		}
		var rs []Result
		for _, it := range items {
			meta := fmt.Sprintf("★ %d", it.StarCount)
			if len(it.LastActivityAt) >= 10 {
				meta += " · active " + it.LastActivityAt[:10]
			}
			rs = append(rs, Result{Title: it.PathWithNamespace, URL: it.WebURL, Snippet: it.Description, Meta: meta, Engine: g.name})
		}
		return rs, nil
	}
}

func (g *GitLab) projectPath(ctx context.Context, id int, h map[string]string) string {
	g.mu.Lock()
	if g.projects == nil {
		g.projects = map[int]string{}
	}
	p, ok := g.projects[id]
	g.mu.Unlock()
	if ok {
		return p
	}
	var proj struct {
		PathWithNamespace string `json:"path_with_namespace"`
	}
	p = fmt.Sprintf("projects/%d", id)
	if err := g.client.JSON(ctx, Request{URL: fmt.Sprintf("%s/api/v4/projects/%d", g.baseURL(), id), Headers: h}, &proj); err == nil && proj.PathWithNamespace != "" {
		p = proj.PathWithNamespace
	}
	g.mu.Lock()
	g.projects[id] = p
	g.mu.Unlock()
	return p
}

// --------------------------------------------------------------- Sourcegraph

// Sourcegraph searches public (or private, with a token) code via the
// streaming search API. sourcegraph.com works without credentials.
type Sourcegraph struct{ base }

func (s *Sourcegraph) baseURL() string {
	if s.cfg.BaseURL != "" {
		return strings.TrimRight(s.cfg.BaseURL, "/")
	}
	return "https://sourcegraph.com"
}

func (s *Sourcegraph) query(q Query) string {
	t := codeQuery(q, "lang", "repo")
	if q.Kind == KindRepos {
		t += " type:repo"
	}
	return t
}

func (s *Sourcegraph) SearchURL(q Query) string {
	return s.baseURL() + "/search?q=" + url.QueryEscape(s.query(q))
}

func (s *Sourcegraph) Search(ctx context.Context, q Query) ([]Result, error) {
	n := limitOf(q, 20)
	// The stream API has no offsets; fetch enough to cover the page.
	want := n * pageOf(q)
	qs := s.query(q)
	if !strings.Contains(qs, "timeout:") {
		qs += " timeout:10s"
	}
	v := url.Values{"q": {fmt.Sprintf("%s count:%d", qs, want)}, "v": {"V3"}, "display": {fmt.Sprint(want)}}
	h := s.headers(map[string]string{"Accept": "text/event-stream"})
	if t, _ := s.cfg.Secret(ctx, "token"); t != "" {
		h["Authorization"] = "token " + t
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL()+"/.api/search/stream?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.client.UserAgent)
	for k, val := range h {
		req.Header.Set(k, val)
	}
	// Streams can be long-lived; rely on ctx rather than the client timeout.
	hc := *s.client.HTTP
	hc.Timeout = 0
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, &HTTPError{Status: resp.StatusCode, URL: req.URL.String(), Body: strings.TrimSpace(string(b))}
	}
	var rs []Result
	var alert string
	sc := bufio.NewScanner(io.LimitReader(resp.Body, s.client.MaxBody*4))
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	event := ""
	// Return partial results rather than waiting for slow, exhaustive searches.
	soft := time.Now().Add(6 * time.Second)
	for sc.Scan() && len(rs) < want {
		if len(rs) > 0 && time.Now().After(soft) {
			break
		}
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data := []byte(strings.TrimPrefix(line, "data: "))
			switch event {
			case "matches":
				rs = append(rs, s.parseMatches(data)...)
			case "alert":
				var a struct {
					Title       string `json:"title"`
					Description string `json:"description"`
				}
				if json.Unmarshal(data, &a) == nil {
					alert = a.Title + ": " + a.Description
				}
			case "error":
				var e struct {
					Message string `json:"message"`
				}
				_ = json.Unmarshal(data, &e)
				return nil, fmt.Errorf("sourcegraph: %s", e.Message)
			case "done":
				goto done
			}
		}
	}
done:
	if len(rs) == 0 {
		if alert != "" {
			if strings.Contains(alert, "Timed out") {
				alert += " Narrow it with --lang/--repo (lang:go, repo:owner/name)."
			}
			return nil, fmt.Errorf("sourcegraph: %s", alert)
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("sourcegraph timed out; narrow the query with --lang or --repo (lang:go, repo:owner/name)")
		}
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("sourcegraph: %w", err)
		}
	}
	start := (pageOf(q) - 1) * n
	if start >= len(rs) {
		return nil, nil
	}
	rs = rs[start:]
	return trimResults(rs, n), nil
}

func (s *Sourcegraph) parseMatches(data []byte) []Result {
	var ms []struct {
		Type        string `json:"type"`
		Repository  string `json:"repository"`
		Path        string `json:"path"`
		Commit      string `json:"commit"`
		Language    string `json:"language"`
		RepoStars   int    `json:"repoStars"`
		Description string `json:"description"`
		LineMatches []struct {
			Line       string `json:"line"`
			LineNumber int    `json:"lineNumber"`
		} `json:"lineMatches"`
		ChunkMatches []struct {
			Content      string `json:"content"`
			ContentStart struct {
				Line int `json:"line"`
			} `json:"contentStart"`
		} `json:"chunkMatches"`
	}
	if err := json.Unmarshal(data, &ms); err != nil {
		return nil
	}
	var rs []Result
	for _, m := range ms {
		switch m.Type {
		case "repo":
			rs = append(rs, Result{
				Title: m.Repository, URL: s.baseURL() + "/" + m.Repository, Snippet: m.Description,
				Meta: fmt.Sprintf("★ %d", m.RepoStars), Engine: s.name,
			})
		case "content", "path", "symbol":
			ref := m.Commit
			cm := &CodeMatch{Repo: m.Repository, Path: m.Path, Ref: ref, Language: m.Language}
			for _, lm := range m.LineMatches {
				cm.Fragments = append(cm.Fragments, Fragment{Line: lm.LineNumber + 1, Text: lm.Line})
			}
			for _, c := range m.ChunkMatches {
				cm.Fragments = append(cm.Fragments, Fragment{Line: c.ContentStart.Line + 1, Text: strings.TrimRight(c.Content, "\n")})
			}
			cm.Fragments = mergeFragments(cm.Fragments)
			at := ""
			if ref != "" {
				at = "@" + ref
			}
			cm.RawURL = fmt.Sprintf("%s/%s%s/-/raw/%s", s.baseURL(), m.Repository, at, m.Path)
			web := fmt.Sprintf("%s/%s%s/-/blob/%s", s.baseURL(), m.Repository, at, m.Path)
			snip := ""
			if len(cm.Fragments) > 0 {
				cm.Line = cm.Fragments[0].Line
				web += fmt.Sprintf("?L%d", cm.Line)
				snip = firstLine(cm.Fragments[0].Text, 160)
			}
			rs = append(rs, Result{Title: m.Repository + "/" + m.Path, URL: web, Snippet: snip, Meta: fmt.Sprintf("★ %d", m.RepoStars), Engine: s.name, Code: cm})
		}
	}
	return rs
}

// mergeFragments joins consecutive single-line fragments into blocks.
func mergeFragments(fs []Fragment) []Fragment {
	if len(fs) < 2 {
		return fs
	}
	out := []Fragment{fs[0]}
	lastEnd := fs[0].Line + strings.Count(fs[0].Text, "\n")
	for _, f := range fs[1:] {
		cur := &out[len(out)-1]
		if f.Line > 0 && f.Line == lastEnd+1 {
			cur.Text += "\n" + f.Text
		} else {
			out = append(out, f)
		}
		lastEnd = f.Line + strings.Count(f.Text, "\n")
	}
	return out
}

// --------------------------------------------------------- Gitea / Forgejo

// Gitea searches repositories on Gitea/Forgejo instances (Codeberg by
// default). These forges have no global code search API.
type Gitea struct{ base }

func (g *Gitea) baseURL() string {
	if g.cfg.BaseURL != "" {
		return strings.TrimRight(g.cfg.BaseURL, "/")
	}
	return "https://codeberg.org"
}

func (g *Gitea) SearchURL(q Query) string {
	return g.baseURL() + "/explore/repos?q=" + url.QueryEscape(q.Text)
}

func (g *Gitea) Search(ctx context.Context, q Query) ([]Result, error) {
	h := g.headers(nil)
	if t, _ := g.cfg.Secret(ctx, "token"); t != "" {
		h["Authorization"] = "token " + t
	}
	v := url.Values{"q": {q.Text}, "limit": {fmt.Sprint(limitOf(q, 20))}, "page": {fmt.Sprint(pageOf(q))}, "sort": {"stars"}, "order": {"desc"}}
	var resp struct {
		Data []struct {
			FullName    string `json:"full_name"`
			HTMLURL     string `json:"html_url"`
			Description string `json:"description"`
			Stars       int    `json:"stars_count"`
			Language    string `json:"language"`
			UpdatedAt   string `json:"updated_at"`
		} `json:"data"`
	}
	if err := g.client.JSON(ctx, Request{URL: g.baseURL() + "/api/v1/repos/search?" + v.Encode(), Headers: h}, &resp); err != nil {
		return nil, err
	}
	var rs []Result
	for _, it := range resp.Data {
		meta := fmt.Sprintf("★ %d", it.Stars)
		if it.Language != "" {
			meta += " · " + it.Language
		}
		rs = append(rs, Result{Title: it.FullName, URL: it.HTMLURL, Snippet: it.Description, Meta: meta, Engine: g.name})
	}
	return rs, nil
}

// FetchRaw downloads the full file for a code match.
func (c *Client) FetchRaw(ctx context.Context, cm *CodeMatch) (string, error) {
	if cm == nil || cm.RawURL == "" {
		return "", fmt.Errorf("no raw file URL for this result")
	}
	b, err := c.Get(ctx, cm.RawURL, cm.RawHeaders)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

var extLang = map[string]string{
	".go": "go", ".rs": "rust", ".py": "python", ".js": "javascript", ".ts": "typescript",
	".tsx": "tsx", ".jsx": "jsx", ".rb": "ruby", ".java": "java", ".kt": "kotlin", ".c": "c",
	".h": "c", ".cc": "cpp", ".cpp": "cpp", ".hpp": "cpp", ".cs": "csharp", ".sh": "bash",
	".bash": "bash", ".zsh": "zsh", ".fish": "fish", ".nix": "nix", ".lua": "lua", ".php": "php",
	".swift": "swift", ".scala": "scala", ".hs": "haskell", ".ml": "ocaml", ".ex": "elixir",
	".exs": "elixir", ".erl": "erlang", ".clj": "clojure", ".el": "elisp", ".vim": "vim",
	".yaml": "yaml", ".yml": "yaml", ".toml": "toml", ".json": "json", ".md": "markdown",
	".html": "html", ".css": "css", ".scss": "scss", ".sql": "sql", ".tf": "hcl", ".hcl": "hcl",
	".zig": "zig", ".dart": "dart", ".r": "r", ".pl": "perl", ".ps1": "powershell", ".xml": "xml",
	".dockerfile": "dockerfile", ".proto": "protobuf", ".graphql": "graphql",
}

func langFromPath(p string) string {
	base := strings.ToLower(path.Base(p))
	switch base {
	case "dockerfile":
		return "dockerfile"
	case "makefile":
		return "make"
	}
	return extLang[path.Ext(base)]
}

// LangFromPath exposes language detection for other packages.
func LangFromPath(p string) string { return langFromPath(p) }

// Focus returns a display-friendly window of a fragment: common indentation
// is removed, and the window starts just before the first line mentioning a
// query term so list previews show the actual match.
func Focus(f Fragment, query string, maxLines int) Fragment {
	lines := strings.Split(strings.ReplaceAll(f.Text, "\t", "    "), "\n")
	start := 0
	terms := strings.Fields(strings.ToLower(query))
	for i, l := range lines {
		ll := strings.ToLower(l)
		hit := false
		for _, t := range terms {
			if strings.Contains(t, ":") {
				continue // qualifiers such as lang:go
			}
			if strings.Contains(ll, t) {
				hit = true
				break
			}
		}
		if hit {
			start = max(i-1, 0)
			break
		}
	}
	for start < len(lines)-1 && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	lines = lines[start:]
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	indent := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " "))
		if indent < 0 || n < indent {
			indent = n
		}
	}
	for i, l := range lines {
		if len(l) >= indent && indent > 0 {
			lines[i] = l[indent:]
		}
	}
	out := Fragment{Text: strings.Join(lines, "\n")}
	if f.Line > 0 {
		out.Line = f.Line + start
	}
	return out
}
