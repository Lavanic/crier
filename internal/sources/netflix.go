package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// netflix runs on eightfold, not a public ats board. its careers api
// at explore.jobs.netflix.net returns a clean positions json array to
// a plain GET, sort_by=timestamp keeps page 1 = the newest postings

type NetflixJobs struct {
	client      *http.Client
	name        string
	url         string
	minInterval time.Duration
}

var _ Source = (*NetflixJobs)(nil)

func NewNetflixJobs(client *http.Client, name, url string, minInterval time.Duration) *NetflixJobs {
	return &NetflixJobs{client: client, name: name, url: url, minInterval: minInterval}
}

func (n *NetflixJobs) Name() string { return "netflix:" + n.name }

func (n *NetflixJobs) MinInterval() time.Duration { return n.minInterval }

func (n *NetflixJobs) Fetch(ctx context.Context) ([]Job, error) {
	var resp struct {
		Positions []struct {
			// id is numeric in the json, the url is built from it
			ID        json.Number `json:"id"`
			Name      string      `json:"name"`
			Location  string      `json:"location"`
			Locations []string    `json:"locations"`
			AtsJobID  string      `json:"ats_job_id"`
			URL       string      `json:"canonicalPositionUrl"`
		} `json:"positions"`
	}
	if err := getJSON(ctx, n.client, n.url, &resp); err != nil {
		return nil, fmt.Errorf("netflix %s: %w", n.name, err)
	}

	jobs := make([]Job, 0, len(resp.Positions))
	for _, p := range resp.Positions {
		if p.Name == "" || p.URL == "" {
			continue
		}
		// ats_job_id is the stable req id, fall back to the numeric id
		// so a missing field never drops a posting
		id := p.AtsJobID
		if id == "" {
			id = p.ID.String()
		}
		loc := strings.Join(p.Locations, " | ")
		if loc == "" {
			loc = p.Location
		}
		jobs = append(jobs, Job{
			Source:   "netflix",
			Company:  "Netflix",
			JobID:    id,
			Title:    p.Name,
			Location: loc,
			URL:      p.URL,
		})
	}
	if len(resp.Positions) > 0 && len(jobs) == 0 {
		return nil, fmt.Errorf("netflix %s: all %d positions missing name/url, layout changed?", n.name, len(resp.Positions))
	}
	return jobs, nil
}
