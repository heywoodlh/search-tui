// Command semver enforces strict Semantic Versioning (https://semver.org) for
// pull requests.
//
// The version lives in the VERSION file. Every pull request title must be a
// Conventional Commit (https://www.conventionalcommits.org), because PRs are
// squash-merged and the title becomes the commit on main. The title's type
// decides which version bump the PR must make — exactly that bump, no more:
//
//	breaking (`type!:` or a "BREAKING CHANGE:" footer)  major (minor while 0.y.z)
//	feat                                                minor
//	fix, perf, revert                                   patch
//	build, chore, ci, docs, refactor, style, test       none (VERSION unchanged)
//
// Usage:
//
//	semver check -title TITLE [-body BODY] -base BASE_VERSION -head HEAD_VERSION
//	semver validate VERSION
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// semverRE is the official SemVer 2.0.0 regular expression.
var semverRE = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
	`(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?` +
	`(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)

// titleRE matches a Conventional Commit header.
var titleRE = regexp.MustCompile(`^([a-z]+)(\([a-z0-9][a-z0-9._/-]*\))?(!)?: (\S.*)$`)

// Bump is a version increment level.
type Bump int

const (
	None Bump = iota
	Patch
	Minor
	Major
)

func (b Bump) String() string { return [...]string{"none", "patch", "minor", "major"}[b] }

var typeBumps = map[string]Bump{
	"feat": Minor, "fix": Patch, "perf": Patch, "revert": Patch,
	"build": None, "chore": None, "ci": None, "docs": None, "refactor": None, "style": None, "test": None,
}

// Version is a parsed semantic version.
type Version struct {
	Major, Minor, Patch int
	Pre                 string
	raw                 string
}

func (v Version) String() string { return v.raw }

// Parse parses a strict SemVer 2.0.0 version (no "v" prefix).
func Parse(s string) (Version, error) {
	s = strings.TrimSpace(s)
	m := semverRE.FindStringSubmatch(s)
	if m == nil {
		return Version{}, fmt.Errorf("%q is not a valid semantic version (MAJOR.MINOR.PATCH[-PRERELEASE][+BUILD], see https://semver.org)", s)
	}
	v := Version{Pre: m[4], raw: s}
	var err error
	if v.Major, err = strconv.Atoi(m[1]); err != nil {
		return v, err
	}
	if v.Minor, err = strconv.Atoi(m[2]); err != nil {
		return v, err
	}
	if v.Patch, err = strconv.Atoi(m[3]); err != nil {
		return v, err
	}
	return v, nil
}

func (v Version) core() [3]int { return [3]int{v.Major, v.Minor, v.Patch} }

// next returns the version core after bumping v by b.
func (v Version) next(b Bump) [3]int {
	switch b {
	case Major:
		return [3]int{v.Major + 1, 0, 0}
	case Minor:
		return [3]int{v.Major, v.Minor + 1, 0}
	case Patch:
		return [3]int{v.Major, v.Minor, v.Patch + 1}
	}
	return v.core()
}

// Required returns the bump a PR title (and body) demands.
func Required(title, body string) (Bump, error) {
	m := titleRE.FindStringSubmatch(strings.TrimSpace(title))
	if m == nil {
		return None, fmt.Errorf("PR title %q is not a Conventional Commit; use `type(optional scope)!: description`, e.g. `feat(tui): add split panes` or `fix!: change config format`", title)
	}
	typ, breaking := m[1], m[3] == "!"
	b, ok := typeBumps[typ]
	if !ok {
		return None, fmt.Errorf("unknown commit type %q; use one of feat, fix, perf, revert, build, chore, ci, docs, refactor, style, test", typ)
	}
	if breaking || regexp.MustCompile(`(?m)^BREAKING[ -]CHANGE: `).MatchString(body) {
		b = Major
	}
	return b, nil
}

// Check verifies that head is exactly the bump of base that the change needs.
func Check(required Bump, base, head Version) error {
	want := []Bump{required}
	if required == Major && base.Major == 0 {
		// SemVer §4: anything may change in 0.y.z. A breaking change bumps the
		// minor version until 1.0.0; going straight to 1.0.0 is also allowed.
		want = []Bump{Minor, Major}
	}
	if required == None {
		if head.raw != base.raw {
			return fmt.Errorf("this change needs no release, so VERSION must stay %s (it is %s); use feat:/fix:/perf: or a breaking `!` to release", base, head)
		}
		return nil
	}
	for _, b := range want {
		if head.core() == base.next(b) {
			return nil
		}
	}
	var opts []string
	for _, b := range want {
		n := base.next(b)
		opts = append(opts, fmt.Sprintf("%d.%d.%d", n[0], n[1], n[2]))
	}
	return fmt.Errorf("a %s change must bump VERSION from %s to %s (it is %s)", required, base, strings.Join(opts, " or "), head)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "semver:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: semver check -title T -base V -head V, or semver validate V")
	}
	switch args[0] {
	case "validate":
		if len(args) != 2 {
			return errors.New("usage: semver validate VERSION")
		}
		v, err := Parse(args[1])
		if err != nil {
			return err
		}
		fmt.Println(v)
		return nil
	case "check":
		fs := flag.NewFlagSet("check", flag.ContinueOnError)
		title := fs.String("title", "", "pull request title")
		body := fs.String("body", "", "pull request body (for BREAKING CHANGE footers)")
		baseS := fs.String("base", "0.0.0", "VERSION on the base branch")
		headS := fs.String("head", "", "VERSION in the pull request")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		base, err := Parse(*baseS)
		if err != nil {
			return fmt.Errorf("base: %w", err)
		}
		head, err := Parse(*headS)
		if err != nil {
			return fmt.Errorf("VERSION: %w", err)
		}
		req, err := Required(*title, *body)
		if err != nil {
			return err
		}
		if err := Check(req, base, head); err != nil {
			return err
		}
		fmt.Printf("ok: %q requires a %s bump; VERSION %s -> %s\n", *title, req, base, head)
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}
