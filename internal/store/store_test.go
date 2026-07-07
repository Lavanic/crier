package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Lavanic/crier/internal/sources"
)

var testJob = sources.Job{
	Source:  "greenhouse",
	Company: "stripe",
	JobID:   "12345",
	Title:   "Software Engineer, New Grad",
	URL:     "https://stripe.com/jobs/12345",
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
