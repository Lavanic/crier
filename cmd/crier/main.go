// crier polls ATS job boards + aggregator feeds and fires a pushover
// critical alert for every new posting that matches my filters.
// runs as a short-lived process under a systemd timer, so
// one invocation = one poll tick, then exit.
//
// first run on a fresh db: EVERYTHING is new, so seed with --dry-run
// once before running for real or the phone gets carpet-bombed
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Lavanic/crier/internal/config"
	"github.com/Lavanic/crier/internal/filter"
	"github.com/Lavanic/crier/internal/notify"
	"github.com/Lavanic/crier/internal/sources"
	"github.com/Lavanic/crier/internal/store"
)

// cap on in-flight http requests so a big slug list doesn't open
// 50 sockets at once
const maxConcurrent = 20

// a tick has to finish well before systemd fires the next one at 30s.
// sources that miss the deadline get cut off, not waited on
const tickTimeout = 25 * time.Second

func main() {
	dryRun := flag.Bool("dry-run", false, "log would-notify jobs instead of alerting")
	configPath := flag.String("config", "config.yaml", "path to config file")
	pretty := flag.Bool("pretty", false, "human-readable logs instead of json")
	flag.Parse()

	// json to stdout in prod so journalctl captures structured lines,
	// text mode for eyeballing during dev
	var handler slog.Handler = slog.NewJSONHandler(os.Stdout, nil)
	if *pretty {
		handler = slog.NewTextHandler(os.Stdout, nil)
	}
	log := slog.New(handler)

	if err := run(log, *configPath, *dryRun); err != nil {
		log.Error("tick failed", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, configPath string, dryRun bool) error {
	start := time.Now()

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	f, err := filter.New(cfg.Filter.Include, cfg.Filter.ExcludeKeywords)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	// creds are only required when we'd actually send
	var notifier *notify.Notifier
	if !dryRun {
		if !cfg.Pushover.HasCreds() {
			return errors.New("no pushover creds configured, add config.local.yaml or use --dry-run")
		}
		notifier = notify.New(cfg.Pushover.AppToken, cfg.Pushover.UserKey)
	}

	srcs := buildSources(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), tickTimeout)
	defer cancel()

	// fan out: each source fetches concurrently, capped by the
	// semaphore. matches fan IN to one slice so notifications can go
	// out serially afterwards (pushover allows max 2 concurrent reqs)
	g, ctx := errgroup.WithContext(ctx)
	sem := make(chan struct{}, maxConcurrent)
	var mu sync.Mutex
	var matched []sources.Job
	now := time.Now()

	for _, src := range srcs {
		g.Go(func() error {
			select {
			case sem <- struct{}{}: // take a slot
			case <-ctx.Done():
				return ctx.Err()
			}
			defer func() { <-sem }() // give it back

			// slow-cadence sources (github feeds) skip most ticks
			last, ok, err := st.LastPolled(src.Name())
			if err != nil {
				return err
			}
			if ok && now.Sub(last) < src.MinInterval() {
				return nil
			}

			jobs, err := src.Fetch(ctx)
			if err != nil {
				// one dead board must not kill the tick, warn and move on
				log.Warn("source failed", "source", src.Name(), "err", err)
				return nil
			}
			if len(jobs) == 0 {
				log.Warn("source returned 0 jobs", "source", src.Name())
			}
			if err := st.SetPolled(src.Name(), now); err != nil {
				return err
			}

			newCount, matchCount := 0, 0
			for _, j := range jobs {
				// mark seen BEFORE filtering so loosening the filter
				// later can't re-alert on old postings
				isNew, err := st.MarkSeen(j, now)
				if err != nil {
					return err
				}
				if !isNew {
					continue
				}
				newCount++
				if f.Match(j.Title) {
					matchCount++
					mu.Lock()
					matched = append(matched, j)
					mu.Unlock()
				}
			}
			log.Info("source ok",
				"source", src.Name(), "jobs", len(jobs),
				"new", newCount, "matched", matchCount)
			return nil
		})
	}
	// waits for every goroutine, returns the first real error (db
	// trouble or timeout, fetch errors were downgraded to warns)
	if err := g.Wait(); err != nil {
		return err
	}

	for _, j := range matched {
		if dryRun {
			log.Info("dry-run, would notify",
				"company", j.Company, "title", j.Title,
				"location", j.Location, "url", j.URL)
			continue
		}
		if err := notifier.Notify(j); err != nil {
			// loud but not fatal, the job stays un-notified in the db
			log.Error("notify failed", "job", j.DedupKey(), "err", err)
			continue
		}
		if err := st.MarkNotified(j.DedupKey(), time.Now()); err != nil {
			log.Error("mark notified failed", "job", j.DedupKey(), "err", err)
		}
		log.Info("notified", "company", j.Company, "title", j.Title, "url", j.URL)
	}

	log.Info("tick done",
		"sources", len(srcs), "matched", len(matched),
		"dry_run", dryRun, "elapsed_ms", time.Since(start).Milliseconds())
	return nil
}

// turns config entries into live Source values. the loop bodies are
// the ONLY place in main that knows concrete source types
func buildSources(cfg *config.Config) []sources.Source {
	client := sources.NewHTTPClient()
	var srcs []sources.Source
	for _, slug := range cfg.Sources.Greenhouse {
		srcs = append(srcs, sources.NewGreenhouse(client, slug))
	}
	for _, slug := range cfg.Sources.Lever {
		srcs = append(srcs, sources.NewLever(client, slug))
	}
	for _, slug := range cfg.Sources.Ashby {
		srcs = append(srcs, sources.NewAshby(client, slug))
	}
	for _, feed := range cfg.Sources.GitHub {
		srcs = append(srcs, sources.NewGitHubFeed(
			client, feed.Name, feed.URL,
			time.Duration(feed.MinIntervalSec)*time.Second))
	}
	return srcs
}
