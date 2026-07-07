package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// trimmed copy of the simplify listings.json shape. second listing is
// inactive (drop), third is missing active/is_visible entirely and
// should be kept anyway
const githubSample = `[
	{
		"id": "uuid-1",
		"company_name": "Google",
		"title": "Software Engineer, New Grad 2027",
		"url": "https://simplify.jobs/p/uuid-1",
		"locations": ["Mountain View, CA", "Kirkland, WA"],
		"active": true,
		"is_visible": true,
		"date_posted": 1751500000,
		"source": "Simplify"
	},
	{
		"id": "uuid-2",
		"company_name": "DeadCo",
		"title": "Closed Role",
		"url": "https://example.com",
		"locations": ["Nowhere"],
		"active": false,
		"is_visible": true
	},
	{
		"id": "uuid-3",
		"company_name": "SchemaDriftCo",
		"title": "Software Engineer I",
		"url": "https://example.com/3",
		"locations": ["Austin, TX"]
	}
]`

func TestGitHubFeedMapsAndFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(githubSample))
	}))
	defer srv.Close()

	f := NewGitHubFeed(NewHTTPClient(), "simplifyjobs", srv.URL, 5*time.Minute)

	jobs, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2 (inactive dropped, schema-drift kept)", len(jobs))
	}
	j := jobs[0]
	if j.Source != "github:simplifyjobs" {
		t.Errorf("Source = %q", j.Source)
	}
	if j.Company != "Google" {
		t.Errorf("Company = %q", j.Company)
	}
	if j.Location != "Mountain View, CA | Kirkland, WA" {
		t.Errorf("Location = %q, want joined list", j.Location)
	}
	if jobs[1].Company != "SchemaDriftCo" {
		t.Errorf("missing-flags listing should survive, got %q", jobs[1].Company)
	}
	if f.MinInterval() != 5*time.Minute {
		t.Errorf("MinInterval = %v", f.MinInterval())
	}
}
