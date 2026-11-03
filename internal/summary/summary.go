// Package summary implements cheap, on-device extractive summarisation.
//
// The approach is centroid-based multi-document summarisation with
// maximal-marginal-relevance (MMR) selection: sentences close to the
// "consensus" of all results (terms that recur across many sources) and to
// the query are favoured, while near-duplicates are suppressed. Everything
// is linear in the input size and allocation-light, so it runs comfortably
// on very low-end hardware without any model files.
package summary

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Doc is an input text attributed to a source (1-based result number).
type Doc struct {
	Text   string
	Source int
}

// Sentence is a selected summary sentence.
type Sentence struct {
	Text   string
	Source int
	Score  float64
}

type sent struct {
	text   string
	source int
	pos    int // position within its document
	terms  map[string]int
	score  float64
}

var (
	datePrefixRE = regexp.MustCompile(`^(?:[A-Z][a-z]{2,8}\.? \d{1,2},? \d{4}|\d{1,2} [A-Z][a-z]{2,8},? \d{4}|\d+ (?:days?|hours?|weeks?|months?|years?) ago)\s*[·—–-]\s*`)
	splitRE      = regexp.MustCompile(`([.!?…])\s+`)
	ellipsisRE   = regexp.MustCompile(`\s+(?:…|\.\.\.)\s+`)
	codeCharRE   = regexp.MustCompile(`[{}()\[\];=<>:/\\|&*_$]`)
)

// SplitSentences splits text into sentences with light abbreviation handling.
func SplitSentences(text string) []string {
	text = strings.TrimSpace(datePrefixRE.ReplaceAllString(strings.TrimSpace(text), ""))
	if text == "" {
		return nil
	}
	idx := splitRE.FindAllStringSubmatchIndex(text, -1)
	var out []string
	start := 0
	for _, m := range idx {
		end := m[3] // include the punctuation
		cand := text[start:end]
		// Don't split after common abbreviations or single initials.
		lastWord := cand
		if i := strings.LastIndexAny(cand, " ("); i >= 0 {
			lastWord = cand[i+1:]
		}
		lw := strings.ToLower(strings.TrimRight(lastWord, ".!?…"))
		if abbreviations[lw] || (len([]rune(lw)) == 1 && unicode.IsLetter([]rune(lw)[0])) {
			continue
		}
		// Require the next sentence to start with an upper-case letter,
		// digit or quote to avoid splitting "v1.2 is" style text.
		if m[1] < len(text) {
			r := []rune(text[m[1]:])[0]
			if !unicode.IsUpper(r) && !unicode.IsDigit(r) && !strings.ContainsRune("\"'“‘([", r) {
				continue
			}
		}
		if s := strings.TrimSpace(cand); s != "" {
			out = append(out, s)
		}
		start = m[1]
	}
	if s := strings.TrimSpace(text[start:]); s != "" {
		out = append(out, s)
	}
	return out
}

var abbreviations = map[string]bool{
	"e.g": true, "i.e": true, "etc": true, "vs": true, "mr": true, "mrs": true, "ms": true,
	"dr": true, "prof": true, "inc": true, "ltd": true, "co": true, "jr": true, "sr": true,
	"st": true, "no": true, "fig": true, "approx": true, "u.s": true, "e.g.": true, "i.e.": true,
}

// Tokenize lowercases, strips punctuation, removes stopwords and stems.
func Tokenize(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '+' && r != '#'
	}) {
		if len(w) < 2 || stopwords[w] {
			continue
		}
		out = append(out, Stem(w))
	}
	return out
}

// Stem is a tiny suffix-stripping stemmer (good enough for overlap scoring).
func Stem(w string) string {
	if len(w) <= 4 {
		return w
	}
	for _, suf := range []string{"ational", "ization", "fulness", "iveness", "ations", "ation", "ingly", "ments", "ment", "ness", "ings", "ing", "edly", "ies", "ied", "ers", "ed", "er", "ly", "es", "s"} {
		if strings.HasSuffix(w, suf) && len(w)-len(suf) >= 3 {
			base := w[:len(w)-len(suf)]
			if suf == "ies" || suf == "ied" {
				base += "y"
			}
			return base
		}
	}
	return w
}

// Summarize selects up to n representative sentences from docs.
func Summarize(docs []Doc, query string, n int) []Sentence {
	if n <= 0 {
		return nil
	}
	var sents []*sent
	sources := map[int]bool{}
	for _, d := range docs {
		var parts []string
		for _, chunk := range ellipsisRE.Split(d.Text, -1) {
			parts = append(parts, SplitSentences(chunk)...)
		}
		for i, s := range parts {
			s = strings.Trim(s, " …")
			words := len(strings.Fields(s))
			if words < 5 {
				continue
			}
			if words > 45 {
				s = strings.Join(strings.Fields(s)[:45], " ") + "…"
			}
			terms := map[string]int{}
			for _, t := range Tokenize(s) {
				terms[t]++
			}
			if len(terms) < 3 {
				continue
			}
			sents = append(sents, &sent{text: s, source: d.Source, pos: i, terms: terms})
			sources[d.Source] = true
		}
	}
	if len(sents) == 0 {
		return nil
	}
	centroid := centroidOf(sents, len(sources))
	qterms := map[string]bool{}
	for _, t := range Tokenize(query) {
		qterms[t] = true
	}
	maxSource := 1
	for s := range sources {
		if s > maxSource {
			maxSource = s
		}
	}
	for _, s := range sents {
		s.score = cosine(s.terms, centroid)
		if len(qterms) > 0 {
			hit := 0
			for t := range qterms {
				if s.terms[t] > 0 {
					hit++
				}
			}
			s.score += 0.25 * float64(hit) / float64(len(qterms))
		}
		if s.pos == 0 {
			s.score += 0.05
		}
		// Prefer higher-ranked results slightly.
		s.score += 0.08 * (1 - float64(s.source-1)/float64(maxSource))
		// Penalise question-like and truncated fragments.
		if strings.HasSuffix(s.text, "?") {
			s.score -= 0.1
		}
		// Penalise code-heavy text, run-ons and mid-sentence fragments.
		if ratio := float64(len(codeCharRE.FindAllStringIndex(s.text, -1))) / float64(len(s.text)); ratio > 0.04 {
			s.score -= 0.3
		}
		if strings.HasSuffix(s.text, "…") {
			s.score -= 0.05
		}
		if r := []rune(s.text)[0]; unicode.IsLower(r) {
			s.score -= 0.08
		} else if !unicode.IsLetter(r) && !strings.ContainsRune("\"'“(", r) {
			s.score -= 0.15
		}
	}
	return mmr(sents, n, 0.72)
}

