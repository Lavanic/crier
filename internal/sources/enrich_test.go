package sources

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// fakeCache is the store's TitleCache without a database behind it
type fakeCache struct {
	mu     sync.Mutex
	titles map[string][2]string
	reads  int
	writes int
}

func newFakeCache() *fakeCache {
	return &fakeCache{titles: map[string][2]string{}}
}

func (c *fakeCache) LookupTitle(url string) (string, string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	v, ok := c.titles[url]
	return v[0], v[1], ok, nil
}

func (c *fakeCache) SaveTitle(url, title, location string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes++
	c.titles[url] = [2]string{title, location}
	return nil
}

// one server for all three boards, so a test proves a link hit the
// right api by the path it asked for
func fakeBoards(t *testing.T, hits *int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/boards/chicagotradingcampus/jobs/4716937005", func(w http.ResponseWriter, r *http.Request) {
		*hits++
		fmt.Fprint(w, `{"id":4716937005,"title":"Associate Engineer","location":{"name":"Chicago, IL"}}`)
	})
	mux.HandleFunc("/v0/postings/anduril/abc-123", func(w http.ResponseWriter, r *http.Request) {
		*hits++
		fmt.Fprint(w, `{"id":"abc-123","text":"Software Engineer, New Grad","categories":{"location":"Costa Mesa, CA"}}`)
	})
	mux.HandleFunc("/posting-api/job-board/blissway", func(w http.ResponseWriter, r *http.Request) {
		*hits++
		fmt.Fprint(w, `{"jobs":[
			{"id":"other-id","title":"Senior Backend","location":"Denver, CO","isListed":true},
			{"id":"51d6d839","title":"Embedded Systems Engineer New Grad","location":"Denver, CO","isListed":true}
		]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestEnricher(srv *httptest.Server, cache TitleCache) *Enricher {
	e := NewEnricher(cache)
	e.greenhouseBase = srv.URL
	e.leverBase = srv.URL
	e.ashbyBase = srv.URL
	return e
}

// these three arrive with no title, and without a lookup there'd be
// nothing for the filter to judge them on at all
func TestEnricherFillsTitlesForBareATSLinks(t *testing.T) {
	hits := 0
	srv := fakeBoards(t, &hits)
	cache := newFakeCache()

	jobs := []Job{
		{URL: "https://job-boards.greenhouse.io/chicagotradingcampus/jobs/4716937005"},
		{URL: "https://jobs.lever.co/anduril/abc-123"},
		{URL: "https://jobs.ashbyhq.com/blissway/51d6d839/application"},
		// already has a title off the url slug, must not be looked up
		{URL: "https://careers.peak6.com/jobs/x/software-engineer-new-grad/JR1", Title: "Software Engineer New Grad"},
		// no board we can ask, stays untitled and that's fine
		{URL: "https://careers.unknownco.com/apply/12345"},
	}
	newTestEnricher(srv, cache).Fill(context.Background(), jobs)

	want := []struct{ title, location string }{
		{"Associate Engineer", "Chicago, IL"},
		{"Software Engineer, New Grad", "Costa Mesa, CA"},
		{"Embedded Systems Engineer New Grad", "Denver, CO"},
		{"Software Engineer New Grad", ""},
		{"", ""},
	}
	for i, w := range want {
		if jobs[i].Title != w.title {
			t.Errorf("job %d title = %q, want %q", i, jobs[i].Title, w.title)
		}
		if jobs[i].Location != w.location {
			t.Errorf("job %d location = %q, want %q", i, jobs[i].Location, w.location)
		}
	}
	if hits != 3 {
		t.Errorf("made %d board requests, want 3", hits)
	}
}

// stories stay up 24h at ~5 min polls, so without the cache that's
// ~288 identical questions per link per day
func TestEnricherUsesTheCacheOnTheSecondPass(t *testing.T) {
	hits := 0
	srv := fakeBoards(t, &hits)
	cache := newFakeCache()
	e := newTestEnricher(srv, cache)

	link := "https://job-boards.greenhouse.io/chicagotradingcampus/jobs/4716937005"
	for range 5 {
		jobs := []Job{{URL: link}}
		e.Fill(context.Background(), jobs)
		if jobs[0].Title != "Associate Engineer" {
			t.Fatalf("title = %q on a repeat poll", jobs[0].Title)
		}
	}
	if hits != 1 {
		t.Errorf("hit the board %d times across 5 polls, want 1", hits)
	}
	if cache.writes != 1 {
		t.Errorf("wrote the cache %d times, want 1", cache.writes)
	}
}

// a 404 is gone for good and worth remembering. a 500 is not, and
// caching it would blind us for a week
func TestEnricherOnlyCachesDefiniteAnswers(t *testing.T) {
	for _, tt := range []struct {
		name       string
		code       int
		wantWrites int
	}{
		{"posting deleted", http.StatusNotFound, 1},
		{"board having a bad day", http.StatusInternalServerError, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.code)
			}))
			defer srv.Close()

			cache := newFakeCache()
			jobs := []Job{{URL: "https://job-boards.greenhouse.io/acme/jobs/1"}}
			newTestEnricher(srv, cache).Fill(context.Background(), jobs)

			if jobs[0].Title != "" {
				t.Errorf("title = %q, want it left empty", jobs[0].Title)
			}
			if cache.writes != tt.wantWrites {
				t.Errorf("cache writes = %d, want %d", cache.writes, tt.wantWrites)
			}
		})
	}
}

// a big story dump must not turn one tick into dozens of board calls
func TestEnricherRespectsItsBudget(t *testing.T) {
	hits := 0
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		fmt.Fprint(w, `{"id":1,"title":"Software Engineer I","location":{"name":"NYC"}}`)
	}))
	defer srv.Close()

	jobs := make([]Job, 20)
	for i := range jobs {
		jobs[i] = Job{URL: fmt.Sprintf("https://job-boards.greenhouse.io/acme/jobs/%d", i)}
	}
	newTestEnricher(srv, nil).Fill(context.Background(), jobs)

	if hits != maxEnrichPerFetch {
		t.Errorf("made %d requests for 20 links, want the %d budget", hits, maxEnrichPerFetch)
	}
}

// nil cache is the test and dry-run path, it must not panic
func TestEnricherWorksWithoutACache(t *testing.T) {
	hits := 0
	srv := fakeBoards(t, &hits)
	jobs := []Job{{URL: "https://job-boards.greenhouse.io/chicagotradingcampus/jobs/4716937005"}}
	newTestEnricher(srv, nil).Fill(context.Background(), jobs)
	if jobs[0].Title != "Associate Engineer" {
		t.Errorf("title = %q", jobs[0].Title)
	}
}

// the shapes real careers pages actually serve, one row per site that
// taught us something
func TestPickTitle(t *testing.T) {
	tests := []struct{ name, og, tag, want string }{
		{"aqr, both clean and identical",
			"2027 Quantitative Prediction Markets Research Summer Analyst",
			"2027 Quantitative Prediction Markets Research Summer Analyst",
			"2027 Quantitative Prediction Markets Research Summer Analyst"},
		// the reason we can't just always trust og:title
		{"deshaw puts the company in og and the role in title",
			"The D. E. Shaw Group",
			"Systems Engineering Intern (New York) - Summer 2027 | The D. E. Shaw Group",
			"Systems Engineering Intern (New York) - Summer 2027"},
		{"uniswap, og is the cleaner one",
			"Software Engineer - Early Career",
			"Software Engineer - Early Career @ Uniswap Labs",
			"Software Engineer - Early Career"},
		{"tiktok, the pm internship that used to leak",
			"Product Manager Intern (Signal and Identity Product) - 2027 Summer",
			"Product Manager Intern (Signal and Identity Product) - 2027 Summer",
			"Product Manager Intern (Signal and Identity Product) - 2027 Summer"},
		{"no og tag at all", "", "Software Engineer, New Grad | Acme", "Software Engineer, New Grad"},
		{"no title tag at all", "Software Engineer, New Grad", "", "Software Engineer, New Grad"},
		// a long tail is part of the role, not a site name
		{"long tail after the pipe is kept",
			"", "Software Engineer - Infrastructure | Platform Reliability And Observability Team",
			"Software Engineer - Infrastructure | Platform Reliability And Observability Team"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pickTitle(clean(tt.og), clean(tt.tag)); got != tt.want {
				t.Errorf("pickTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPageTitle(t *testing.T) {
	// apple's real markup: entities in the title, content before property
	const page = `<html><head>
		<meta name="description" content="jobs">
		<meta content="Software Engineer, IS&amp;T Early Career Opportunities" property="og:title"/>
		<title>Software Engineer, IS&amp;T   Early Career Opportunities - Careers</title>
	</head><body>...</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, page)
	}))
	defer srv.Close()

	got, err := NewEnricher(nil).pageTitle(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	// entity decoded, so the filter sees the same "IS&T" a human would
	if got != "Software Engineer, IS&T Early Career Opportunities" {
		t.Errorf("got %q", got)
	}
}

// a title that says nothing is worse than no title, it would sail past
// the excludes and alert on whatever the page happened to be
func TestPageTitleRejectsGenericPages(t *testing.T) {
	for _, body := range []string{
		`<html><head><title>Careers</title></head></html>`,
		`<html><head><title>Job Search</title></head></html>`,
		`<html><head></head><body>no title at all</body></html>`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, body)
		}))
		if _, err := NewEnricher(nil).pageTitle(context.Background(), srv.URL); err == nil {
			t.Errorf("accepted a junk title from %q", body)
		}
		srv.Close()
	}
}

// ibm answers 202 with an empty body to non-browsers. that link can
// never be titled, and must not become an alert
func TestPageTitleOnBotWall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	jobs := []Job{{URL: srv.URL + "/en_US/careers/JobDetail?jobId=128497"}}
	NewEnricher(nil).Fill(context.Background(), jobs)
	if jobs[0].Title != "" {
		t.Errorf("title = %q, want it left empty so the filter drops it", jobs[0].Title)
	}
}
