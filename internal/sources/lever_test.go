package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// trimmed copy of a real lever response, note it's a bare array
const leverSample = `[
	{
		"id": "a1b2c3d4-1111-2222-3333-444455556666",
		"text": "Backend Engineer, Early Career",
		"hostedUrl": "https://jobs.lever.co/spotify/a1b2c3d4",
		"applyUrl": "https://jobs.lever.co/spotify/a1b2c3d4/apply",
		"categories": {
			"location": "New York, NY",
			"team": "Platform",
			"commitment": "Full-time"
		},
		"createdAt": 1751500000000
	}
]`

func TestLeverFetchMapsJobs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/postings/spotify" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.URL.Query().Get("mode") != "json" {
			t.Errorf("missing mode=json query param, got %q", r.URL.RawQuery)
		}
		w.Write([]byte(leverSample))
	}))
	defer srv.Close()

	l := NewLever(NewHTTPClient(), "spotify")
	l.baseURL = srv.URL

	jobs, err := l.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want 1", len(jobs))
	}
	j := jobs[0]
	if j.Title != "Backend Engineer, Early Career" {
		t.Errorf("Title = %q", j.Title)
	}
	if j.Location != "New York, NY" {
		t.Errorf("Location = %q", j.Location)
	}
	// applyUrl preferred over hostedUrl, lands on the actual form
	if j.URL != "https://jobs.lever.co/spotify/a1b2c3d4/apply" {
		t.Errorf("URL = %q, want the applyUrl", j.URL)
	}
	if j.DedupKey() != "lever:spotify:a1b2c3d4-1111-2222-3333-444455556666" {
		t.Errorf("DedupKey = %q", j.DedupKey())
	}
}
