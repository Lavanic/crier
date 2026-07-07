package sources

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// ashby's public job posting api:
//   GET https://api.ashbyhq.com/posting-api/job-board/{slug}
// community-observed soft limit is ~100 req/min so our once-per-tick
// per-board rate is nowhere close.
// we skip ?includeCompensation=true, same deal as greenhouse's
// content param, we don't alert on comp so why fetch it

type Ashby struct {
	client  *http.Client
	slug    string
	baseURL string
}

var _ Source = (*Ashby)(nil)

func NewAshby(client *http.Client, slug string) *Ashby {
	return &Ashby{
		client:  client,
		slug:    slug,
		baseURL: "https://api.ashbyhq.com",
	}
}

func (a *Ashby) Name() string { return "ashby:" + a.slug }

// poll every tick
func (a *Ashby) MinInterval() time.Duration { return 0 }

// wire format. location is a plain string here, and jobs that are
// unlisted still show up in some tenants so we check isListed
type ashbyResponse struct {
	Jobs []struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Location string `json:"location"`
		JobURL   string `json:"jobUrl"`
		IsListed bool   `json:"isListed"`
	} `json:"jobs"`
}

func (a *Ashby) Fetch(ctx context.Context) ([]Job, error) {
	url := fmt.Sprintf("%s/posting-api/job-board/%s", a.baseURL, a.slug)
	var resp ashbyResponse
	if err := getJSON(ctx, a.client, url, &resp); err != nil {
		return nil, fmt.Errorf("ashby %s: %w", a.slug, err)
	}
	jobs := make([]Job, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		if !j.IsListed {
			continue
		}
		jobs = append(jobs, Job{
			Source:   "ashby",
			Company:  a.slug,
			JobID:    j.ID,
			Title:    j.Title,
			Location: j.Location,
			URL:      j.JobURL,
		})
	}
	return jobs, nil
}
