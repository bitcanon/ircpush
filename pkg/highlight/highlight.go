/*
MIT License

Copyright (c) 2025 Mikael Schultz <mikael@conf-t.se>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/
package highlight

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bitcanon/ircpush/pkg/config"
)

const (
	ircColor = "\x03"
	ircBold  = "\x02"
	ircUnder = "\x1F"
	ircReset = "\x0F"
)

type Highlighter struct {
	rules []compiledRule
}

type compiledRule struct {
	re         *regexp.Regexp
	stylePref  string
	wholeLine  bool
	includes   []string // lower-cased globs
	excludes   []string // lower-cased globs
	hasFilters bool
}

func New(hc config.HighlightConfig) *Highlighter {
	hl := &Highlighter{}
	for _, r := range hc.Rules {
		re := compileRule(r)
		if re == nil {
			continue
		}
		style := buildStyle(r)
		cr := compiledRule{
			re:        re,
			stylePref: style,
			wholeLine: r.WholeLine,
		}
		// compile channel filters (store as lowercase patterns)
		for _, p := range r.Channels {
			p = strings.TrimSpace(p)
			if p != "" {
				cr.includes = append(cr.includes, strings.ToLower(p))
			}
		}
		for _, p := range r.ExcludeChannels {
			p = strings.TrimSpace(p)
			if p != "" {
				cr.excludes = append(cr.excludes, strings.ToLower(p))
			}
		}
		cr.hasFilters = len(cr.includes) > 0 || len(cr.excludes) > 0

		hl.rules = append(hl.rules, cr)
	}
	return hl
}

// Apply keeps backward compatibility (no channel context).
func (h *Highlighter) Apply(s string) string {
	return h.ApplyFor("", s)
}

// ApplyFor applies highlighting considering the target channel.
// If channel is empty, only rules without channel filters are considered.
func (h *Highlighter) ApplyFor(channel string, s string) string {
	if s == "" || len(h.rules) == 0 {
		return s
	}
	chLower := strings.ToLower(strings.TrimSpace(channel))

	out := s

	// Whole-line rules: first match wins, only if rule applies to this channel.
	for _, r := range h.rules {
		if !h.ruleAppliesTo(r, chLower) {
			continue
		}
		if r.wholeLine && r.re.MatchString(out) {
			return r.stylePref + out + ircReset
		}
	}

	// Per-match replacements
	for _, r := range h.rules {
		if !h.ruleAppliesTo(r, chLower) || r.wholeLine {
			continue
		}
		out = r.re.ReplaceAllStringFunc(out, func(m string) string {
			return r.stylePref + m + ircReset
		})
	}
	return out
}

func (h *Highlighter) ruleAppliesTo(r compiledRule, chLower string) bool {
	// No channel context provided: only rules without filters apply.
	if chLower == "" {
		return !r.hasFilters
	}
	// Exclusions win
	for _, ex := range r.excludes {
		if globMatch(ex, chLower) {
			return false
		}
	}
	// If includes specified, require a match
	if len(r.includes) > 0 {
		for _, in := range r.includes {
			if globMatch(in, chLower) {
				return true
			}
		}
		return false
	}
	// No includes -> applies to all (unless excluded above)
	return true
}

func globMatch(pattern, name string) bool {
	ok, err := filepath.Match(pattern, name)
	if err != nil {
		// On invalid pattern, fail closed (no match)
		return false
	}
	return ok
}

func compileRule(r config.HighlightRule) *regexp.Regexp {
	pat := strings.TrimSpace(r.Pattern)
	if pat == "" {
		return nil
	}
	switch strings.ToLower(r.Kind) {
	case "regex":
		if r.CaseInsensitive && !strings.HasPrefix(pat, "(?i)") {
			pat = "(?i)" + pat
		}
	case "word", "":
		pat = `\b` + regexp.QuoteMeta(pat) + `\b`
		if r.CaseInsensitive {
			pat = "(?i)" + pat
		}
	default:
		return nil
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil
	}
	return re
}

func buildStyle(r config.HighlightRule) string {
	var b strings.Builder
	if r.Bold {
		b.WriteString(ircBold)
	}
	if r.Underline {
		b.WriteString(ircUnder)
	}
	if code := colorToCode(r.Color); code != "" {
		b.WriteString(ircColor)
		b.WriteString(code)
	}
	return b.String()
}

func colorToCode(name string) string {
	n := strings.TrimSpace(strings.ToLower(name))
	if n == "" {
		return ""
	}
	if isNumericColor(n) {
		return normalizeNumeric(n)
	}
	switch n {
	case "white":
		return "00"
	case "black":
		return "01"
	case "blue", "navy":
		return "02"
	case "green":
		return "03"
	case "red":
		return "04"
	case "brown", "maroon":
		return "05"
	case "purple":
		return "06"
	case "orange", "olive":
		return "07"
	case "yellow":
		return "08"
	case "lightgreen", "lime":
		return "09"
	case "teal", "cyan":
		return "10"
	case "lightcyan", "aqua":
		return "11"
	case "lightblue", "royal":
		return "12"
	case "pink", "fuchsia":
		return "13"
	case "grey", "gray":
		return "14"
	case "lightgrey", "lightgray", "silver":
		return "15"
	default:
		return ""
	}
}

func isNumericColor(s string) bool {
	for _, ch := range s {
		if (ch < '0' || ch > '9') && ch != ',' {
			return false
		}
	}
	return true
}

func normalizeNumeric(s string) string {
	parts := strings.Split(s, ",")
	for i, p := range parts {
		if len(p) == 1 {
			parts[i] = "0" + p
		} else if len(p) == 2 {
			parts[i] = p
		} else if len(p) > 2 {
			parts[i] = p[:2]
		}
	}
	return strings.Join(parts, ",")
}
