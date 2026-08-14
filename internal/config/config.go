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

	"github.com/Lavanic/crier/internal/sources"
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
	// burner cookies for the instagram source. creds, so same rules as
	// pushover: config.local.yaml or env, never config.yaml
	Instagram InstagramSession `yaml:"instagram"`
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
	Google  []Feed `yaml:"google"`
	Apple   []Feed `yaml:"apple"`
	Netflix []Feed `yaml:"netflix"`
	// myworkdayjobs tenants (nvidia, ...), polled through their cxs api
	Workday []WorkdaySource `yaml:"workday"`
	// accounts whose story link stickers we mine ie zero2sudo
	Instagram []InstagramSource `yaml:"instagram"`
}

// UserID is instagram's numeric account id, which survives a rename,
// so the api wants that rather than the handle
type InstagramSource struct {
	Name           string `yaml:"name"`
	UserID         string `yaml:"user_id"`
	MinIntervalSec int    `yaml:"min_interval_seconds"`
	// extra random delay, rolled per tick, so the polls don't land on
	// a perfect grid
	JitterSec int `yaml:"jitter_seconds"`
	// polling window like "09-23", empty means around the clock.
	// timezone matters, the droplet runs utc
	ActiveHours string `yaml:"active_hours"`
	Timezone    string `yaml:"timezone"`
}

// the burner's cookies, lifted from a logged-in browser
type InstagramSession struct {
	SessionID string `yaml:"session_id"`
	DsUserID  string `yaml:"ds_user_id"`
	CSRFToken string `yaml:"csrf_token"`
}

// WorkdaySource is one myworkdayjobs tenant. unlike Feed it carries a
// company label (the alert shows "NVIDIA", not the url) and a search
// string that trims workday's large result set down to page 1
type WorkdaySource struct {
	Name           string `yaml:"name"`
	Company        string `yaml:"company"`
	URL            string `yaml:"url"` // the cxs /jobs endpoint
	Search         string `yaml:"search"`
	MinIntervalSec int    `yaml:"min_interval_seconds"`
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

// Instagram please do not ban my account :)
// stories live 24h, so ~35 checks a day still sees every link. the old
// 5 min got the burner challenged in under three hours
const (
	defaultInstagramIntervalSec = 1200
	defaultInstagramJitterSec   = 600
)

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
			Pushover  Pushover         `yaml:"pushover"`
			Instagram InstagramSession `yaml:"instagram"`
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
		if lc.Instagram.SessionID != "" {
			cfg.Instagram = lc.Instagram
		}
	}

	// env beats files
	if v := os.Getenv("CRIER_PUSHOVER_APP_TOKEN"); v != "" {
		cfg.Pushover.AppToken = v
	}
	if v := os.Getenv("CRIER_PUSHOVER_USER_KEY"); v != "" {
		cfg.Pushover.UserKey = v
	}
	if v := os.Getenv("CRIER_IG_SESSION_ID"); v != "" {
		cfg.Instagram.SessionID = v
	}
	if v := os.Getenv("CRIER_IG_DS_USER_ID"); v != "" {
		cfg.Instagram.DsUserID = v
	}
	if v := os.Getenv("CRIER_IG_CSRF_TOKEN"); v != "" {
		cfg.Instagram.CSRFToken = v
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
	for _, feeds := range [][]Feed{c.Sources.GitHub, c.Sources.Google, c.Sources.Apple, c.Sources.Netflix} {
		for i := range feeds {
			if feeds[i].MinIntervalSec == 0 {
				feeds[i].MinIntervalSec = defaultFeedIntervalSec
			}
		}
	}
	for i := range c.Sources.Workday {
		if c.Sources.Workday[i].MinIntervalSec == 0 {
			c.Sources.Workday[i].MinIntervalSec = defaultFeedIntervalSec
		}
	}
	for i := range c.Sources.Instagram {
		if c.Sources.Instagram[i].MinIntervalSec == 0 {
			c.Sources.Instagram[i].MinIntervalSec = defaultInstagramIntervalSec
		}
		if c.Sources.Instagram[i].JitterSec == 0 {
			c.Sources.Instagram[i].JitterSec = defaultInstagramJitterSec
		}
	}
}

func (c *Config) validate() error {
	n := len(c.Sources.Greenhouse) + len(c.Sources.Lever) +
		len(c.Sources.Ashby) + len(c.Sources.GitHub) +
		len(c.Sources.Google) + len(c.Sources.Apple) +
		len(c.Sources.Netflix) + len(c.Sources.Workday) +
		len(c.Sources.Instagram)
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
	for _, feeds := range [][]Feed{c.Sources.GitHub, c.Sources.Google, c.Sources.Apple, c.Sources.Netflix} {
		for _, f := range feeds {
			if f.Name == "" || f.URL == "" {
				return fmt.Errorf("feed source needs both name and url, got name=%q url=%q", f.Name, f.URL)
			}
		}
	}
	for _, w := range c.Sources.Workday {
		if w.Name == "" || w.Company == "" || w.URL == "" {
			return fmt.Errorf("workday source needs name, company and url, got name=%q company=%q url=%q", w.Name, w.Company, w.URL)
		}
	}
	for _, ig := range c.Sources.Instagram {
		if ig.Name == "" || ig.UserID == "" {
			return fmt.Errorf("instagram source needs name and user_id, got name=%q user_id=%q", ig.Name, ig.UserID)
		}
		// a handle pasted here answers 200 with an empty reel, so the
		// source would look healthy while finding nothing forever
		if !isDigits(ig.UserID) {
			return fmt.Errorf("instagram user_id %q must be the numeric account id, not the handle", ig.UserID)
		}
		// fail at startup, not silently at 3am with the wrong window
		if _, err := sources.ParseActiveHours(ig.ActiveHours, ig.Timezone); err != nil {
			return fmt.Errorf("instagram %s: %w", ig.Name, err)
		}
	}
	return nil
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
