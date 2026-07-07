//go:build smoke

// live tests that hit the real endpoints, run with `make smoke`.
// kept behind a build tag so plain `go test ./...` never touches
// the network

package sources

import (
	"context"
	"testing"
	"time"
)

func TestSmokeGitHubFeeds(t *testing.T) {
	client := NewHTTPClient()
	feeds := map[string]string{
		"simplifyjobs": "https://raw.githubusercontent.com/SimplifyJobs/New-Grad-Positions/dev/.github/scripts/listings.json",
		"vanshb03":     "https://raw.githubusercontent.com/vanshb03/New-Grad-2027/dev/.github/scripts/listings.json",
	}
	for name, url := range feeds {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			jobs, err := NewGitHubFeed(client, name, url, 5*time.Minute).Fetch(ctx)
			if err != nil {
				t.Fatalf("live fetch failed: %v", err)
			}
			if len(jobs) == 0 {
				t.Fatal("0 active jobs in the whole aggregator, suspicious")
			}
			t.Logf("%d active jobs, first: %q at %s", len(jobs), jobs[0].Title, jobs[0].Company)
		})
	}
}

func TestSmokeAshby(t *testing.T) {
	client := NewHTTPClient()
	for _, slug := range []string{"ramp", "linear"} {
		t.Run(slug, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			jobs, err := NewAshby(client, slug).Fetch(ctx)
			if err != nil {
				t.Fatalf("live fetch failed: %v", err)
			}
			if len(jobs) == 0 {
				t.Fatal("0 jobs returned, suspicious for this company")
			}
			t.Logf("%d jobs, first: %q (%s)", len(jobs), jobs[0].Title, jobs[0].Location)
		})
	}
}

func TestSmokeLever(t *testing.T) {
	client := NewHTTPClient()
	for _, slug := range []string{"spotify"} {
		t.Run(slug, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			jobs, err := NewLever(client, slug).Fetch(ctx)
			if err != nil {
				t.Fatalf("live fetch failed: %v", err)
			}
			if len(jobs) == 0 {
				t.Fatal("0 jobs returned, suspicious for this company")
			}
			t.Logf("%d jobs, first: %q (%s)", len(jobs), jobs[0].Title, jobs[0].Location)
		})
	}
}

func TestSmokeGreenhouse(t *testing.T) {
	client := NewHTTPClient()
	for _, slug := range []string{"stripe", "anthropic"} {
		t.Run(slug, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			jobs, err := NewGreenhouse(client, slug).Fetch(ctx)
			if err != nil {
				t.Fatalf("live fetch failed: %v", err)
			}
			// zero jobs from stripe or anthropic means something is off
			// (bad slug, api change), treat it as a failure
			if len(jobs) == 0 {
				t.Fatal("0 jobs returned, suspicious for this company")
			}
			t.Logf("%d jobs, first: %q (%s)", len(jobs), jobs[0].Title, jobs[0].Location)
		})
	}
}
