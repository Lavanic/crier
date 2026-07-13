package sources

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// the aggregator repos (SimplifyJobs/New-Grad-Positions and
// vanshb03/New-Grad-2027) both publish a listings.json built by the
// same scripts, so one client covers both. we fetch the raw file:
//   https://raw.githubusercontent.com/{repo}/dev/.github/scripts/listings.json
// github's cdn caches it ~5 min, which is why these feeds carry a
// MinInterval instead of being polled every tick.
// these cover big tech + everyone we can't reach directly

type GitHubFeed struct {
	client      *http.Client
	name        string
	url         string
	minInterval time.Duration
}

var _ Source = (*GitHubFeed)(nil)

func NewGitHubFeed(client *http.Client, name, url string, minInterval time.Duration) *GitHubFeed {
	return &GitHubFeed{client: client, name: name, url: url, minInterval: minInterval}
}

func (f *GitHubFeed) Name() string { return "github:" + f.name }

func (f *GitHubFeed) MinInterval() time.Duration { return f.minInterval }

// wire format. active/is_visible are *bool on purpose: this json is
// maintained by volunteers, if a field disappears from the schema one
// day a plain bool would decode as false and we'd silently drop every
// job in the feed. pointer stays nil when absent so we can treat
// "missing" as "keep it"
type githubListing struct {
	ID          string   `json:"id"`
	CompanyName string   `json:"company_name"`
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	Locations   []string `json:"locations"`
	Category    string   `json:"category"`
	Active      *bool    `json:"active"`
	IsVisible   *bool    `json:"is_visible"`
}

func (f *GitHubFeed) Fetch(ctx context.Context) ([]Job, error) {
	var listings []githubListing
	if err := getJSON(ctx, f.client, f.url, &listings); err != nil {
		return nil, fmt.Errorf("github feed %s: %w", f.name, err)
	}
	jobs := make([]Job, 0, len(listings))
	skipped := 0
	for _, l := range listings {
		// only drop when the feed explicitly says so
		if (l.Active != nil && !*l.Active) || (l.IsVisible != nil && !*l.IsVisible) {
			continue
		}
		// volunteer-maintained json: skip individual junk rows, but if
		// EVERY row lost its id the schema drifted, fail the feed
		if l.ID == "" || l.Title == "" {
			skipped++
			continue
		}
		jobs = append(jobs, Job{
			Source:   f.Name(),
			Company:  l.CompanyName,
			JobID:    l.ID,
			Title:    l.Title,
			Location: strings.Join(l.Locations, " | "),
			URL:      l.URL,
			Category: l.Category,
		})
	}
	if len(listings) > 0 && len(jobs) == 0 && skipped > 0 {
		return nil, fmt.Errorf("github feed %s: all %d rows missing id/title, schema drift?", f.name, skipped)
	}
	return jobs, nil
}
