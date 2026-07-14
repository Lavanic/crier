package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// apple's jobs site embeds its search results as json inside a
// JSON.parse("...") call in the page html, plain GET works. sort=newest
// plus the us location filter keeps page 1 = the 20 newest us postings

type AppleJobs struct {
	client      *http.Client
	name        string
	url         string
	minInterval time.Duration
}

var _ Source = (*AppleJobs)(nil)

func NewAppleJobs(client *http.Client, name, url string, minInterval time.Duration) *AppleJobs {
	return &AppleJobs{client: client, name: name, url: url, minInterval: minInterval}
}

func (a *AppleJobs) Name() string { return "apple:" + a.name }

func (a *AppleJobs) MinInterval() time.Duration { return a.minInterval }

const appleMarker = "window.__staticRouterHydrationData"

func (a *AppleJobs) Fetch(ctx context.Context) ([]Job, error) {
	page, err := getHTML(ctx, a.client, a.url)
	if err != nil {
		return nil, fmt.Errorf("apple %s: %w", a.name, err)
	}
	blob, ok := jsStringAfter(page, appleMarker)
	if !ok {
		return nil, fmt.Errorf("apple %s: no hydration blob in page, layout changed?", a.name)
	}
	// the blob is a json-encoded STRING whose content is the real json
	var inner string
	if err := json.Unmarshal([]byte(blob), &inner); err != nil {
		return nil, fmt.Errorf("apple %s: hydration string not decodable: %w", a.name, err)
	}
	var hydration struct {
		LoaderData map[string]json.RawMessage `json:"loaderData"`
	}
	if err := json.Unmarshal([]byte(inner), &hydration); err != nil {
		return nil, fmt.Errorf("apple %s: hydration json not decodable: %w", a.name, err)
	}

	// route keys come from their router config, so find whichever
	// route loaded searchResults instead of hardcoding "search"
	var res struct {
		SearchResults []struct {
			PositionID   string `json:"positionId"`
			PostingTitle string `json:"postingTitle"`
			Slug         string `json:"transformedPostingTitle"`
			Locations    []struct {
				Name string `json:"name"`
			} `json:"locations"`
		} `json:"searchResults"`
	}
	for _, raw := range hydration.LoaderData {
		if json.Unmarshal(raw, &res) == nil && len(res.SearchResults) > 0 {
			break
		}
	}
	if len(res.SearchResults) == 0 {
		return nil, fmt.Errorf("apple %s: no searchResults in hydration data, layout changed?", a.name)
	}

	jobs := make([]Job, 0, len(res.SearchResults))
	for _, r := range res.SearchResults {
		if r.PositionID == "" || r.PostingTitle == "" {
			continue
		}
		var locs []string
		for _, l := range r.Locations {
			if l.Name != "" {
				locs = append(locs, l.Name)
			}
		}
		jobs = append(jobs, Job{
			Source:   "apple",
			Company:  "Apple",
			JobID:    r.PositionID,
			Title:    r.PostingTitle,
			Location: strings.Join(locs, " | "),
			URL:      "https://jobs.apple.com/en-us/details/" + r.PositionID + "/" + r.Slug,
		})
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("apple %s: all results missing id/title, layout changed?", a.name)
	}
	return jobs, nil
}

// jsStringAfter returns the first double-quoted js string literal
// after marker, quotes included. scans bytes instead of regexing
// because the blob itself is full of escaped quotes
func jsStringAfter(s, marker string) (string, bool) {
	i := strings.Index(s, marker)
	if i < 0 {
		return "", false
	}
	start := strings.IndexByte(s[i:], '"')
	if start < 0 {
		return "", false
	}
	start += i
	for j := start + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++ // escaped char, skip it
		case '"':
			return s[start : j+1], true
		}
	}
	return "", false
}
