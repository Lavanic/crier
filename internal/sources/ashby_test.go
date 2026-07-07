package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// trimmed copy of a real ashby response, second job is unlisted
// and should get dropped
const ashbySample = `{
	"apiVersion": "1",
	"jobs": [
		{
			"id": "f0e1d2c3-aaaa-bbbb-cccc-ddddeeeeffff",
			"title": "Software Engineer - New Grad (2027)",
			"location": "New York",
			"isListed": true,
			"isRemote": false,
			"jobUrl": "https://jobs.ashbyhq.com/ramp/f0e1d2c3",
			"applyUrl": "https://jobs.ashbyhq.com/ramp/f0e1d2c3/application"
		},
		{
			"id": "00000000-1111-2222-3333-444444444444",
			"title": "Ghost Role",
			"location": "Nowhere",
			"isListed": false,
			"jobUrl": "https://jobs.ashbyhq.com/ramp/00000000"
		}
	]
}`

func TestAshbyFetchMapsAndFiltersUnlisted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/posting-api/job-board/ramp" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Write([]byte(ashbySample))
	}))
	defer srv.Close()

	a := NewAshby(NewHTTPClient(), "ramp")
	a.baseURL = srv.URL

	jobs, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want 1 (unlisted job should be dropped)", len(jobs))
	}
	j := jobs[0]
	if j.Title != "Software Engineer - New Grad (2027)" {
		t.Errorf("Title = %q", j.Title)
	}
	// applyUrl preferred over jobUrl, one less tap to the form
	if j.URL != "https://jobs.ashbyhq.com/ramp/f0e1d2c3/application" {
		t.Errorf("URL = %q, want the applyUrl", j.URL)
	}
	if j.DedupKey() != "ashby:ramp:f0e1d2c3-aaaa-bbbb-cccc-ddddeeeeffff" {
		t.Errorf("DedupKey = %q", j.DedupKey())
	}
}

func TestAshbyErrorsOnMissingIDs(t *testing.T) {
	// a renamed id field must fail the fetch, not quietly collapse
	// every posting into one dedup key
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jobs": [{"title": "Ghost", "isListed": true, "jobUrl": "https://x"}]}`))
	}))
	defer srv.Close()

	a := NewAshby(NewHTTPClient(), "ramp")
	a.baseURL = srv.URL
	if _, err := a.Fetch(context.Background()); err == nil {
		t.Fatal("expected schema-drift error for empty ids")
	}
}
