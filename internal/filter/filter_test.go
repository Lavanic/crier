package filter

import (
	"testing"

	"github.com/Lavanic/crier/internal/sources"
)

// same patterns as config.yaml, kept in sync by hand.
// filter package itself doesn't care what the patterns are
var include = []string{
	`(?i)(software\s+engineer|software\s+development\s+engineer|swe|sde|sw\s*eng|developer|programmer)[\s,:()\-–—/]*(i\b|1\b|new\s*grad(uate)?|early\s+career|entry\s+level|associate|university|college|campus|graduate|junior)`,
	`(?i)new\s*grad.*software`,
	`(?i)software.*new\s*grad`,
	`(?i)entry[-\s]level.*(engineer|developer|programmer)`,
	`(?i)(engineer|developer|programmer).*entry[-\s]level`,
	`(?i)associate\s+software\s+engineer`,
	`(?i)new\s*grad(uate)?`,
	`(?i)(software|engineer|developer|swe|sde).*\b202[67]\b`,
	`(?i)\b202[67]\b.*(software|engineer|developer|swe|sde)`,
	`(?i)(university|campus|college)\s*(grad(uate)?|hire|recruit)`,
	`(?i)\bgrad(uate)?\b.*\b(software|engineer|developer|swe|sde|machine\s+learning)\b`,
	`(?i)\b(software|engineer(ing)?|developer|swe|sde|machine\s+learning|technology)\b.*\bgrad(uate)?\b`,
	`(?i)(\bjunior\b.*\b(engineer|developer|swe|sde)\b|\b(engineer|developer)\b.*\bjunior\b)`,
	`(?i)early[\s-]+(in[\s-]+)?career`,
	`(?i)recent\s+grad(uate)?`,
	`(?i)emerging\s+talent`,
	`(?i)\bbachelors?\b`,
	`(?i)(leadership|technology|engineering|software|digital)\s+development\s+program`,
	`(?i)(\bassociate\b.*\b(software|developer)\b|\b(software|developer)\b.*\bassociate\b|\bassociate\b\s+((ai|ml|machine\s+learning|deep\s+learning|data)\s+)+engineer\b)`,
	`(?i)(\b(software|developer|engineer(ing)?|tech(nology)?)\b.*\b(apprentice(ship)?|residency|fellowship|rotation(al)?)\b|\b(apprentice(ship)?|residency|fellowship|rotation(al)?)\b.*\b(software|developer|engineer(ing)?|tech(nology)?)\b)`,
	`(?i)\bengineer\b[\s,:()\-–—/]*(i|1)\b`,
}

// staff and lead deliberately absent, they killed real postings
// (MTS new grad at AI labs, TikTok's Lead Ads team)
var exclude = []string{
	"intern", "internship", "co-op", "coop", "senior",
	"principal", "manager", "director", "architect", "phd",
	"recruiter", "coordinator",
	"sr", "field service", "field engineer", "sales engineer",
	"technical support", "propulsion", "mechanical", "machining",
	"manufacturing", "production engineer", "process development",
	"trainer", "gnc", "asic", "power platform",
	"product operations", "configuration management", "hvac",
	"uipath", "robotic process automation",
}