func centroidOf(sents []*sent, nsources int) map[string]float64 {
	tf := map[string]int{}
	sf := map[string]int{}
	df := map[string]map[int]bool{}
	for _, s := range sents {
		for t, c := range s.terms {
			tf[t] += c
			sf[t]++
			if df[t] == nil {
				df[t] = map[int]bool{}
			}
			df[t][s.source] = true
		}
	}
	n := float64(len(sents))
	c := make(map[string]float64, len(tf))
	for t, f := range tf {
		idf := math.Log(1 + n/float64(sf[t]))
		consensus := 1 + float64(len(df[t]))/float64(max(nsources, 1))
		c[t] = math.Log(1+float64(f)) * idf * consensus
	}
	return c
}

func cosine(a map[string]int, b map[string]float64) float64 {
	var dot, na, nb float64
	for t, c := range a {
		fc := float64(c)
		dot += fc * b[t]
		na += fc * fc
	}
	for _, v := range b {
		nb += v * v
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func jaccard(a, b map[string]int) float64 {
	inter := 0
	for t := range a {
		if b[t] > 0 {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func mmr(sents []*sent, n int, lambda float64) []Sentence {
	sort.SliceStable(sents, func(i, j int) bool { return sents[i].score > sents[j].score })
	// Only consider the strongest candidates; keeps this O(n·k).
	if len(sents) > 60 {
		sents = sents[:60]
	}
	var chosen []*sent
	used := make([]bool, len(sents))
	for len(chosen) < n {
		best, bestVal := -1, math.Inf(-1)
		for i, s := range sents {
			if used[i] {
				continue
			}
			red := 0.0
			for _, c := range chosen {
				red = math.Max(red, jaccard(s.terms, c.terms))
			}
			if red > 0.6 {
				continue
			}
			v := lambda*s.score - (1-lambda)*red
			if v > bestVal {
				best, bestVal = i, v
			}
		}
		if best < 0 {
			break
		}
		used[best] = true
		chosen = append(chosen, sents[best])
	}
	out := make([]Sentence, len(chosen))
	for i, c := range chosen {
		out[i] = Sentence{Text: c.text, Source: c.source, Score: c.score}
	}
	return out
}

// KeyTerms returns up to n salient terms (surface forms) not in the query.
func KeyTerms(docs []Doc, query string, n int) []string {
	q := map[string]bool{}
	for _, t := range Tokenize(query) {
		q[t] = true
	}
	type agg struct {
		count   int
		sources map[int]bool
		forms   map[string]int
	}
	terms := map[string]*agg{}
	for _, d := range docs {
		for _, w := range strings.FieldsFunc(d.Text, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '+' && r != '#' && r != '-'
		}) {
			w = strings.Trim(w, "-")
			lw := strings.ToLower(w)
			if len(lw) < 3 || stopwords[lw] || isNumber(lw) {
				continue
			}
			st := Stem(lw)
			if q[st] {
				continue
			}
			a := terms[st]
			if a == nil {
				a = &agg{sources: map[int]bool{}, forms: map[string]int{}}
				terms[st] = a
			}
			a.count++
			a.sources[d.Source] = true
			a.forms[w]++
		}
	}
	type kv struct {
		form  string
		score float64
	}
	var list []kv
	for _, a := range terms {
		if len(a.sources) < 2 {
			continue
		}
		best, bc := "", 0
		for f, c := range a.forms {
			if c > bc || (c == bc && f < best) {
				best, bc = f, c
			}
		}
		list = append(list, kv{best, float64(len(a.sources))*2 + math.Log(float64(a.count))})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].score != list[j].score {
			return list[i].score > list[j].score
		}
		return list[i].form < list[j].form
	})
	var out []string
	for _, kv := range list {
		if len(out) >= n {
			break
		}
		out = append(out, kv.form)
	}
	return out
}

func isNumber(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) && r != '.' && r != ',' {
			return false
		}
	}
	return true
}

// SummarizeText summarises a single long text (such as a fetched page).
// Each paragraph is treated as its own "source" so the centroid reflects
// the whole page, and earlier paragraphs get a small lead bonus.
func SummarizeText(text, query string, n int) []Sentence {
	paras := strings.Split(text, "\n")
	var docs []Doc
	for i, p := range paras {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "```") || strings.HasPrefix(p, "#") || strings.HasPrefix(p, "    ") {
			continue
		}
		p = strings.TrimPrefix(p, "- ")
		docs = append(docs, Doc{Text: p, Source: i + 1})
		if len(docs) >= 400 {
			break
		}
	}
	return Summarize(docs, query, n)
}
