// Package store is crier's memory, a sqlite file with two tables:
// jobs (every posting ever seen, keyed by DedupKey, this is the dedup)
// and source_polls (when each slow-cadence source was last polled).
// the binary dies and restarts every 30s under systemd, this file is
// the only thing that persists between ticks
package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	// blank import: we never call this package directly, importing it
	// runs its init() which registers the "sqlite" driver with
	// database/sql. pure go, no C compiler needed
	_ "modernc.org/sqlite"

	"github.com/Lavanic/crier/internal/sources"
)

const schemaV1 = `
CREATE TABLE IF NOT EXISTS jobs (
	id            TEXT PRIMARY KEY,
	company       TEXT NOT NULL,
	title         TEXT NOT NULL,
	url           TEXT NOT NULL,
	source        TEXT NOT NULL,
	first_seen_at INTEGER NOT NULL,
	notified_at   INTEGER
);
CREATE INDEX IF NOT EXISTS idx_jobs_source_first_seen ON jobs(source, first_seen_at);

CREATE TABLE IF NOT EXISTS source_polls (
	source         TEXT PRIMARY KEY,
	last_polled_at INTEGER NOT NULL
);
`

// migrations run in order at Open. sqlite's user_version pragma
// remembers how far a db has gotten, so old dbs pick up new columns
// on their next tick. append new entries, never edit old ones
var migrations = []string{
	schemaV1,
	`ALTER TABLE jobs ADD COLUMN location TEXT NOT NULL DEFAULT ''`,
	// so backlog retries re-filter with the same category the fresh
	// fetch had, old rows default to ''
	`ALTER TABLE jobs ADD COLUMN category TEXT NOT NULL DEFAULT ''`,
	// retired. a link-title cache for a source that no longer exists.
	// migrations are append-only, so it stays as a no-op on old dbs
	`CREATE TABLE IF NOT EXISTS link_titles (
		url        TEXT PRIMARY KEY,
		title      TEXT NOT NULL,
		location   TEXT NOT NULL,
		fetched_at INTEGER NOT NULL
	)`,
	// retired alongside link_titles. scratch space for values that had
	// to outlive a tick, kept so existing dbs still migrate cleanly
	`CREATE TABLE IF NOT EXISTS kv (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,
	// a source can fail for days while the tick still exits 0 and the
	// dead-man switch stays green. these two make that noisy
	`ALTER TABLE source_polls ADD COLUMN last_ok_at INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE source_polls ADD COLUMN last_warned_at INTEGER NOT NULL DEFAULT 0`,
	// existing rows would otherwise read as "never succeeded" and all
	// page at once on the next tick
	`UPDATE source_polls SET last_ok_at = last_polled_at WHERE last_ok_at = 0`,
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	// busy_timeout makes a second process wait 5s for the lock
	// instead of erroring instantly, in case a manual run overlaps
	// the timer. WAL journal mode is the friendlier mode for that
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// database/sql keeps a connection POOL by default. sqlite has one
	// writer at a time, so cap the pool at 1 and skip a whole class
	// of table-locked errors
	db.SetMaxOpenConns(1)

	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		db.Close()
		return nil, fmt.Errorf("reading schema version: %w", err)
	}
	for i := version; i < len(migrations); i++ {
		if _, err := db.Exec(migrations[i]); err != nil {
			db.Close()
			return nil, fmt.Errorf("migration %d: %w", i+1, err)
		}
		// pragma can't take ? placeholders, i+1 is our own int
		if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			db.Close()
			return nil, fmt.Errorf("bumping schema version to %d: %w", i+1, err)
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// CountJobs is how main detects a fresh database (seed mode: mark
// everything seen but send no alerts, so a new host can't carpet-bomb
// the phone with hundreds of "new" postings)
func (s *Store) CountJobs() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&n)
	return n, err
}

// MarkSeen records a job and reports whether it was new. the INSERT OR
// IGNORE against the primary key IS the dedup: re-inserting a job we
// already know is a no-op that reports 0 rows changed. safe to rerun
// on the same jobs forever
func (s *Store) MarkSeen(j sources.Job, now time.Time) (bool, error) {
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO jobs (id, company, title, url, source, location, category, first_seen_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		j.DedupKey(), j.Company, j.Title, j.URL, j.Source, j.Location, j.Category, now.Unix(),
	)
	if err != nil {
		return false, fmt.Errorf("mark seen %s: %w", j.DedupKey(), err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// MarkNotified stamps a job as handled. "handled" means alerted for
// real, or logged in dry-run, or suppressed during a seed run. rows
// WITHOUT this stamp are the retry queue (see UnnotifiedSince)
func (s *Store) MarkNotified(dedupKey string, now time.Time) error {
	_, err := s.db.Exec(
		`UPDATE jobs SET notified_at = ? WHERE id = ?`, now.Unix(), dedupKey,
	)
	return err
}

// PendingAlert is a job that was seen but never successfully alerted,
// e.g. pushover was down or the tick got cut off mid-run. Key carries
// the original dedup key since Job.JobID isn't stored separately
type PendingAlert struct {
	Key string
	Job sources.Job
}

// UnnotifiedSince returns recent seen-but-never-alerted jobs. this is
// the safety net that turns "pushover hiccuped, alert lost forever"
// into "alert arrives one tick late". main re-runs the filter on
// these, so filtered-out jobs (notified_at NULL too) don't resurface
// unless the filter now wants them
func (s *Store) UnnotifiedSince(since time.Time) ([]PendingAlert, error) {
	rows, err := s.db.Query(
		`SELECT id, company, title, url, source, location, category FROM jobs
		 WHERE notified_at IS NULL AND first_seen_at >= ?`, since.Unix(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PendingAlert
	for rows.Next() {
		var p PendingAlert
		if err := rows.Scan(&p.Key, &p.Job.Company, &p.Job.Title, &p.Job.URL,
			&p.Job.Source, &p.Job.Location, &p.Job.Category); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SeenRef is just enough of a recorded job for main's cross-post
// dedup. Key lets main skip an alert's own row in the lookup
type SeenRef struct {
	Key     string
	Company string
	URL     string
}

// SeenSince returns every job recorded after the given time, alerted
// or not. cross-post dedup checks new alerts against all of these:
// once one portal's copy was seen and judged, the same req id arriving
// through another source shouldn't reopen the question
func (s *Store) SeenSince(since time.Time) ([]SeenRef, error) {
	rows, err := s.db.Query(
		`SELECT id, company, url FROM jobs
		 WHERE first_seen_at >= ? OR notified_at >= ?`, since.Unix(), since.Unix(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SeenRef
	for rows.Next() {
		var r SeenRef
		if err := rows.Scan(&r.Key, &r.Company, &r.URL); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LastPolled returns when a source was last polled. ok=false means
// never, ie. this is the first tick that's seen it
func (s *Store) LastPolled(source string) (t time.Time, ok bool, err error) {
	var unix int64
	err = s.db.QueryRow(
		`SELECT last_polled_at FROM source_polls WHERE source = ?`, source,
	).Scan(&unix)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return time.Unix(unix, 0), true, nil
}

// SetPolled upserts the last-poll timestamp for a source. a brand new
// row starts out "healthy" so a first-ever poll can't page instantly
func (s *Store) SetPolled(source string, now time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO source_polls (source, last_polled_at, last_ok_at) VALUES (?, ?, ?)
		 ON CONFLICT(source) DO UPDATE SET last_polled_at = excluded.last_polled_at`,
		source, now.Unix(), now.Unix(),
	)
	return err
}

