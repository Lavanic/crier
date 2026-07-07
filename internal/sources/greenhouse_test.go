package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// trimmed-down copy of a real greenhouse response
const greenhouseSample = `{
	"jobs": [
		{
			"id": 4400001,
			"title": "Software Engineer, New Grad",
			"absolute_url": "https://stripe.com/jobs/listing/4400001",
			"location": {"name": "San Francisco, CA"},
			"updated_at": "2026-07-01T00:00:00-04:00",
			"some_field_we_dont_care_about": true
		},
		{
			"id": 4400002,
			"title": "Staff Engineer, Payments",
			"absolute_url": "https://stripe.com/jobs/listing/4400002",
			"location": {"name": "Remote"}
		}
	],
	"meta": {"total": 2}
}`

func TestGreenhouseFetchMapsJobs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// make sure the client builds the right path
		if r.URL.Path != "/v1/boards/stripe/jobs" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Write([]byte(greenhouseSample))
	}))
	defer srv.Close()

	g := NewGreenhouse(NewHTTPClient(), "stripe")
	g.baseURL = srv.URL

	jobs, err := g.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(jobs))
	}
	first := jobs[0]
	if first.Source != "greenhouse" || first.Company != "stripe" {
		t.Errorf("source/company = %q/%q", first.Source, first.Company)
	}
	if first.JobID != "4400001" {
		t.Errorf("JobID = %q, want the numeric id as a string", first.JobID)
	}
	if first.Title != "Software Engineer, New Grad" {
		t.Errorf("Title = %q", first.Title)
	}
	if first.Location != "San Francisco, CA" {
		t.Errorf("Location = %q", first.Location)
	}
	if first.DedupKey() != "greenhouse:stripe:4400001" {
		t.Errorf("DedupKey = %q", first.DedupKey())
	}
}
