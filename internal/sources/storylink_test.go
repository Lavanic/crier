package sources

import (
	"net/url"
	"testing"
)

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

// the peak6 link is copied from a real story dump, tracking params
// and all. the rest is the shape each board actually posts
func TestJobFromStoryLink(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantOK   bool
		company  string
		title    string
		location string
		url      string
	}{
		{
			name:     "peak6 through the instagram shim",
			raw:      "https://l.instagram.com/?u=https%3A%2F%2Fcareers.peak6.com%2Fjobs%2Fbusiness-operation-services%2Fchicago-illinois-united-states-of-america%2Ftrading-bootcamp-micro-internship-summer-2027%2FJR105057%3Futm_source%3Dlinkedin%26utm_medium%3Dreferral%26utm_campaign%3Dzero2sudo%26fbclid%3DPAcGRvZgRzcnRj&e=AUA24kGRCFTX3c9",
			wantOK:   true,
			company:  "peak6",
			title:    "Trading Bootcamp Micro Internship Summer 2027",
			location: "Chicago Illinois United States Of America",
			url:      "https://careers.peak6.com/jobs/business-operation-services/chicago-illinois-united-states-of-america/trading-bootcamp-micro-internship-summer-2027/JR105057",
		},
		{
			// greenhouse links carry no words at all, which is what the
			// title lookup exists for. company MUST be the
			// board slug so cross-post dedup lines up with our own copy
			name:    "greenhouse bare id",
			raw:     "https://job-boards.greenhouse.io/chicagotradingcampus/jobs/4716937005",
			wantOK:  true,
			company: "chicagotradingcampus",
			title:   "",
			url:     "https://job-boards.greenhouse.io/chicagotradingcampus/jobs/4716937005",
		},
		{
			name:    "lever uuid",
			raw:     "https://jobs.lever.co/mistral/f1234567-89ab-4cde-8f01-23456789abcd",
			wantOK:  true,
			company: "mistral",
			title:   "",
			url:     "https://jobs.lever.co/mistral/f1234567-89ab-4cde-8f01-23456789abcd",
		},
		{
			name:    "ashby keeps its embed param",
			raw:     "https://jobs.ashbyhq.com/blissway/51d6d839-9801-4436-bfc2-918bae428ed8/application?embed=true",
			wantOK:  true,
			company: "blissway",
			title:   "",
			url:     "https://jobs.ashbyhq.com/blissway/51d6d839-9801-4436-bfc2-918bae428ed8/application?embed=true",
		},
		{
			// req id glued on with an underscore, office in its own
			// segment right before the title
			name:     "workday tenant",
			raw:      "https://tamus.wd1.myworkdayjobs.com/System-wide_External/job/College-Station-TX/RSSI-Graduate-Research-Assistant_R-089229",
			wantOK:   true,
			company:  "tamus",
			title:    "RSSI Graduate Research Assistant",
			location: "College Station TX",
			url:      "https://tamus.wd1.myworkdayjobs.com/System-wide_External/job/College-Station-TX/RSSI-Graduate-Research-Assistant_R-089229",
		},
		{
			name:    "plain company careers page, trailing req number trimmed",
			raw:     "https://www.tesla.com/careers/search/job/software-engineer-new-grad-235049",
			wantOK:  true,
			company: "tesla",
			title:   "Software Engineer New Grad",
			url:     "https://www.tesla.com/careers/search/job/software-engineer-new-grad-235049",
		},
		{
			// a locale label in front of the domain isn't the company
			name:    "locale subdomain skipped",
			raw:     "https://us.mercer.com/careers/software-engineer-i",
			wantOK:  true,
			company: "mercer",
			title:   "Software Engineer I",
			url:     "https://us.mercer.com/careers/software-engineer-i",
		},
		{
			name:   "his linktree is not a job",
			raw:    "https://l.instagram.com/?u=https%3A%2F%2Flinktr.ee%2Fzero2sudo",
			wantOK: false,
		},
		{
			name:   "bare domain has no posting in it",
			raw:    "https://www.janestreet.com",
			wantOK: false,
		},
		{
			name:   "garbage",
			raw:    "not a url at all",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job, ok := jobFromStoryLink("zero2sudo", tt.raw)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (job %+v)", ok, tt.wantOK, job)
			}
			if !ok {
				return
			}
			if job.Company != tt.company {
				t.Errorf("company = %q, want %q", job.Company, tt.company)
			}
			if job.Title != tt.title {
				t.Errorf("title = %q, want %q", job.Title, tt.title)
			}
			if job.Location != tt.location {
				t.Errorf("location = %q, want %q", job.Location, tt.location)
			}
			if job.URL != tt.url {
				t.Errorf("url = %q, want %q", job.URL, tt.url)
			}
			if job.Source != "instagram:zero2sudo" {
				t.Errorf("source = %q", job.Source)
			}
			// the url IS the dedup id, a repost gets a new sticker id
			// but has to land on the same job
			if job.JobID != tt.url {
				t.Errorf("jobID = %q, want the canonical url", job.JobID)
			}
		})
	}
}

