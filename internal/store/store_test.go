package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/Lavanic/crier/internal/sources"
)

var testJob = sources.Job{
	Source:   "greenhouse",
	Company:  "stripe",
	JobID:    "12345",
	Title:    "Software Engineer, New Grad",
	Location: "San Francisco, CA",
	URL:      "https://stripe.com/jobs/12345",
	Category: "Software",
}

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func TestMarkSeenDedups(t *testing.T) {
	s, _ := openTemp(t)
	now := time.Now()

	isNew, err := s.MarkSeen(testJob, now)
	if err != nil {
		t.Fatal(err)
	}
	if !isNew {
		t.Error("first MarkSeen should report new")
	}

	// same job again, must be a no-op
	isNew, err = s.MarkSeen(testJob, now.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if isNew {
		t.Error("second MarkSeen should NOT report new")
	}

	// different job id is a different job
	other := testJob
	other.JobID = "99999"
	if isNew, _ = s.MarkSeen(other, now); !isNew {
		t.Error("job with different id should be new")
	}
}

func TestSurvivesReopen(t *testing.T) {
	// the whole point of the store: memory must outlive the process.
	// simulate a restart by closing and reopening the same file
	s, path := openTemp(t)
	if _, err := s.MarkSeen(testJob, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	isNew, err := s2.MarkSeen(testJob, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if isNew {
		t.Error("job seen before restart should still be known after")
	}
}

func TestMarkNotified(t *testing.T) {
	s, _ := openTemp(t)
	now := time.Unix(1751800000, 0)
	if _, err := s.MarkSeen(testJob, now); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkNotified(testJob.DedupKey(), now); err != nil {
		t.Fatal(err)
	}
	var got int64
	err := s.db.QueryRow(`SELECT notified_at FROM jobs WHERE id = ?`, testJob.DedupKey()).Scan(&got)
	if err != nil {
		t.Fatal(err)
	}
	if got != now.Unix() {
		t.Errorf("notified_at = %d, want %d", got, now.Unix())
	}
}

func TestUnnotifiedBacklog(t *testing.T) {
	s, _ := openTemp(t)
	now := time.Unix(1751800000, 0)

	if _, err := s.MarkSeen(testJob, now); err != nil {
		t.Fatal(err)
	}
	old := testJob
	old.JobID = "ancient"
	if _, err := s.MarkSeen(old, now.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}

	pending, err := s.UnnotifiedSince(now.Add(-24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("got %d pending, want 1 (48h-old row is outside the window)", len(pending))
	}
	p := pending[0]
	if p.Key != testJob.DedupKey() {
		t.Errorf("Key = %q", p.Key)
	}
	if p.Job.Title != testJob.Title || p.Job.Location != testJob.Location ||
		p.Job.Category != testJob.Category {
		t.Errorf("round-tripped job = %+v", p.Job)
	}

	// once notified it leaves the backlog
	if err := s.MarkNotified(testJob.DedupKey(), now); err != nil {
		t.Fatal(err)
	}
	if pending, _ = s.UnnotifiedSince(now.Add(-24 * time.Hour)); len(pending) != 0 {
		t.Errorf("notified job still in backlog: %+v", pending)
	}
}

func TestSeenSince(t *testing.T) {
	// feeds the cross-post dedup: anything seen OR notified inside the
	// window comes back, even if it never alerted. jobs whose whole
	// history is outside the window stay out
	s, _ := openTemp(t)
	now := time.Unix(1751800000, 0)

	fresh := testJob
	stale := testJob
	stale.JobID = "old-alert"
	killed := testJob
	killed.JobID = "seen-but-filtered"
	for _, j := range []sources.Job{fresh, stale} {
		if _, err := s.MarkSeen(j, now.Add(-10*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.MarkSeen(killed, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkNotified(fresh.DedupKey(), now); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkNotified(stale.DedupKey(), now.Add(-9*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	got, err := s.SeenSince(now.Add(-7 * 24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d refs, want 2 (fresh notified in window, killed seen in window, stale all outside)", len(got))
	}
	keys := map[string]bool{}
	for _, r := range got {
		if r.Company != testJob.Company || r.URL != testJob.URL {
			t.Errorf("ref = %+v", r)
		}
		keys[r.Key] = true
	}
	if !keys[fresh.DedupKey()] || !keys[killed.DedupKey()] {
		t.Errorf("wrong keys came back: %v", keys)
	}
}

func TestCountJobs(t *testing.T) {
	s, _ := openTemp(t)
	if n, err := s.CountJobs(); err != nil || n != 0 {
		t.Fatalf("fresh db count = %d err = %v, want 0", n, err)
	}
	if _, err := s.MarkSeen(testJob, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountJobs(); n != 1 {
		t.Errorf("count = %d, want 1", n)
	}
}

func TestMigratesLegacySchema(t *testing.T) {
	// simulate a db created before the location column existed:
	// v1 schema only, user_version left at 1
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(migrations[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO jobs (id, company, title, url, source, first_seen_at)
		VALUES ('greenhouse:stripe:1', 'stripe', 'SWE I', 'https://x', 'greenhouse', 1751800000)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("opening legacy db should migrate, got %v", err)
	}
	defer s.Close()
	// old row survives with a default location, new writes carry one
	pending, err := s.UnnotifiedSince(time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Job.Location != "" {
		t.Errorf("legacy row after migration = %+v", pending)
	}
	if _, err := s.MarkSeen(testJob, time.Now()); err != nil {
		t.Errorf("insert with location after migration: %v", err)
	}
}

func TestPollTimestamps(t *testing.T) {
	s, _ := openTemp(t)

	if _, ok, err := s.LastPolled("github:simplifyjobs"); err != nil || ok {
		t.Fatalf("never-polled source: ok=%v err=%v, want ok=false err=nil", ok, err)
	}

	first := time.Unix(1751800000, 0)
	if err := s.SetPolled("github:simplifyjobs", first); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.LastPolled("github:simplifyjobs")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !got.Equal(first) {
		t.Errorf("LastPolled = %v, want %v", got, first)
	}

	// second SetPolled must overwrite, not error on the primary key
	second := first.Add(5 * time.Minute)
	if err := s.SetPolled("github:simplifyjobs", second); err != nil {
		t.Fatal(err)
	}
	if got, _, _ = s.LastPolled("github:simplifyjobs"); !got.Equal(second) {
		t.Errorf("after upsert LastPolled = %v, want %v", got, second)
	}
}

func TestStaleSourcesFindsAndThrottles(t *testing.T) {
	s, _ := openTemp(t)
	now := time.Now()

	// a first poll counts as healthy, otherwise every new source pages
	// on the tick that introduces it
	if err := s.SetPolled("github:simplifyjobs", now); err != nil {
		t.Fatal(err)
	}
	stale, err := s.StaleSources(now, 6*time.Hour, 24*time.Hour, []string{"github:simplifyjobs"})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Fatalf("a brand new source read as stale: %+v", stale)
	}

	// keep attempting for 7h without ever succeeding
	later := now.Add(7 * time.Hour)
	if err := s.SetPolled("github:simplifyjobs", later); err != nil {
		t.Fatal(err)
	}
	stale, err = s.StaleSources(later, 6*time.Hour, 24*time.Hour, []string{"github:simplifyjobs"})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || stale[0].Name != "github:simplifyjobs" {
		t.Fatalf("got %+v, want the one stale source", stale)
	}

	// warned once, so it must stay quiet on the very next tick
	stale, err = s.StaleSources(later.Add(30*time.Second), 6*time.Hour, 24*time.Hour, []string{"github:simplifyjobs"})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Errorf("warned twice in a row: %+v", stale)
	}

	// one success clears it, the cookie came back
	if err := s.SetPolledOK("github:simplifyjobs", later); err != nil {
		t.Fatal(err)
	}
	stale, err = s.StaleSources(later.Add(time.Hour), 6*time.Hour, 24*time.Hour, []string{"github:simplifyjobs"})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Errorf("still stale after a success: %+v", stale)
	}
}

// a source deleted from the config leaves its poll row behind. nothing
// ever polls it again, so without the live filter it pages every day
func TestStaleSourcesIgnoresRemovedSources(t *testing.T) {
	s, _ := openTemp(t)
	now := time.Now()

	if err := s.SetPolled("greenhouse:gone", now); err != nil {
		t.Fatal(err)
	}
	later := now.Add(7 * time.Hour)

	stale, err := s.StaleSources(later, 6*time.Hour, 24*time.Hour, []string{"greenhouse:still-here"})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Fatalf("paged about a source that is no longer configured: %+v", stale)
	}

	// still reported while it IS configured
	stale, err = s.StaleSources(later, 6*time.Hour, 24*time.Hour, []string{"greenhouse:gone"})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 {
		t.Errorf("got %+v, want the configured-but-down source", stale)
	}
}