// SetPolledOK stamps a successful fetch. the gap between this and
// last_polled_at is how long a source has been quietly broken
func (s *Store) SetPolledOK(source string, now time.Time) error {
	_, err := s.db.Exec(
		`UPDATE source_polls SET last_ok_at = ? WHERE source = ?`, now.Unix(), source,
	)
	return err
}

// StaleSource is a gated source that hasn't fetched anything in a while
type StaleSource struct {
	Name string
	Ok   time.Time // last successful fetch
}

// StaleSources returns sources with no success in `after` that haven't
// been warned about in `repeat`, and marks them warned in the same
// call so the caller can't double-send. live is the currently
// configured source names: a row for a source that was deleted from
// the config would otherwise page forever, nothing ever polls it again
func (s *Store) StaleSources(now time.Time, after, repeat time.Duration, live []string) ([]StaleSource, error) {
	if len(live) == 0 {
		return nil, nil
	}
	// has to be in the query, not a filter on the results. this marks
	// rows warned as it reads them
	args := []any{now.Add(-after).Unix(), now.Add(-repeat).Unix()}
	for _, name := range live {
		args = append(args, name)
	}
	rows, err := s.db.Query(
		`SELECT source, last_ok_at FROM source_polls
		 WHERE last_ok_at < ? AND last_warned_at < ?
		   AND source IN (?`+strings.Repeat(`,?`, len(live)-1)+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StaleSource
	for rows.Next() {
		var st StaleSource
		var unix int64
		if err := rows.Scan(&st.Name, &unix); err != nil {
			return nil, err
		}
		st.Ok = time.Unix(unix, 0)
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, st := range out {
		if _, err := s.db.Exec(
			`UPDATE source_polls SET last_warned_at = ? WHERE source = ?`,
			now.Unix(), st.Name,
		); err != nil {
			return nil, err
		}
	}
	return out, nil
}
