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
	DBPath   string   `yaml:"db_path"`
	Filter   Filter   `yaml:"filter"`
	Sources  Sources  `yaml:"sources"`
	Pushover Pushover `yaml:"pushover"`
}

type Filter struct {
	// a title passes if it matches ANY include pattern
	// and contains NO exclude keyword
	Include         []string `yaml:"include"`
	ExcludeKeywords []string `yaml:"exclude_keywords"`
}

type Sources struct {
	// ats sources are just lists of board slugs
	Greenhouse []string `yaml:"greenhouse"`
	Lever      []string `yaml:"lever"`
	Ashby      []string `yaml:"ashby"`
	// github aggregator feeds (simplify, vanshb03) have their own shape
	GitHub []GitHubFeed `yaml:"github"`
}

type GitHubFeed struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
	// github's raw cdn caches ~5 min so polling faster is wasted,
	// the orchestrator skips this feed if it polled more recently
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

const defaultGitHubIntervalSec = 300

// Load reads the main config, layers config.local.yaml on top if it
// exists next to it, then applies env var overrides, then validates.
func Load(path string) (*Config, error) {
	var cfg Config
	if err := unmarshalFile(path, &cfg); err != nil {
		return nil, err
	}

	// overlay: unmarshaling into the SAME struct only touches keys
	// present in the local file, everything else stays as loaded above
	local := filepath.Join(filepath.Dir(path), "config.local.yaml")
	if _, err := os.Stat(local); err == nil {
		if err := unmarshalFile(local, &cfg); err != nil {
			return nil, err
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
	for i := range c.Sources.GitHub {
		if c.Sources.GitHub[i].MinIntervalSec == 0 {
			c.Sources.GitHub[i].MinIntervalSec = defaultGitHubIntervalSec
		}
	}
}

func (c *Config) validate() error {
	n := len(c.Sources.Greenhouse) + len(c.Sources.Lever) +
		len(c.Sources.Ashby) + len(c.Sources.GitHub)
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
	for _, f := range c.Sources.GitHub {
		if f.Name == "" || f.URL == "" {
			return fmt.Errorf("github feed needs both name and url, got name=%q url=%q", f.Name, f.URL)
		}
	}
	return nil
}
