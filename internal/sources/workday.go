package sources

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Workday is the generic client for any myworkdayjobs tenant (nvidia,
// and any other later). the public site has no api, but the react app
// it serves talks to a json endpoint at /wday/cxs/{tenant}/{site}/jobs
// via POST. one searchText shrinks the result set to page 1: workday
// caps limit at 20 (asking for more returns an empty array, no error)
// and throttles aggressive paging, so we poll the 20 newest matches and
// let sqlite id-diffing spot the new reqs. postedOn is only relative
// ("Posted 3 Days Ago") which is why we never lean on a timestamp here

type Workday struct {
	client      *http.Client
	name        string
	company     string
	url         string // the cxs /jobs endpoint
	search      string
	minInterval time.Duration
}

var _ Source = (*Workday)(nil)

func NewWorkday(client *http.Client, name, company, cxsURL, search string, minInterval time.Duration) *Workday {
	return &Workday{client: client, name: name, company: company, url: cxsURL, search: search, minInterval: minInterval}
}

func (w *Workday) Name() string { return "workday:" + w.name }

func (w *Workday) MinInterval() time.Duration { return w.minInterval }

func (w *Workday) Fetch(ctx context.Context) ([]Job, error) {
	body := map[string]any{
		"appliedFacets": map[string]any{},
		"limit":         20, // anything over 20 comes back empty
		"offset":        0,
		"searchText":    w.search,
	}
	var resp struct {
		JobPostings []struct {
			Title         string   `json:"title"`
			ExternalPath  string   `json:"externalPath"`
			LocationsText string   `json:"locationsText"`
			BulletFields  []string `json:"bulletFields"`
		} `json:"jobPostings"`
	}
	if err := postJSON(ctx, w.client, w.url, body, &resp); err != nil {
		return nil, fmt.Errorf("workday %s: %w", w.name, err)
	}

	base, err := workdayBase(w.url)
	if err != nil {
		return nil, fmt.Errorf("workday %s: %w", w.name, err)
	}

	jobs := make([]Job, 0, len(resp.JobPostings))
	for _, p := range resp.JobPostings {
		if p.Title == "" || p.ExternalPath == "" {
			continue
		}
		// bulletFields[0] is the JR req id, the stable dedup key and
		// what the cross-post logic matches against workday feed echoes
		id := p.ExternalPath
		if len(p.BulletFields) > 0 && p.BulletFields[0] != "" {
			id = p.BulletFields[0]
		}
		jobs = append(jobs, Job{
			Source:   "workday",
			Company:  w.company,
			JobID:    id,
			Title:    p.Title,
			Location: workdayLocation(p.LocationsText, p.ExternalPath),
			URL:      base + p.ExternalPath,
		})
	}
	// a broken layout returns postings we can't parse; an honestly
	// empty search just returns nothing, so only the former is an error
	if len(resp.JobPostings) > 0 && len(jobs) == 0 {
		return nil, fmt.Errorf("workday %s: all %d postings missing title/path, layout changed?", w.name, len(resp.JobPostings))
	}
	return jobs, nil
}

// workdayBase turns a cxs endpoint into the public site root that
// externalPath hangs off of:
//
//	https://host/wday/cxs/{tenant}/{site}/jobs  ->  https://host/{site}
func workdayBase(cxsURL string) (string, error) {
	u, err := url.Parse(cxsURL)
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 5 || parts[0] != "wday" || parts[1] != "cxs" {
		return "", fmt.Errorf("not a workday cxs url: %q", cxsURL)
	}
	site := parts[len(parts)-2] // the segment right before "jobs"
	return u.Scheme + "://" + u.Host + "/" + site, nil
}

// workday summarizes multi-office roles as "2 Locations" instead of
// listing them, which the us-only gate can't read
var workdaySummary = regexp.MustCompile(`^\d+ Locations?$`)

// workdayLocation prefers the real locationsText, but when workday
// hides the cities behind a "2 Locations" summary it recovers the
// primary office from the path (/job/US-CA-Santa-Clara/Title_JR...)
// so at least single-office foreign roles still hit the location gate
func workdayLocation(text, path string) string {
	if text != "" && !workdaySummary.MatchString(text) {
		return text
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "job" {
		return strings.ReplaceAll(parts[1], "-", " ")
	}
	return text
}
