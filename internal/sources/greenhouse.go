package sources

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// greenhouse's public job board api, no auth needed:
//   GET https://boards-api.greenhouse.io/v1/boards/{slug}/jobs
// greenhouse says it's "heavily cached, no hard rate limits" so
// hitting it every 30s is fine.
// note we skip the ?content=true param on purpose, it inlines the full
// html description for every posting and we only alert on titles.
// smaller payload = faster tick

type Greenhouse struct {
	client *http.Client
	slug   string
	// unit tests point this at a fake local server
	baseURL string
}

// compile-time proof that *Greenhouse satisfies Source, if a method
// goes missing this line stops the build
var _ Source = (*Greenhouse)(nil)

func NewGreenhouse(client *http.Client, slug string) *Greenhouse {
	return &Greenhouse{
		client:  client,
		slug:    slug,
		baseURL: "https://boards-api.greenhouse.io",
	}
}

func (g *Greenhouse) Name() string { return "greenhouse:" + g.slug }

// poll every tick
func (g *Greenhouse) MinInterval() time.Duration { return 0 }

// the wire format, declaring only the fields we care about.
// everything else in the response gets ignored by the json decoder
type greenhouseResponse struct {
	Jobs []struct {
		ID       int64  `json:"id"`
		Title    string `json:"title"`
		URL      string `json:"absolute_url"`
		Location struct {
			Name string `json:"name"`
		} `json:"location"`
	} `json:"jobs"`
}

func (g *Greenhouse) Fetch(ctx context.Context) ([]Job, error) {
	url := fmt.Sprintf("%s/v1/boards/%s/jobs", g.baseURL, g.slug)
	var resp greenhouseResponse
	if err := getJSON(ctx, g.client, url, &resp); err != nil {
		return nil, fmt.Errorf("greenhouse %s: %w", g.slug, err)
	}
	jobs := make([]Job, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		jobs = append(jobs, Job{
			Source:   "greenhouse",
			Company:  g.slug,
			JobID:    strconv.FormatInt(j.ID, 10),
			Title:    j.Title,
			Location: j.Location.Name,
			URL:      j.URL,
		})
	}
	return jobs, nil
}
