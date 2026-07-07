// Package filter decides if a job title is alert-worthy.
// two-step rule: title must match at least one include pattern, then
// must not contain any exclude keyword. both lists come from config
// so tuning never needs a rebuild
package filter

import (
	"fmt"
	"regexp"
	"strings"
)

type Filter struct {
	include []*regexp.Regexp
	exclude *regexp.Regexp // nil when there are no exclude keywords
}

func New(include, exclude []string) (*Filter, error) {
	f := &Filter{}
	for _, pat := range include {
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("include pattern %q: %w", pat, err)
		}
		f.include = append(f.include, re)
	}
	if len(exclude) > 0 {
		// one big case-insensitive alternation with \b word boundaries.
		// plain substring matching would be wrong here: "intern" is
		// inside "international" and we'd silently drop legit roles
		// like "New Grad SWE, International Payments"
		quoted := make([]string, len(exclude))
		for i, kw := range exclude {
			quoted[i] = regexp.QuoteMeta(kw)
		}
		re, err := regexp.Compile(`(?i)\b(` + strings.Join(quoted, "|") + `)\b`)
		if err != nil {
			return nil, fmt.Errorf("exclude keywords: %w", err)
		}
		f.exclude = re
	}
	return f, nil
}

// Match reports whether a title should trigger an alert
func (f *Filter) Match(title string) bool {
	included := false
	for _, re := range f.include {
		if re.MatchString(title) {
			included = true
			break
		}
	}
	if !included {
		return false
	}
	return f.exclude == nil || !f.exclude.MatchString(title)
}
