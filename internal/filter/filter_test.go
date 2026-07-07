package filter

import "testing"

// same patterns as examples/config.yaml, kept in sync by hand.
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
}

func TestMatch(t *testing.T) {
	f, err := New(include, exclude)
	if err != nil {
		t.Fatal(err)
	}

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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.Match(tt.title); got != tt.want {
				t.Errorf("Match(%q) = %v, want %v", tt.title, got, tt.want)
			}
		})
	}
}

func TestNoExcludeKeywords(t *testing.T) {
	// empty exclude list must mean "exclude nothing", not "exclude
	// everything" (an empty regex alternation would match every string)
	f, err := New([]string{`(?i)new\s*grad`}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Match("New Grad Backend Engineer") {
		t.Error("filter with no exclude list should still pass includes")
	}
}

func TestBadPatternErrors(t *testing.T) {
	if _, err := New([]string{"([unclosed"}, nil); err == nil {
		t.Error("expected error for a bad include pattern")
	}
}