// titleOnly builds a filter with just the two title gates, the way v1
// worked. most tests only care about titles
func titleOnly(t *testing.T) *Filter {
	t.Helper()
	f, err := New(Config{Include: include, ExcludeKeywords: exclude})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestMatch(t *testing.T) {
	f := titleOnly(t)

	tests := []struct {
		name  string
		title string
		want  bool
	}{
		// should alert
		{"classic new grad", "Software Engineer, New Grad", true},
		{"year and parens", "Software Engineer - New Grad (2027)", true},
		{"level one", "Software Engineer I", true},
		{"early career after comma", "Software Developer, Early Career", true},
		{"associate", "Associate Software Engineer", true},
		{"lowercase university", "software engineer, university grad 2027", true},
		// the word-boundary case: contains "intern" inside
		// "International" but must NOT be dropped
		{"international is not intern", "New Grad Software Engineer, International Team", true},
		{"nonstandard role new grad", "Forward Deployed Engineer - New Grad", true},
		{"year tagged", "Software Engineer (2027 Start)", true},
		{"university graduate", "University Graduate - Software Engineering", true},
		// gains from the audited filter rewrite, all real titles
		// that the old patterns silently missed
		{"en dash early career", "Software Engineer – Early Career", true},
		{"graduate first", "Graduate Software Engineer", true},
		{"graduate last", "Machine Learning Engineer Graduate", true},
		{"junior first", "Junior Software Engineer", true},
		{"year first", "2026/2027 Early Career Software Engineer", true},
		{"engineer one", "Android Engineer I", true},
		{"rotational", "Software Developer - Rotational Program", true},
		// staff removed from excludes for exactly this ai-lab title
		{"member of technical staff", "Member of Technical Staff New Grad - Machine Learning", true},
		{"lead ads is not lead", "New Grad 2026: ML Engineer Graduate (Lead Ads Technology)", true},
		// "production" alone is deliberately not an exclude keyword
		{"production infra is fine", "Software Engineer New Grad - Production Infrastructure", true},

		// should stay silent
		{"level two", "Software Engineer II", false},
		{"senior fullstack", "Senior Fullstack Engineer", false},
		{"sales role", "Account Executive, AI Sales (Grower)", false},
		{"intern", "Software Engineering Intern - Summer 2027", false},
		{"new grad but phd", "New Grad Software Engineer (PhD)", false},
		{"new grad but manager", "New Grad Software Engineering Manager", false},
		{"co-op with hyphen", "New Grad Software Co-op", false},
		// exclude list still guards the wide year pattern
		{"year tagged but senior", "Senior Software Engineer (2027 Start)", false},
		// hr roles kept matching the early-career patterns until
		// recruiter/coordinator joined the excludes
		{"recruiter", "Early Careers Recruiter", false},
		{"coordinator", "Campus Attraction & Events Coordinator", false},
		// the 2026-07 precision batch, each one a real junk alert
		{"sr dot leak", "Sr. Software Engineer I, Cloud Security Platform", false},
		{"field service", "Field Service Engineer 1", false},
		{"tech support", "Technical Support Engineer 1", false},
		{"propulsion", "Propulsion Engineer I - Engine Systems", false},
		{"mechanical", "Dec 2026 New Graduate Engineer, Mechanical (Starship)", false},
		{"chem plant production", "Production Engineer 1", false},
		{"software trainer", "Systems Analyst Associate - Software Trainer", false},
		{"asic", "ASIC Design Engineer New Grad", false},
		{"config mgmt", "Digital Product Configuration Management Engineer 1", false},
		{"low code", "Application Developer 1 - Power Platform", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.Match(sources.Job{Title: tt.title}); got != tt.want {
				t.Errorf("Match(%q) = %v, want %v", tt.title, got, tt.want)
			}
		})
	}
}

func TestExcludePatterns(t *testing.T) {
	// the 2026-start pattern has to catch the date whether it's in the
	// title or buried in a workday url slug (vanguard's trick)
	f, err := New(Config{
		Include:         include,
		ExcludePatterns: []string{`(?i)\b202[0-6][\s_-]*start\b`},
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		job  sources.Job
		want bool
	}{
		{"date in title", sources.Job{Title: "Software Engineer (2026 Start)"}, false},
		{"date in url only", sources.Job{
			Title: "Entry Level Application Engineer",
			URL:   "https://vanguard.wd5.myworkdayjobs.com/en-US/vanguard_external/job/Malvern-PA/Entry-Level-Application-Engineer---2026-Start-Date_168908",
		}, false},
		{"2027 start is my cohort", sources.Job{Title: "Software Engineer (2027 Start)"}, true},
		// "2026" without "start" right after must survive, that's how
		// new-grad-class-of-2026 titles are written
		{"class of 2026", sources.Job{
			Title: "Software Engineer New Grad - 2026",
			URL:   "https://cadence.wd1.myworkdayjobs.com/University_Talent/job/Burlington-MA/Software-Engineer--New-College-Grad-2026--Undergrads-_R54894",
		}, true},
		// \b after "start" so 2026-startup-x urls don't die
		{"startup is not start", sources.Job{
			Title: "New Grad Software Engineer",
			URL:   "https://example.com/jobs/2026-startup-cohort/123",
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.Match(tt.job); got != tt.want {
				t.Errorf("Match(%+v) = %v, want %v", tt.job, got, tt.want)
			}
		})
	}
}

func TestLocationGate(t *testing.T) {
	f, err := New(Config{
		Include:          include,
		ExcludeLocations: []string{"canada", "toronto", "uk", "london", "india", "ireland", "dublin"},
	})
	if err != nil {
		t.Fatal(err)
	}

	title := "Software Engineer, New Grad"
	tests := []struct {
		name string
		loc  string
		want bool
	}{
		{"us city", "Boston, MA", true},
		{"empty location", "", true},
		// fail open: a city we can't classify must not cost an alert
		{"unknown location", "Starbase, TX", true},
		{"canada", "Ottawa, ON, Canada", false},
		{"remote uk", "Remote - UK", false},
		{"india", "Remote - India", false},
		// multi-location: one us office saves the posting
		{"toronto or sf", "Toronto, ON, Canada | SF", true},
		{"all non-us", "London, UK | Dublin, Ireland", false},
		// word boundary: "Indianapolis" contains "india" but is not india
		{"indianapolis", "Indianapolis, IN", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.Match(sources.Job{Title: title, Location: tt.loc}); got != tt.want {
				t.Errorf("location %q = %v, want %v", tt.loc, got, tt.want)
			}
		})
	}
}

