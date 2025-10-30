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
	"sort"
	"strconv"
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
	includes   []string
	excludes   []string
	hasFilters bool
	groupIdxs  []int // new: which capture groups to color (1-based)
}

func New(hc config.HighlightConfig) *Highlighter {
	hl := &Highlighter{}
	for _, r := range hc.Rules {
		re := compileRule(r)
		if re == nil {
			continue
		}
		cr := compiledRule{
			re:         re,
			stylePref:  buildStyle(r),
			wholeLine:  r.WholeLine,
			includes:   nil,
			excludes:   nil,
			hasFilters: false,
		}
		// channels include/exclude
		for _, p := range r.Channels {
			if p = strings.TrimSpace(p); p != "" {
				cr.includes = append(cr.includes, strings.ToLower(p))
			}
		}
		for _, p := range r.ExcludeChannels {
			if p = strings.TrimSpace(p); p != "" {
				cr.excludes = append(cr.excludes, strings.ToLower(p))
			}
		}
		cr.hasFilters = len(cr.includes) > 0 || len(cr.excludes) > 0

		// groups: map names/indices to int list
		if len(r.Groups) > 0 {
			var idxs []int
			for _, g := range r.Groups {
				g = strings.TrimSpace(g)
				if g == "" {
					continue
				}
				if i, err := strconv.Atoi(g); err == nil {
					if i > 0 {
						idxs = append(idxs, i)
					}
					continue
				}
				if i := re.SubexpIndex(g); i > 0 {
					idxs = append(idxs, i)
				}
			}
			// dedupe + sort
			idxs = uniqueInts(idxs)
			sort.Ints(idxs)
			cr.groupIdxs = idxs
		}

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

	// whole-line first
	for _, r := range h.rules {
		if !h.ruleAppliesTo(r, chLower) {
			continue
		}
		if r.wholeLine && r.re.MatchString(out) {
			return r.stylePref + out + ircReset
		}
	}

	// per-match/group
	for _, r := range h.rules {
		if !h.ruleAppliesTo(r, chLower) || r.wholeLine {
			continue
		}
		if len(r.groupIdxs) == 0 {
			out = r.re.ReplaceAllStringFunc(out, func(m string) string {
				return r.stylePref + m + ircReset
			})
			continue
		}
		out = applyGroups(out, r.re, r.groupIdxs, r.stylePref)
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

func applyGroups(s string, re *regexp.Regexp, groups []int, style string) string {
	matches := re.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return s
	}
	// Build list of intervals [start,end) to color across all matches
	type seg struct{ a, b int }
	var segs []seg
	for _, idx := range matches {
		for _, g := range groups {
			// idx layout: [m0s,m0e, g1s,g1e, g2s,g2e, ...]
			pos := 2 * g
			if pos+1 >= len(idx) {
				continue
			}
			a, b := idx[pos], idx[pos+1]
			if a >= 0 && b >= 0 && b > a {
				segs = append(segs, seg{a: a, b: b})
			}
		}
	}
	if len(segs) == 0 {
		return s
	}
	// sort by start, merge overlaps
	sort.Slice(segs, func(i, j int) bool { return segs[i].a < segs[j].a })
	merged := segs[:0]
	for _, cur := range segs {
		n := len(merged)
		if n == 0 || cur.a > merged[n-1].b {
			merged = append(merged, cur)
		} else if cur.b > merged[n-1].b {
			merged[n-1].b = cur.b
		}
	}

	var bld strings.Builder
	last := 0
	for _, sg := range merged {
		if sg.a > last {
			bld.WriteString(s[last:sg.a])
		}
		bld.WriteString(style)
		bld.WriteString(s[sg.a:sg.b])
		bld.WriteString(ircReset)
		last = sg.b
	}
	if last < len(s) {
		bld.WriteString(s[last:])
	}
	return bld.String()
}

func uniqueInts(in []int) []int {
	seen := make(map[int]struct{}, len(in))
	out := make([]int, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
