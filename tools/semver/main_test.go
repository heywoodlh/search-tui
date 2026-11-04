package main

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for _, ok := range []string{"0.0.0", "0.1.0", "1.2.3", "10.20.30", "1.0.0-rc.1", "1.0.0-alpha.beta+exp.sha.5114f85", "1.0.0+20130313144700"} {
		if _, err := Parse(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "1", "1.2", "v1.2.3", "01.2.3", "1.02.3", "1.2.3-01", "1.2.3-", "1.2.3+", "1.2.3.4", "1.2.3 beta"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestRequired(t *testing.T) {
	cases := map[string]Bump{
		"feat: add panes":                Minor,
		"feat(tui): add panes":           Minor,
		"fix(engine): handle 429":        Patch,
		"perf: faster summaries":         Patch,
		"revert: undo x":                 Patch,
		"docs: typo":                     None,
		"chore(deps): bump golang.org/x": None,
		"ci: pin actions":                None,
		"refactor!: drop old config":     Major,
		"feat(cli)!: rename flags":       Major,
	}
	for title, want := range cases {
		got, err := Required(title, "")
		if err != nil || got != want {
			t.Errorf("Required(%q) = %v, %v; want %v", title, got, err, want)
		}
	}
	if b, _ := Required("feat: x", "text\n\nBREAKING CHANGE: config keys renamed"); b != Major {
		t.Error("BREAKING CHANGE footer should require a major bump")
	}
	for _, bad := range []string{"Add search", "feat:missing space", "feat: ", "Feat: capital", "wip: stuff", "feat(Scope): x", "feat(): x"} {
		if _, err := Required(bad, ""); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestCheck(t *testing.T) {
	v := func(s string) Version {
		x, err := Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	good := []struct {
		b          Bump
		base, head string
	}{
		{Minor, "0.0.0", "0.1.0"},
		{Minor, "1.4.2", "1.5.0"},
		{Patch, "1.4.2", "1.4.3"},
		{Major, "1.4.2", "2.0.0"},
		{Major, "0.4.2", "0.5.0"}, // breaking while 0.y.z
		{Major, "0.4.2", "1.0.0"}, // or graduate to 1.0.0
		{None, "1.4.2", "1.4.2"},
		{Minor, "1.4.2", "1.5.0-rc.1"},
	}
	for _, c := range good {
		if err := Check(c.b, v(c.base), v(c.head)); err != nil {
			t.Errorf("%v %s->%s: %v", c.b, c.base, c.head, err)
		}
	}
	bad := []struct {
		b          Bump
		base, head string
	}{
		{Minor, "1.4.2", "1.4.2"}, // missing bump
		{Minor, "1.4.2", "1.5.2"}, // patch not reset
		{Minor, "1.4.2", "1.6.0"}, // skipped a version
		{Minor, "1.4.2", "2.0.0"}, // over-bump
		{Patch, "1.4.2", "1.5.0"}, // over-bump
		{Major, "1.4.2", "1.5.0"}, // under-bump for breaking change
		{None, "1.4.2", "1.4.3"},  // no release for docs/ci/chore
		{Patch, "1.4.2", "1.4.1"}, // downgrade
		{Major, "0.4.2", "0.4.3"}, // breaking needs at least minor in 0.y.z
	}
	for _, c := range bad {
		err := Check(c.b, v(c.base), v(c.head))
		if err == nil {
			t.Errorf("%v %s->%s accepted", c.b, c.base, c.head)
		} else if !strings.Contains(err.Error(), "VERSION") {
			t.Errorf("error should explain the fix: %v", err)
		}
	}
}

func TestRun(t *testing.T) {
	if err := run([]string{"check", "-title", "feat: x", "-base", "0.1.0", "-head", "0.2.0"}); err != nil {
		t.Error(err)
	}
	if err := run([]string{"check", "-title", "feat: x", "-base", "0.1.0", "-head", "v0.2.0"}); err == nil {
		t.Error("v prefix must be rejected in VERSION")
	}
	if err := run([]string{"validate", "1.2.3"}); err != nil {
		t.Error(err)
	}
}