func TestCategoryGate(t *testing.T) {
	f, err := New(Config{
		Include:                include,
		ExcludeCategories:      []string{"product", "hardware"},
		CategoryRescueKeywords: []string{"software", "firmware", "embedded"},
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		title    string
		category string
		want     bool
	}{
		{"software category", "Software Engineer New Grad", "Software", true},
		{"boards have no category", "Software Engineer New Grad", "", true},
		{"hardware dies", "Computer Hardware Engineer 1", "Hardware", false},
		{"product dies", "Product Development Engineer 1", "Product", false},
		// substring match covers the feed's legacy category names
		{"legacy hardware dies", "Design Engineer New Grad", "Hardware Engineering", false},
		// the rescue list: simplify files these under Hardware but
		// they're software jobs (anduril, rtx, sandisk, all real)
		{"firmware rescued", "Early Career Firmware Engineer", "Hardware", true},
		{"embedded rescued", "Entry Level Embedded Engineer", "Hardware", true},
		{"software rescued", "Embedded Linux Software Engineer 1", "Hardware", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := f.Match(sources.Job{Title: tt.title, Category: tt.category})
			if got != tt.want {
				t.Errorf("title %q category %q = %v, want %v", tt.title, tt.category, got, tt.want)
			}
		})
	}
}

func TestNoExcludeKeywords(t *testing.T) {
	// empty lists must mean "exclude nothing", not "exclude everything"
	// (an empty regex alternation would match every string)
	f, err := New(Config{Include: []string{`(?i)new\s*grad`}})
	if err != nil {
		t.Fatal(err)
	}
	if !f.Match(sources.Job{Title: "New Grad Backend Engineer", Location: "Berlin?", Category: "???"}) {
		t.Error("filter with no exclude lists should still pass includes")
	}
}

// a story link we couldn't get a title for is a link we know nothing
// about. it dies here, and shows up in `make stories` for review
func TestUntitledJobNeverAlerts(t *testing.T) {
	f, err := New(Config{Include: include, ExcludeKeywords: exclude})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{
		"https://job-boards.greenhouse.io/ctc/jobs/4716937005",
		"https://careers.ibm.com/en_US/careers/JobDetail?jobId=128497",
		"https://lifeattiktok.com/search/7672554809555192117",
	} {
		if f.Match(sources.Job{URL: u}) {
			t.Errorf("untitled link alerted: %s", u)
		}
	}
}

func TestBadPatternErrors(t *testing.T) {
	if _, err := New(Config{Include: []string{"([unclosed"}}); err == nil {
		t.Error("expected error for a bad include pattern")
	}
	if _, err := New(Config{
		Include:         []string{"x"},
		ExcludePatterns: []string{"([unclosed"},
	}); err == nil {
		t.Error("expected error for a bad exclude pattern")
	}
}
