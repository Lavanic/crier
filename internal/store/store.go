// Package store is crier's memory, a sqlite file with two tables:
// jobs (every posting ever seen, keyed by DedupKey, this is the dedup)
// and source_polls (when each source was last polled, so slow-cadence
// sources like the github feeds can be skipped between ticks).
// the binary dies and restarts every 30s under systemd, this file is
// the only thing that persists between ticks
package store

import (
	"database/sql"
	"fmt"
	"time"

	// blank import: we never call this package directly, importing it
	// runs its init() which registers the "sqlite" driver with
	// database/sql. pure go, no C compiler needed
	_ "modernc.org/sqlite"

	"github.com/Lavanic/crier/internal/sources"
)

const schema = `
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

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	// busy_timeout makes a second process wait 5s for the lock
	// instead of erroring instantly, in case a slow tick overlaps
	// the next one. WAL journal mode is the friendlier mode for that
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// database/sql keeps a connection POOL by default. sqlite has one
	// writer at a time, so cap the pool at 1 and skip a whole class
	// of table-locked errors
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// MarkSeen records a job and reports whether it was new. the INSERT OR
// IGNORE against the primary key IS the dedup: re-inserting a job we
// already know is a no-op that reports 0 rows changed. safe to rerun
// on the same jobs forever
func (s *Store) MarkSeen(j sources.Job, now time.Time) (bool, error) {
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO jobs (id, company, title, url, source, first_seen_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		j.DedupKey(), j.Company, j.Title, j.URL, j.Source, now.Unix(),
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

// MarkNotified stamps when the alert actually went out. lets us tell
// "seen and alerted" apart from "seen while alerts were failing"
func (s *Store) MarkNotified(dedupKey string, now time.Time) error {
	_, err := s.db.Exec(
		`UPDATE jobs SET notified_at = ? WHERE id = ?`, now.Unix(), dedupKey,
	)
	return err
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

// SetPolled upserts the last-poll timestamp for a source
func (s *Store) SetPolled(source string, now time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO source_polls (source, last_polled_at) VALUES (?, ?)
		 ON CONFLICT(source) DO UPDATE SET last_polled_at = excluded.last_polled_at`,
		source, now.Unix(),
	)
	return err
}
