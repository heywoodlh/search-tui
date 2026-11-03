// Package safe neutralises untrusted text before it reaches a terminal and
// validates URLs before they are handed to other programs.
//
// Everything that comes off the network (search results, fetched pages,
// code files, error bodies) is attacker-controlled. Printed verbatim, it
// could carry terminal escape sequences that rewrite the screen, set the
// clipboard (OSC 52), change the window title, or smuggle text into a later
// paste; bidirectional-override characters can make code read differently
// from how it executes ("Trojan Source").
package safe

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"
)

// isUnsafeRune reports runes that are never displayed as-is: C0 controls
// (except tab and newline, handled by callers), DEL, C1 controls (which
// include the 8-bit CSI/OSC introducers), and bidi controls.
func isUnsafeRune(r rune) bool {
	switch {
	case r < 0x20 && r != '\n' && r != '\t':
		return true
	case r == 0x7f:
		return true
	case r >= 0x80 && r <= 0x9f:
		return true
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r == 0x200e, r == 0x200f, r == 0x061c:
		return true
	}
	return false
}

// Text sanitises multi-line text: invalid UTF-8 is replaced, and control,
// escape and bidi-override characters are removed. Newlines and tabs stay.
func Text(s string) string {
	if !needsWork(s) {
		return s
	}
	s = strings.ToValidUTF8(s, "�")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !isUnsafeRune(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Line sanitises single-line text (titles, URLs, metadata): like Text, but
// newlines and tabs become spaces.
func Line(s string) string {
	s = Text(s)
	if strings.ContainsAny(s, "\n\t") {
		s = strings.Map(func(r rune) rune {
			if r == '\n' || r == '\t' {
				return ' '
			}
			return r
		}, s)
	}
	return s
}

func needsWork(s string) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		if isUnsafeRune(r) {
			return true
		}
	}
	return false
}

// ErrUnsafeURL is returned for links that must not be handed to a browser.
var ErrUnsafeURL = errors.New("refusing to open link: only http and https URLs are allowed")

// WebURL validates a link before it is opened in a browser. Only absolute
// http(s) URLs with a host and without control characters are allowed: other
// schemes (file:, javascript:, smb:, ms-msdt:, custom app handlers ...) can
// read local files, leak credentials or launch programs.
func WebURL(raw string) error {
	if raw == "" || raw != Line(raw) || strings.ContainsAny(raw, " \"'<>\\`") {
		return ErrUnsafeURL
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ErrUnsafeURL
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Opaque != "" {
		return ErrUnsafeURL
	}
	return nil
}

// secretParams are query parameters whose values are redacted in messages.
var secretParams = map[string]bool{
	"key": true, "api_key": true, "apikey": true, "token": true, "access_token": true,
	"auth": true, "password": true, "secret": true, "sig": true, "signature": true,
}

// RedactURL hides credentials (userinfo and secret-looking query values) in
// a URL so it can be shown in errors.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "(unparseable URL)"
	}
	if u.User != nil {
		u.User = url.User("REDACTED")
	}
	q := u.Query()
	changed := false
	for k := range q {
		if secretParams[strings.ToLower(k)] {
			q.Set(k, "REDACTED")
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
	}
	return u.String()
}
