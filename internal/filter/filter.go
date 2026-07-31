// Package filter decides if a job posting is alert-worthy: title must
// match an include pattern, then survive the exclude keywords/patterns
// and the category and location gates (which fail open when the info
// is missing). everything comes from config, tuning never needs a rebuild
package filter

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Lavanic/crier/internal/sources"
)

// Config mirrors the filter: section of config.yaml. main copies the
// yaml struct into this one so the filter doesn't import config
type Config struct {
	Include                []string
	ExcludeKeywords        []string
	ExcludePatterns        []string
	ExcludeLocations       []string
	ExcludeCategories      []string
	CategoryRescueKeywords []string
}

type Filter struct {
	include     []*regexp.Regexp
	excludeKw   *regexp.Regexp   // nil when there are no exclude keywords
	excludePat  []*regexp.Regexp // raw regexes, run on title AND url
	nonUS       *regexp.Regexp   // nil disables the location gate
	badCategory []string         // lowercased, matched as substrings
	rescue      *regexp.Regexp   // titles matching this dodge the category gate
}

func New(cfg Config) (*Filter, error) {
	f := &Filter{}
	for _, pat := range cfg.Include {
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("include pattern %q: %w", pat, err)
		}
		f.include = append(f.include, re)
	}
	var err error
	if f.excludeKw, err = wordList(cfg.ExcludeKeywords); err != nil {
		return nil, fmt.Errorf("exclude keywords: %w", err)
	}
	for _, pat := range cfg.ExcludePatterns {
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("exclude pattern %q: %w", pat, err)
		}
		f.excludePat = append(f.excludePat, re)
	}
	if f.nonUS, err = wordList(cfg.ExcludeLocations); err != nil {
		return nil, fmt.Errorf("exclude locations: %w", err)
	}
	if f.rescue, err = wordList(cfg.CategoryRescueKeywords); err != nil {
		return nil, fmt.Errorf("category rescue keywords: %w", err)
	}
	for _, c := range cfg.ExcludeCategories {
		f.badCategory = append(f.badCategory, strings.ToLower(c))
	}
	return f, nil
}

// wordList builds one case-insensitive \b-bounded alternation, nil for
// empty. substrings would be wrong, "intern" is inside "international"
func wordList(words []string) (*regexp.Regexp, error) {
	if len(words) == 0 {
		return nil, nil
	}
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = regexp.QuoteMeta(w)
	}
	return regexp.Compile(`(?i)\b(` + strings.Join(quoted, "|") + `)\b`)
}

// Match reports whether a posting should trigger an alert
func (f *Filter) Match(j sources.Job) bool {
	included := false
	for _, re := range f.include {
		if re.MatchString(j.Title) {
			included = true
			break
		}
	}
	if !included {
		return false
	}
	if f.excludeKw != nil && f.excludeKw.MatchString(j.Title) {
		return false
	}
	// urls are checked too because workday slugs leak facts the title
	// hides, like vanguard's "2026-Start-Date"
	for _, re := range f.excludePat {
		if re.MatchString(j.Title) || re.MatchString(j.URL) {
			return false
		}
	}
	if f.dropByCategory(j) {
		return false
	}
	return !f.dropByLocation(j.Location)
}

// dropByCategory kills excluded feed disciplines. the rescue list is
// there because simplify files firmware/embedded roles under Hardware
func (f *Filter) dropByCategory(j sources.Job) bool {
	if len(f.badCategory) == 0 || j.Category == "" {
		return false
	}
	cat := strings.ToLower(j.Category)
	bad := false
	for _, c := range f.badCategory {
		if strings.Contains(cat, c) {
			bad = true
			break
		}
	}
	if !bad {
		return false
	}
	return f.rescue == nil || !f.rescue.MatchString(j.Title)
}

// some listings name both countries inside ONE part that never gets
// split, like cerebras' "US and Canada Offices". an explicit us
// mention wins so those don't read as fully non-us
var usSignal = regexp.MustCompile(`(?i)(\bus\b|\busa\b|\bu\.s\.?|\bunited\s+states\b)`)

// dropByLocation kills a posting only when EVERY listed location is
// non-us. empty or unrecognized locations pass, fail open
func (f *Filter) dropByLocation(location string) bool {
	if f.nonUS == nil {
		return false
	}
	sawOne := false
	// feeds separate multi-office listings with pipes, greenhouse uses
	// semicolons ("New York, NY; Singapore"). NOT commas, those live
	// inside single locations ("Toronto, ON, CA")
	for _, part := range strings.FieldsFunc(location, func(r rune) bool { return r == '|' || r == ';' }) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		sawOne = true
		if !f.nonUS.MatchString(part) || usSignal.MatchString(part) {
			return false
		}
	}
	return sawOne
}