// three ways he might link the same posting, all have to collapse
// onto one dedup key or a repost re-alerts
func TestCanonicalJobURLStableAcrossReposts(t *testing.T) {
	want := "https://job-boards.greenhouse.io/celonis/jobs/7725788003"
	for _, raw := range []string{
		"https://job-boards.greenhouse.io/celonis/jobs/7725788003",
		"https://job-boards.greenhouse.io/celonis/jobs/7725788003/",
		"https://job-boards.greenhouse.io/celonis/jobs/7725788003?utm_source=instagram&utm_campaign=zero2sudo&fbclid=abc123#apply",
	} {
		if got := canonicalJobURL(raw); got != want {
			t.Errorf("canonicalJobURL(%q) = %q, want %q", raw, got, want)
		}
	}
}

// gh_jid names the posting, so it has to survive the tracking strip
func TestCanonicalJobURLKeepsMeaningfulParams(t *testing.T) {
	got := canonicalJobURL("https://job-boards.greenhouse.io/celonis/jobs/7725788003?gh_jid=7725788003&gh_src=abc&utm_medium=ig")
	want := "https://job-boards.greenhouse.io/celonis/jobs/7725788003?gh_jid=7725788003"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUnwrapStoryLink(t *testing.T) {
	tests := []struct{ raw, want string }{
		{
			"https://l.instagram.com/?u=https%3A%2F%2Fexample.com%2Fjobs%2F1&e=AUA",
			"https://example.com/jobs/1",
		},
		{
			// facebook occasionally wraps an already wrapped link
			"https://l.facebook.com/?u=https%3A%2F%2Fl.instagram.com%2F%3Fu%3Dhttps%253A%252F%252Fexample.com%252Fjobs%252F2",
			"https://example.com/jobs/2",
		},
		{
			"https://example.com/jobs/3",
			"https://example.com/jobs/3",
		},
	}
	for _, tt := range tests {
		if got := unwrapStoryLink(tt.raw); got != tt.want {
			t.Errorf("unwrapStoryLink(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestBoardSlugRecognizesTheBoardsWeAlreadyPoll(t *testing.T) {
	tests := []struct{ raw, kind, slug, jobID string }{
		{"https://job-boards.greenhouse.io/appian/jobs/7951022", "greenhouse", "appian", "7951022"},
		{"https://boards.greenhouse.io/stripe/jobs/4400001", "greenhouse", "stripe", "4400001"},
		{"https://boards.greenhouse.io/embed/job_app?for=ramp&token=555", "greenhouse", "ramp", "555"},
		{"https://jobs.lever.co/anduril/abc-123", "lever", "anduril", "abc-123"},
		{"https://jobs.ashbyhq.com/vanta/uuid-here/application", "ashby", "vanta", "uuid-here"},
		{"https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite/job/US-CA/x", "workday", "nvidia", ""},
		{"https://careers.peak6.com/jobs/x/y", "", "", ""},
	}
	for _, tt := range tests {
		u := mustParse(t, tt.raw)
		kind, slug, jobID := boardSlug(u)
		if kind != tt.kind || slug != tt.slug || jobID != tt.jobID {
			t.Errorf("boardSlug(%q) = %q/%q/%q, want %q/%q/%q",
				tt.raw, kind, slug, jobID, tt.kind, tt.slug, tt.jobID)
		}
	}
}

// the office segment must never win the title slot, it usually has
// more words in it than the real title
func TestLooksLikeLocation(t *testing.T) {
	yes := []string{
		"chicago-illinois-united-states-of-america",
		"College-Station-TX",
		"Ames-IA",
		"remote-united-states",
		"san-francisco-california",
	}
	no := []string{
		"trading-bootcamp-micro-internship-summer-2027",
		"software-engineer-new-grad",
		"business-operation-services",
		"RSSI-Graduate-Research-Assistant",
	}
	for _, s := range yes {
		if !looksLikeLocation(s) {
			t.Errorf("looksLikeLocation(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if looksLikeLocation(s) {
			t.Errorf("looksLikeLocation(%q) = true, want false", s)
		}
	}
}
