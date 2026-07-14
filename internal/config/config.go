// Package config loads crier's yaml config.
//
// two-file setup: config.yaml holds everything safe to commit (slugs,
// filters), and an optional config.local.yaml next to it holds the
// pushover creds. local file is gitignored. env vars win over both,
// handy on the vps where a systemd EnvironmentFile is easier than
// scp-ing yaml around.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DBPath  string  `yaml:"db_path"`
	Filter  Filter  `yaml:"filter"`
	Sources Sources `yaml:"sources"`
	// pretty names for alert titles, slugs like "andurilindustries"
	// read terribly on a lock screen. lookup only, dedup keys always
	// use the raw slug so renaming here never re-alerts old jobs
	DisplayNames map[string]string `yaml:"display_names"`
	// companies whose matches siren through dnd (pushover emergency).
	// everything else arrives as a normal ping that respects quiet
	// hours. matched case-insensitively against the display name
	PriorityCompanies []string `yaml:"priority_companies"`
	Pushover          Pushover `yaml:"pushover"`
}

type Filter struct {
	// a title passes if it matches ANY include pattern
	// and contains NO exclude keyword
	Include         []string `yaml:"include"`
	ExcludeKeywords []string `yaml:"exclude_keywords"`
	// raw regexes run against title AND url, for stuff like start
	// dates that only show up in workday url slugs
	ExcludePatterns []string `yaml:"exclude_patterns"`
	// non-us markers. a posting dies only if every listed location
	// matches one of these
	ExcludeLocations []string `yaml:"exclude_locations"`
	// feed category tags to drop (product, hardware), with rescue
	// keywords that let software-sounding titles through anyway
	ExcludeCategories      []string `yaml:"exclude_categories"`
	CategoryRescueKeywords []string `yaml:"category_rescue_keywords"`
}

type Sources struct {
	// ats sources are just lists of board slugs
	Greenhouse []string `yaml:"greenhouse"`
	Lever      []string `yaml:"lever"`
	Ashby      []string `yaml:"ashby"`
	// github aggregator feeds (simplify, vanshb03)
	GitHub []Feed `yaml:"github"`
	// big-tech careers pages parsed straight off their html, for the
	// companies not on any public ats board
	Google []Feed `yaml:"google"`
	Apple  []Feed `yaml:"apple"`
}

// Feed is a named url polled on its own cadence: github aggregators,
// google and apple careers queries all share this shape
type Feed struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
	// these endpoints cache or don't need 30s polling, the
	// orchestrator skips a feed it polled more recently than this
	MinIntervalSec int `yaml:"min_interval_seconds"`
}

type Pushover struct {
	AppToken string `yaml:"app_token"`
	UserKey  string `yaml:"user_key"`
}

// HasCreds reports whether pushover is usable. main checks this
// unless --dry-run, config doesn't get to decide that policy
func (p Pushover) HasCreds() bool {
	return p.AppToken != "" && p.UserKey != ""
}

const defaultFeedIntervalSec = 300

// Load reads the main config, layers config.local.yaml on top if it
// exists next to it, then applies env var overrides, then validates.
func Load(path string) (*Config, error) {
	var cfg Config
	if err := unmarshalFile(path, &cfg); err != nil {
		return nil, err
	}

	// the local overlay may ONLY hold creds. decoding it into the full
	// Config would let a stray "sources:" key silently replace the
	// entire slug list (yaml replaces whole lists), so it gets its own
	// struct and any other top-level key is a hard error
	local := filepath.Join(filepath.Dir(path), "config.local.yaml")
	if _, err := os.Stat(local); err == nil {
		var lc struct {
			Pushover Pushover `yaml:"pushover"`
		}
		b, err := os.ReadFile(local)
		if err != nil {
			return nil, err
		}
		dec := yaml.NewDecoder(bytes.NewReader(b))
		dec.KnownFields(true)
		if err := dec.Decode(&lc); err != nil {
			return nil, fmt.Errorf("%s (creds only, everything else goes in config.yaml): %w", local, err)
		}
		if lc.Pushover.AppToken != "" {
			cfg.Pushover.AppToken = lc.Pushover.AppToken
		}
		if lc.Pushover.UserKey != "" {
			cfg.Pushover.UserKey = lc.Pushover.UserKey
		}
	}

	// env beats files
	if v := os.Getenv("CRIER_PUSHOVER_APP_TOKEN"); v != "" {
		cfg.Pushover.AppToken = v
	}
	if v := os.Getenv("CRIER_PUSHOVER_USER_KEY"); v != "" {
		cfg.Pushover.UserKey = v
	}

	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &cfg, nil
}

func unmarshalFile(path string, into *Config) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	// KnownFields makes a typo'd key an error instead of silently
	// ignoring it. a misspelled slug list vanishing quietly would be
	// the worst kind of bug for this bot
	dec.KnownFields(true)
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func (c *Config) applyDefaults() {
	if c.DBPath == "" {
		c.DBPath = "crier.db"
	}
	for _, feeds := range [][]Feed{c.Sources.GitHub, c.Sources.Google, c.Sources.Apple} {
		for i := range feeds {
			if feeds[i].MinIntervalSec == 0 {
				feeds[i].MinIntervalSec = defaultFeedIntervalSec
			}
		}
	}
}

func (c *Config) validate() error {
	n := len(c.Sources.Greenhouse) + len(c.Sources.Lever) +
		len(c.Sources.Ashby) + len(c.Sources.GitHub) +
		len(c.Sources.Google) + len(c.Sources.Apple)
	if n == 0 {
		return errors.New("no sources configured")
	}
	if len(c.Filter.Include) == 0 {
		return errors.New("filter.include is empty, bot would match nothing")
	}
	// compile every regex now so a bad pattern fails at startup,
	// not silently at match time
	for _, pat := range c.Filter.Include {
		if _, err := regexp.Compile(pat); err != nil {
			return fmt.Errorf("bad include pattern %q: %w", pat, err)
		}
	}
	for _, pat := range c.Filter.ExcludePatterns {
		if _, err := regexp.Compile(pat); err != nil {
			return fmt.Errorf("bad exclude pattern %q: %w", pat, err)
		}
	}
	for _, feeds := range [][]Feed{c.Sources.GitHub, c.Sources.Google, c.Sources.Apple} {
		for _, f := range feeds {
			if f.Name == "" || f.URL == "" {
				return fmt.Errorf("feed source needs both name and url, got name=%q url=%q", f.Name, f.URL)
			}
		}
	}
	return nil
}
