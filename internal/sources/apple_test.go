package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// trimmed copy of the real page. the first jobSummary contains an
// escaped quote right before a paren, the exact sequence that would
// fool a lazy regex into cutting the blob short
const appleSample = `<html><script>window.__staticRouterHydrationData = JSON.parse("{\"loaderData\":{\"routes/search\":{\"searchResults\":[{\"positionId\":\"200613768\",\"postingTitle\":\"Software Engineer - Early Career\",\"transformedPostingTitle\":\"software-engineer-early-career\",\"jobSummary\":\"they said \\\"ship it\\\") and we did\",\"locations\":[{\"name\":\"Cupertino\"},{\"name\":\"Austin\"}]},{\"positionId\":\"200999999\",\"postingTitle\":\"AIML - Engineer I\",\"transformedPostingTitle\":\"aiml-engineer-i\",\"locations\":[{\"name\":\"Seattle\"}]}],\"totalRecords\":2}}}");</script></html>`

func TestAppleJobsMapsResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(appleSample))
	}))
	defer srv.Close()

	a := NewAppleJobs(NewHTTPClient(), "swe-us-newest", srv.URL, 5*time.Minute)
	jobs, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(jobs))
	}
	j := jobs[0]
	if j.Source != "apple" || j.Company != "Apple" || j.JobID != "200613768" {
		t.Errorf("job = %+v", j)
	}
	if j.Location != "Cupertino | Austin" {
		t.Errorf("Location = %q", j.Location)
	}
	if j.URL != "https://jobs.apple.com/en-us/details/200613768/software-engineer-early-career" {
		t.Errorf("URL = %q", j.URL)
	}
	if a.Name() != "apple:swe-us-newest" {
		t.Errorf("Name = %q", a.Name())
	}
}

func TestAppleJobsLayoutDrift(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html><body>no hydration here</body></html>"))
	}))
	defer srv.Close()

	a := NewAppleJobs(NewHTTPClient(), "x", srv.URL, 0)
	if _, err := a.Fetch(context.Background()); err == nil {
		t.Fatal("expected an error for a page without the hydration blob")
	}
}

func TestJSStringAfter(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		want   string
		wantOK bool
	}{
		{"plain", `x = JSON.parse("hello");`, `"hello"`, true},
		// the escaped quote must not terminate the scan early
		{"escaped quote", `x = JSON.parse("say \"hi\") ok");`, `"say \"hi\") ok"`, true},
		{"escaped backslash then quote", `x = JSON.parse("a\\\"b\\") tail`, `"a\\\"b\\"`, true},
		{"no marker", `nothing here`, "", false},
		{"unterminated", `x = JSON.parse("runs off the end`, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := jsStringAfter(tt.s, "JSON.parse")
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("jsStringAfter = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
