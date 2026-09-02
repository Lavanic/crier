package main

import (
	"strings"
	"testing"
)

func TestSirenLookup(t *testing.T) {
	names := map[string]string{"wehrtyou": "HRT", "openai": "OpenAI"}
	siren := sirenSet([]string{"OpenAI", "Google", "HRT", "Hudson River Trading"})

	tests := []struct {
		company string
		want    bool
	}{
		{"openai", true},   // board slug, mapped by display_names
		{"wehrtyou", true}, // ugly slug that maps to HRT
		{"Google", true},   // feed display name
		{"GOOGLE", true},   // case must not matter
		{"Hudson River Trading", true},
		{"stripe", false},
		{"Googler Staffing LLC", false}, // exact match only, no substrings
	}
	for _, tt := range tests {
		got := siren[strings.ToLower(displayName(names, tt.company))]
		if got != tt.want {
			t.Errorf("siren lookup for %q = %v, want %v", tt.company, got, tt.want)
		}
	}
}

// a "new grad" title sirens on its own, no matter who posted it.
// every title here is real, straight out of the prod db
func TestNewGradSirens(t *testing.T) {
	names := map[string]string{"openai": "OpenAI"}
	siren := sirenSet([]string{"OpenAI"})

	tests := []struct {
		company string
		title   string
		want    bool
		why     string
	}{
		{"katalyst", "Agentic Engineer New Grad", true, "plain new grad, unlisted company"},
		{"jhuapl", "Applied Algorithms Engineer New Grad", true, "same"},
		{"acme", "New-Grad Software Engineer", true, "hyphenated"},
		{"acme", "Software Engineer, New Grads", true, "plural"},
		{"acme", "Software Engineer New Graduate", true, "spelled out"},
		{"openai", "Software Engineer", true, "priority company, no new grad"},
		{"openai", "Backend Engineer New Grad", true, "both, still one siren"},
		// tiktok and bytedance say "Graduate" for their new grad roles.
		// matching that bare word would siren university staff jobs, so
		// it stays out on purpose
		{"tiktok", "AI Engineer Graduate Level", false, "graduate alone is not new grad"},
		{"jhuapl", "Academic Graduate Appointee - Data Acquisition Software Development", false,
			"real university job that alerted, must stay a normal ping"},
		{"stripe", "Software Engineer II", false, "ordinary role, unlisted company"},
	}
	for _, tt := range tests {
		got := isSiren(siren, displayName(names, tt.company), tt.title)
		if got != tt.want {
			t.Errorf("isSiren(%q, %q) = %v, want %v (%s)",
				tt.company, tt.title, got, tt.want, tt.why)
		}
	}
}

// every case here is a real url from the prod db
func TestReqID(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		// workday: id after the last underscore
		{"workday plain",
			"https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/US-CA-SAN-DIEGO-SD1--8650-Balboa-Ave--SAN-ANTONIO-BLDG/Software-Engineer-I--Onsite-_01858576",
			"01858576"},
		// workday repost counter stripped, so the same req on a sibling
		// portal collides (cadence had R54894, R54894-1 and R54894-3)
		{"workday repost",
			"https://cadence.wd1.myworkdayjobs.com/University_Talent_NCG/job/Burlington-MA/Software-Engineer--New-College-Grad-2026--Undergrads-_R54894-3",
			"r54894"},
		// a long numeric tail is part of the id, not a repost counter
		{"req dash number kept",
			"https://ultra.wd3.myworkdayjobs.com/uiccareers/job/Huntsville-USA/Associate-Software-Engineer_REQ-12218",
			"req-12218"},
		{"req dash number repost",
			"https://ultra.wd3.myworkdayjobs.com/ultra-careers/job/Huntsville-USA/Associate-Software-Engineer_REQ-12218-1",
			"req-12218"},
		// greenhouse: numeric last segment, query string ignored
		{"greenhouse with gh_jid",
			"https://boards.greenhouse.io/andurilindustries/jobs/5166390007?gh_jid=5166390007",
			"5166390007"},
		{"greenhouse bare",
			"https://boards.greenhouse.io/andurilindustries/jobs/5166390007",
			"5166390007"},
		// trailing slash before the query
		{"uber trailing slash",
			"https://jobs.uber.com/en/jobs/160017/?_csid=BOBcQVO6jwRuNsBKbjoAZA",
			"160017"},
		// no digits anywhere useful: fall back to the whole url so
		// different jobs never collide on a segment like "job_app"
		{"greenhouse embed falls back",
			"https://boards.greenhouse.io/embed/job_app?token=8046879",
			"https://boards.greenhouse.io/embed/job_app?token=8046879"},
		// google's slugged and slugless url forms must collide, the
		// feeds use both and the direct source uses slugless
		{"google slugged",
			"https://www.google.com/about/careers/applications/jobs/results/143333237156913862-software-engineer-ii/",
			"143333237156913862"},
		{"google slugless",
			"https://www.google.com/about/careers/applications/jobs/results/143333237156913862",
			"143333237156913862"},
		// apple puts the position id one level above the title slug,
		// and simplify links the id-only form of the same posting
		{"apple id plus slug",
			"https://jobs.apple.com/en-us/details/200622968/swe-early-career-user-land-security?team=SFTWR",
			"200622968"},
		{"apple id only",
			"https://jobs.apple.com/en-us/details/200622968",
			"200622968"},
		// ashby's /{uuid}/application form keys on the uuid
		{"ashby uuid",
			"https://jobs.ashbyhq.com/evenup/41488eae-50a9-4ad3-b6e0-2fd28efb238e/application",
			"41488eae-50a9-4ad3-b6e0-2fd28efb238e"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reqID(tt.url); got != tt.want {
				t.Errorf("reqID(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestCrossPostKey(t *testing.T) {
	names := map[string]string{"andurilindustries": "Anduril"}

	// the anduril double-ping: greenhouse board first, then simplify
	// echoes the same req under a prettier name. keys must collide
	a := crossPostKey(names, "andurilindustries",
		"https://boards.greenhouse.io/andurilindustries/jobs/5167865007?gh_jid=5167865007")
	b := crossPostKey(names, "Anduril",
		"https://boards.greenhouse.io/andurilindustries/jobs/5167865007")
	if a != b {
		t.Errorf("anduril slug key %q != feed key %q", a, b)
	}

	// "The Boeing Company" on two workday portals of the same req
	c := crossPostKey(nil, "The Boeing Company",
		"https://boeing.wd1.myworkdayjobs.com/external_subsidiary/job/USA---Colorado-Springs-CO/DevSecOps-Software-Engineer_JR2026515911")
	d := crossPostKey(nil, "The Boeing Company",
		"https://boeing.wd1.myworkdayjobs.com/EXTERNAL_CAREERS/job/USA---Colorado-Springs-CO/DevSecOps-Software-Engineer_JR2026515911-1")
	if c != d {
		t.Errorf("boeing portal keys differ: %q vs %q", c, d)
	}

	// different reqs at the same company must NOT collide, rtx posts
	// distinct "Software Engineer 1" roles in different cities
	e := crossPostKey(nil, "RTX",
		"https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/X/Software-Engineer-I--Onsite-_01858576")
	f := crossPostKey(nil, "RTX",
		"https://globalhr.wd5.myworkdayjobs.com/rec_rtx_ext_gateway/job/Y/Software-Engineer-I--Onsite-_01858115")
	if e == f {
		t.Errorf("distinct rtx reqs collided on %q", e)
	}
}
