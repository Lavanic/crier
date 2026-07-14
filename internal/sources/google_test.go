package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// trimmed copy of the real page: a ds:0 decoy blob first, then ds:1
// with the positional job records. third record is truncated and must
// be skipped, second record's empty company falls back to Google
const googleSample = `<html><script>AF_initDataCallback({key: 'ds:0', hash: '1', data:[1], sideChannel: {}});</script>
<script>AF_initDataCallback({key: 'ds:1', hash: '2', data:[[["138156162599002822","Software Engineer II, Early Career, Google Cloud AI Career Catalyst Program","https://signin.example",null,null,"projects/x",null,"Google","en-US",[["Sunnyvale, CA, USA",["Sunnyvale, CA, USA"],"Sunnyvale",null,"CA","US"]]],["81917268119691974","Software Engineer II, Fitbit",null,null,null,null,null,"",null,[["Boulder, CO, USA",null,"Boulder",null,"CO","US"],["New York, NY, USA",null,"New York",null,"NY","US"]]],["truncated-record"]],null,null,null], sideChannel: {}});</script></html>`

func TestGoogleCareersMapsRecords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(googleSample))
	}))
	defer srv.Close()

	g := NewGoogleCareers(NewHTTPClient(), "swe-early-us", srv.URL, 5*time.Minute)
	jobs, err := g.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2 (truncated record skipped)", len(jobs))
	}
	j := jobs[0]
	if j.Source != "google" {
		t.Errorf("Source = %q, want bare site name so cross-query dedup works", j.Source)
	}
	if j.JobID != "138156162599002822" || j.Company != "Google" {
		t.Errorf("job = %+v", j)
	}
	if j.Location != "Sunnyvale, CA, USA" {
		t.Errorf("Location = %q", j.Location)
	}
	if !strings.HasSuffix(j.URL, "/jobs/results/138156162599002822") {
		t.Errorf("URL = %q", j.URL)
	}
	if jobs[1].Company != "Google" {
		t.Errorf("empty company should fall back to Google, got %q", jobs[1].Company)
	}
	if jobs[1].Location != "Boulder, CO, USA | New York, NY, USA" {
		t.Errorf("multi location = %q", jobs[1].Location)
	}
	if g.Name() != "google:swe-early-us" {
		t.Errorf("Name = %q", g.Name())
	}
}

func TestGoogleCareersLayoutDrift(t *testing.T) {
	// a page without the ds:1 blob means google changed the layout,
	// that has to be a loud per-tick error, not silently 0 jobs
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html><body>totally different page</body></html>"))
	}))
	defer srv.Close()

	g := NewGoogleCareers(NewHTTPClient(), "x", srv.URL, 0)
	if _, err := g.Fetch(context.Background()); err == nil {
		t.Fatal("expected an error for a page without the data blob")
	}
}
