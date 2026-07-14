package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// google runs its own careers site, no public board api. the search
// page at google.com/about/careers/applications/jobs/results embeds
// its results as json in an AF_initDataCallback script blob and
// renders fine to a plain GET. filtering happens server side via url
// params (q, target_level=EARLY, location, sort_by=date), so page 1
// is always the 20 newest matches for that query

type GoogleCareers struct {
	client      *http.Client
	name        string
	url         string
	minInterval time.Duration
}

var _ Source = (*GoogleCareers)(nil)

func NewGoogleCareers(client *http.Client, name, url string, minInterval time.Duration) *GoogleCareers {
	return &GoogleCareers{client: client, name: name, url: url, minInterval: minInterval}
}

func (g *GoogleCareers) Name() string { return "google:" + g.name }

func (g *GoogleCareers) MinInterval() time.Duration { return g.minInterval }

// the ds:1 blob holds the search results, sideChannel always follows
// the data array so the lazy match stops in the right place
var googleData = regexp.MustCompile(`(?s)AF_initDataCallback\(\{key: 'ds:1'.*?data:(\[.*?\]), sideChannel`)

func (g *GoogleCareers) Fetch(ctx context.Context) ([]Job, error) {
	page, err := getHTML(ctx, g.client, g.url)
	if err != nil {
		return nil, fmt.Errorf("google %s: %w", g.name, err)
	}
	m := googleData.FindStringSubmatch(page)
	if m == nil {
		return nil, fmt.Errorf("google %s: no ds:1 data blob in page, layout changed?", g.name)
	}
	// the blob is positional nested arrays, not objects. mapped by
	// hand from live pages: record[0]=id, [1]=title, [7]=company,
	// [9]=locations (each location's display string sits at [0])
	var outer []json.RawMessage
	if err := json.Unmarshal([]byte(m[1]), &outer); err != nil {
		return nil, fmt.Errorf("google %s: ds:1 blob not decodable: %w", g.name, err)
	}
	if len(outer) == 0 {
		return nil, fmt.Errorf("google %s: ds:1 blob is empty, layout changed?", g.name)
	}
	var recs [][]json.RawMessage
	if err := json.Unmarshal(outer[0], &recs); err != nil {
		return nil, fmt.Errorf("google %s: results array not decodable: %w", g.name, err)
	}

	jobs := make([]Job, 0, len(recs))
	for _, rec := range recs {
		if len(rec) < 10 {
			continue
		}
		var id, title, company string
		json.Unmarshal(rec[0], &id)
		json.Unmarshal(rec[1], &title)
		json.Unmarshal(rec[7], &company)
		if id == "" || title == "" {
			continue
		}
		if company == "" {
			// the site hosts alphabet subsidiaries too, [7] names them
			company = "Google"
		}
		var locRecs [][]json.RawMessage
		json.Unmarshal(rec[9], &locRecs)
		var locs []string
		for _, lr := range locRecs {
			var s string
			if len(lr) > 0 && json.Unmarshal(lr[0], &s) == nil && s != "" {
				locs = append(locs, s)
			}
		}
		jobs = append(jobs, Job{
			// Source is the bare site (not the query name) so the same
			// job found by two configured queries dedups to one alert
			Source:   "google",
			Company:  company,
			JobID:    id,
			Title:    title,
			Location: strings.Join(locs, " | "),
			// the slugless url form serves the posting directly
			URL: "https://www.google.com/about/careers/applications/jobs/results/" + id,
		})
	}
	if len(recs) > 0 && len(jobs) == 0 {
		return nil, fmt.Errorf("google %s: all %d records missing id/title, layout changed?", g.name, len(recs))
	}
	return jobs, nil
}
