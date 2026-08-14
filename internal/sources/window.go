package sources

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Windowed is an optional interface. main skips a source that says no
// before it even checks the poll interval. board apis don't care what
// time it is, a burner instagram account pinging at 4am does
type Windowed interface {
	ActiveAt(t time.Time) bool
}

// ActiveHours is a polling window like 09-23, half open, so 23 means
// "stops at 11pm". zero value is always on
type ActiveHours struct {
	Start, End int
	Loc        *time.Location
}

// ParseActiveHours reads "09-23". empty string means always on, which
// is also what config leaves it as when nobody sets it
func ParseActiveHours(spec, tz string) (ActiveHours, error) {
	var a ActiveHours
	if spec == "" {
		return a, nil
	}
	start, end, ok := strings.Cut(spec, "-")
	if !ok {
		return a, fmt.Errorf("active_hours %q: want START-END, like 09-23", spec)
	}
	for _, p := range []struct {
		s   string
		dst *int
	}{{start, &a.Start}, {end, &a.End}} {
		n, err := strconv.Atoi(strings.TrimSpace(p.s))
		if err != nil || n < 0 || n > 23 {
			return ActiveHours{}, fmt.Errorf("active_hours %q: %q is not an hour 0-23", spec, p.s)
		}
		*p.dst = n
	}
	if a.Start == a.End {
		return ActiveHours{}, fmt.Errorf("active_hours %q: start and end are the same, leave it empty for always on", spec)
	}
	// utc on the droplet, so a window without a zone would silently
	// mean something 4-5 hours off from what you wrote
	loc := time.UTC
	if tz != "" {
		l, err := time.LoadLocation(tz)
		if err != nil {
			return ActiveHours{}, fmt.Errorf("timezone %q: %w", tz, err)
		}
		loc = l
	}
	a.Loc = loc
	return a, nil
}

func (a ActiveHours) Contains(t time.Time) bool {
	if a.Start == a.End {
		return true
	}
	h := t.In(a.Loc).Hour()
	if a.Start < a.End {
		return h >= a.Start && h < a.End
	}
	// wraps midnight, 22-06
	return h >= a.Start || h < a.End
}
