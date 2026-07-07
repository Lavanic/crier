package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimal but valid config used as the base for most tests
const baseYAML = `
filter:
  include:
    - '(?i)new\s*grad'
  exclude_keywords: [intern, senior]
sources:
  greenhouse: [stripe, anthropic]
  lever: [spotify]
  github:
    - name: simplifyjobs
      url: https://example.com/listings.json
`

// writes yaml files into a temp dir and returns the main config path
func writeConfig(t *testing.T, main string, local string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	if local != "" {
		lp := filepath.Join(dir, "config.local.yaml")
		if err := os.WriteFile(lp, []byte(local), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestLoadBasics(t *testing.T) {
	cfg, err := Load(writeConfig(t, baseYAML, ""))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Sources.Greenhouse; len(got) != 2 || got[0] != "stripe" {
		t.Errorf("greenhouse slugs = %v, want [stripe anthropic]", got)
	}
	if cfg.DBPath != "crier.db" {
		t.Errorf("DBPath default = %q, want crier.db", cfg.DBPath)
	}
	if got := cfg.Sources.GitHub[0].MinIntervalSec; got != 300 {
		t.Errorf("github min interval default = %d, want 300", got)
	}
	if cfg.Pushover.HasCreds() {
		t.Error("HasCreds should be false with no creds anywhere")
	}
}

func TestLocalOverlay(t *testing.T) {
	local := `
pushover:
  app_token: tok123
  user_key: usr456
`
	cfg, err := Load(writeConfig(t, baseYAML, local))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Pushover.HasCreds() {
		t.Fatal("creds from local overlay not picked up")
	}
	// overlay must not clobber fields it doesn't mention
	if len(cfg.Sources.Greenhouse) != 2 {
		t.Errorf("overlay wiped greenhouse slugs: %v", cfg.Sources.Greenhouse)
	}
}

func TestEnvBeatsFile(t *testing.T) {
	local := "pushover: {app_token: filetok, user_key: fileusr}\n"
	t.Setenv("CRIER_PUSHOVER_APP_TOKEN", "envtok")
	cfg, err := Load(writeConfig(t, baseYAML, local))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Pushover.AppToken != "envtok" {
		t.Errorf("AppToken = %q, want env value to win", cfg.Pushover.AppToken)
	}
	if cfg.Pushover.UserKey != "fileusr" {
		t.Errorf("UserKey = %q, unset env should leave file value alone", cfg.Pushover.UserKey)
	}
}

// table-driven: each bad config should fail with a recognizable error
func TestValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "typo'd key rejected",
			yaml:    strings.Replace(baseYAML, "greenhouse:", "grenhouse:", 1),
			wantErr: "grenhouse",
		},
		{
			name: "no sources",
			yaml: `
filter:
  include: ['x']
sources: {}
`,
			wantErr: "no sources",
		},
		{
			name: "empty include list",
			yaml: `
filter:
  exclude_keywords: [intern]
sources:
  greenhouse: [stripe]
`,
			wantErr: "include is empty",
		},
		{
			name:    "bad regex",
			yaml:    strings.Replace(baseYAML, `(?i)new\s*grad`, "([unclosed", 1),
			wantErr: "bad include pattern",
		},
		{
			name: "github feed missing url",
			yaml: `
filter:
  include: ['x']
sources:
  github:
    - name: simplifyjobs
`,
			wantErr: "needs both name and url",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tt.yaml, ""))
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not mention %q", err, tt.wantErr)
			}
		})
	}
}
