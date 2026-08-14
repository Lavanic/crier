package sources

import (
	"testing"
	"time"
)

func TestParseActiveHoursRejectsGarbage(t *testing.T) {
	for _, spec := range []string{"9", "09:23", "09-24", "-5-9", "nine-ten", "09-09"} {
		if _, err := ParseActiveHours(spec, ""); err == nil {
			t.Errorf("ParseActiveHours(%q) accepted it, want an error", spec)
		}
	}
	if _, err := ParseActiveHours("09-23", "Mars/Olympus"); err == nil {
		t.Error("bad timezone accepted, want an error")
	}
}

func TestActiveHoursContains(t *testing.T) {
	ny, err := ParseActiveHours("09-23", "America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		utc  string
		want bool
	}{
		// the whole point of the timezone field: 13:00 utc is 9am in ny,
		// and a naive utc window would have started four hours early
		{"9am ny", "2026-08-14T13:00:00Z", true},
		{"8:59am ny", "2026-08-14T12:59:00Z", false},
		{"10:59pm ny", "2026-08-15T02:59:00Z", true},
		{"11pm ny", "2026-08-15T03:00:00Z", false},
		{"4am ny", "2026-08-14T08:00:00Z", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at, err := time.Parse(time.RFC3339, tt.utc)
			if err != nil {
				t.Fatal(err)
			}
			if got := ny.Contains(at); got != tt.want {
				t.Errorf("Contains(%s) = %v, want %v", tt.utc, got, tt.want)
			}
		})
	}
}

// an unset window must never gate anything, that's the default for
// every source that isn't instagram
func TestActiveHoursZeroValueIsAlwaysOn(t *testing.T) {
	var a ActiveHours
	if !a.Contains(time.Date(2026, 8, 14, 4, 0, 0, 0, time.UTC)) {
		t.Error("zero value gated a poll")
	}
	empty, err := ParseActiveHours("", "")
	if err != nil {
		t.Fatal(err)
	}
	if !empty.Contains(time.Date(2026, 8, 14, 4, 0, 0, 0, time.UTC)) {
		t.Error("empty spec gated a poll")
	}
}

func TestActiveHoursWrapsMidnight(t *testing.T) {
	a, err := ParseActiveHours("22-06", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	at := func(h int) time.Time { return time.Date(2026, 8, 14, h, 0, 0, 0, time.UTC) }
	for _, h := range []int{22, 23, 0, 5} {
		if !a.Contains(at(h)) {
			t.Errorf("hour %d should be inside 22-06", h)
		}
	}
	for _, h := range []int{6, 12, 21} {
		if a.Contains(at(h)) {
			t.Errorf("hour %d should be outside 22-06", h)
		}
	}
}
