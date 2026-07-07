// Package sources holds the Job model, the Source interface that every
// job board client implements, and the shared http plumbing they all use.
package sources

import (
	"context"
	"fmt"
	"time"
)

// Job is one job posting, normalized to the same shape no matter
// which board it came from. every client's whole purpose is to turn
// its board's weird json into a slice of these
type Job struct {
	Source   string // which client found it (greenhouse, lever, ...)
	Company  string
	JobID    string // the board's own id for this posting
	Title    string
	Location string
	URL      string // the apply link, this is what the phone alert opens
}

// DedupKey becomes the primary key in sqlite. same posting seen twice
// makes the same key, so the second insert is a no-op and no dupe alert
func (j Job) DedupKey() string {
	return fmt.Sprintf("%s:%s:%s", j.Source, j.Company, j.JobID)
}

// Source is anything crier can poll for jobs. greenhouse, lever, ashby
// and the github feeds each get a type that satisfies this
type Source interface {
	// Name identifies the source in logs and the poll-cadence table
	Name() string
	// Fetch grabs all current postings. an error means the whole
	// source failed this tick, don't return partial results
	Fetch(ctx context.Context) ([]Job, error)
	// MinInterval is the minimum time between polls of this source,
	// zero means poll every tick
	MinInterval() time.Duration
}
