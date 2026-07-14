package main

import "testing"

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
