//go:build smoke

// pull whatever is on his story right now, run every link through the
// REAL filter, print keep/drop. this is the tuning tool for this
// source, same job the audits did for the boards.
// needs a session cookie in config.local.yaml, skips without one

package main

import (
	"context"
	"testing"
	"time"

	"github.com/Lavanic/crier/internal/config"
	"github.com/Lavanic/crier/internal/filter"
	"github.com/Lavanic/crier/internal/sources"
)

func TestSmokeInstagramStories(t *testing.T) {
	cfg, err := config.Load("../../config.yaml")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.Sources.Instagram) == 0 {
		t.Skip("no instagram sources configured")
	}
	if cfg.Instagram.SessionID == "" {
		t.Skip("no instagram session cookie, add one to config.local.yaml")
	}
	f, err := filter.New(filter.Config{
		Include:                cfg.Filter.Include,
		ExcludeKeywords:        cfg.Filter.ExcludeKeywords,
		ExcludePatterns:        cfg.Filter.ExcludePatterns,
		ExcludeLocations:       cfg.Filter.ExcludeLocations,
		ExcludeCategories:      cfg.Filter.ExcludeCategories,
		CategoryRescueKeywords: cfg.Filter.CategoryRescueKeywords,
	})
	if err != nil {
		t.Fatalf("build filter: %v", err)
	}

	session := sources.Session{
		SessionID: cfg.Instagram.SessionID,
		DsUserID:  cfg.Instagram.DsUserID,
		CSRFToken: cfg.Instagram.CSRFToken,
	}
	// no cache, a smoke run should ask the boards for real
	enricher := sources.NewEnricher(nil)

	for _, ig := range cfg.Sources.Instagram {
		// zero interval, the point is to fetch right now
		src := sources.NewInstagramStories(
			sources.NewInstagramClient(), ig.Name, ig.UserID, session, 0, 0)
		src.SetEnricher(enricher)

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		jobs, err := src.Fetch(ctx)
		cancel()
		if err != nil {
			t.Errorf("%s fetch failed: %v", src.Name(), err)
			continue
		}
		// empty is a quiet night, a dead cookie errors above instead
		if len(jobs) == 0 {
			t.Logf("=== %s: no links on the story right now ===", src.Name())
			continue
		}

		kept, untitled := 0, 0
		t.Logf("=== %s: %d links ===", src.Name(), len(jobs))
		for _, j := range jobs {
			mark := "drop"
			if f.Match(j) {
				mark = "KEEP"
				kept++
			}
			title := j.Title
			if title == "" {
				// nothing to judge these on, they always drop
				title = "(no title, unjudgeable)"
				untitled++
			}
			// display name, not the raw slug, so this reads like the
			// alert actually would on a lock screen
			t.Logf("  [%s] %-18s %s | %s",
				mark, displayName(cfg.DisplayNames, j.Company), title, j.Location)
			t.Logf("         %s", j.URL)
		}
		t.Logf("=== %s: %d kept of %d, %d had no derivable title ===",
			src.Name(), kept, len(jobs), untitled)
	}
}
