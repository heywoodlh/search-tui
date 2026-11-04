package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heywoodlh/tui-search/internal/engine"
)

func TestSelectedField(t *testing.T) {
	rs := []engine.Result{
		{Title: "First", URL: "https://example.com/1"},
		{Title: "Second", URL: "https://example.com/2", Code: &engine.CodeMatch{Fragments: []engine.Fragment{{Text: "func main() {}"}}}},
	}
	for _, n := range []int{0, 3} {
		if _, err := selectResult(rs, n); err == nil {
			t.Errorf("result %d should be out of range", n)
		}
	}
	r, err := selectResult(rs, 2)
	if err != nil || r.Title != "Second" {
		t.Fatalf("selection: %+v, %v", r, err)
	}
	for field, want := range map[string]string{"url": "https://example.com/2\n", "title": "Second\n", "code": "func main() {}\n"} {
		var b bytes.Buffer
		if err := printField(&b, r, field); err != nil || b.String() != want {
			t.Errorf("%s: %q, %v", field, b.String(), err)
		}
	}
	if err := printField(&bytes.Buffer{}, rs[0], "code"); err == nil {
		t.Error("non-code result should reject code field")
	}
}

func TestInlineSelectedResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"First","link":"https://example.com/one","description":"alpha"},{"name":"Second","link":"https://example.com/two","description":"beta"}]`))
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := fmt.Sprintf("default_engine = \"local\"\n[engines.local]\ntype = \"json\"\nurl = %q\ntitle_field = \"name\"\nurl_field = \"link\"\nsnippet_field = \"description\"\n", srv.URL+"/?q={query}")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if err := run([]string{"--config", path, "--result", "2", "--field", "url", "test"}, &out, &stderr); err != nil || out.String() != "https://example.com/two\n" {
		t.Fatalf("field: %q, %v (stderr: %s)", out.String(), err, stderr.String())
	}
	out.Reset()
	if err := run([]string{"--config", path, "--result", "2", "--json", "test"}, &out, &stderr); err != nil || !strings.Contains(out.String(), `"title": "Second"`) || strings.Contains(out.String(), `"title": "First"`) {
		t.Fatalf("filtered JSON: %q, %v", out.String(), err)
	}
	out.Reset()
	if err := run([]string{"--config", path, "--result", "3", "--field", "url", "test"}, &out, &stderr); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("out-of-range result: %v", err)
	}
}

func TestInvalidFieldBeforeNetwork(t *testing.T) {
	var out, stderr bytes.Buffer
	if err := run([]string{"--field", "bogus", "something"}, &out, &stderr); err == nil || !strings.Contains(err.Error(), "unknown --field") {
		t.Errorf("expected field validation: %v", err)
	}
	if err := run([]string{"--result", "-1", "something"}, &out, &stderr); err == nil {
		t.Error("negative result index should fail before search")
	}
}
