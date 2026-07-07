package sources

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// lever's public postings api (v0 is the unauthenticated one):
//   GET https://api.lever.co/v0/postings/{slug}?mode=json
// unlike greenhouse the response is a bare json array, not an object,
// and job ids are uuids instead of numbers

type Lever struct {
	client  *http.Client
	slug    string
	baseURL string
}

var _ Source = (*Lever)(nil)

func NewLever(client *http.Client, slug string) *Lever {
	return &Lever{
		client:  client,
		slug:    slug,
		baseURL: "https://api.lever.co",
	}
}

func (l *Lever) Name() string { return "lever:" + l.slug }

// poll every tick
func (l *Lever) MinInterval() time.Duration { return 0 }

// wire format. "text" is what lever calls the job title. applyUrl is
// hostedUrl + /apply, the actual form instead of the description page
type leverPosting struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	HostedURL  string `json:"hostedUrl"`
	ApplyURL   string `json:"applyUrl"`
	Categories struct {
		Location string `json:"location"`
	} `json:"categories"`
}

func (l *Lever) Fetch(ctx context.Context) ([]Job, error) {
	url := fmt.Sprintf("%s/v0/postings/%s?mode=json", l.baseURL, l.slug)
	var postings []leverPosting
	if err := getJSON(ctx, l.client, url, &postings); err != nil {
		return nil, fmt.Errorf("lever %s: %w", l.slug, err)
	}
	jobs := make([]Job, 0, len(postings))
	for _, p := range postings {
		// empty ids all collide into one dedup key and silently eat
		// the whole board, treat schema drift as a hard error
		if p.ID == "" {
			return nil, fmt.Errorf("lever %s: posting with empty id, schema drift?", l.slug)
		}
		link := p.ApplyURL
		if link == "" {
			link = p.HostedURL
		}
		jobs = append(jobs, Job{
			Source:   "lever",
			Company:  l.slug,
			JobID:    p.ID,
			Title:    p.Text,
			Location: p.Categories.Location,
			URL:      link,
		})
	}
	return jobs, nil
}
