// crier polls ATS job boards + aggregator feeds and fires a pushover
// critical alert for every new posting that matches my filters.
// runs as a short-lived process under a systemd timer, so
// one invocation = one poll tick, then exit.
//
// safety rails built in: a fresh/empty db triggers seed mode (mark
// everything seen, alert nothing), and jobs whose alert failed are
// retried on later ticks via the notified_at IS NULL backlog
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"golang.org/x/sync/errgroup"

	"github.com/Lavanic/crier/internal/config"
	"github.com/Lavanic/crier/internal/filter"
	"github.com/Lavanic/crier/internal/notify"
	"github.com/Lavanic/crier/internal/sources"
	"github.com/Lavanic/crier/internal/store"
)

// cap on in-flight http requests. 40 keeps the 125-source fan-out to
// ~3 waves so the last wave's matches aren't waiting on the first,
// still gentle on the cdn-fronted apis
const maxConcurrent = 40

// a tick has to finish well before systemd fires the next one at 30s.
// sources that miss the deadline get cut off, not waited on
const tickTimeout = 25 * time.Second

// how far back the seen-but-never-alerted backlog reaches. anything
// older than this that still failed to alert is stale enough to drop
const backlogWindow = 24 * time.Hour

// more matches than this in one tick means something bulk-merged
// (aggregator catch-up, new source). individual alerts drop to normal
// priority and ONE emergency digest fires, instead of N sirens each
// re-buzzing every 30s until individually acked
const maxEmergencyPerTick = 3

// an alert plus its dedup key. backlog rows can't recompute the key
// from parts (JobID isn't stored as a column) so it rides along
type alert struct {
	job sources.Job
	key string
}

func main() {
	dryRun := flag.Bool("dry-run", false, "log would-notify jobs instead of alerting")
	configPath := flag.String("config", "config.yaml", "path to config file")
	pretty := flag.Bool("pretty", false, "human-readable logs instead of json")
	flag.Parse()

	// the pushover lib does its POSTs through http.DefaultClient,
	// which ships with NO timeout. a blackholed connection would hang
	// the tick forever (and systemd oneshot waits forever by default).
	// nothing else in this process uses the default client
	http.DefaultClient.Timeout = 20 * time.Second

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

	// empty jobs table = fresh host. mark everything seen, alert
	// nothing, so a first tick can't fire hundreds of sirens
	jobCount, err := st.CountJobs()
	if err != nil {
		return err
	}
	seedRun := jobCount == 0
	if seedRun {
		log.Warn("empty database, this tick seeds without alerting")
	}

	now := time.Now()
	var alerts []alert

	// backlog first: jobs seen on earlier ticks whose alert never went
	// out (pushover down, tick cut off, crash). re-run the filter so
	// only alert-worthy ones come back
	if !seedRun {
		pending, err := st.UnnotifiedSince(now.Add(-backlogWindow))
		if err != nil {
			return err
		}
		for _, p := range pending {
			if f.Match(p.Job.Title) {
				alerts = append(alerts, alert{job: p.Job, key: p.Key})
			}
		}
		if len(alerts) > 0 {
			log.Info("retrying unnotified backlog", "count", len(alerts))
		}
	}

	srcs := buildSources(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), tickTimeout)
	defer cancel()

	// fan out: each source fetches concurrently, capped by the
	// semaphore. matches fan IN to the alerts slice so notifications
	// go out serially afterwards (pushover allows max 2 concurrent)
	g, ctx := errgroup.WithContext(ctx)
	sem := make(chan struct{}, maxConcurrent)
	var mu sync.Mutex
	var fetchOK atomic.Int64

	for _, src := range srcs {
		g.Go(func() error {
			select {
			case sem <- struct{}{}: // take a slot
			case <-ctx.Done():
				return ctx.Err()
			}
			defer func() { <-sem }() // give it back

			// only slow-cadence sources (github feeds) need the poll
			// table, skipping it for the rest saves ~51 db writes and
			// fsyncs per tick (matters on sd cards)
			gated := src.MinInterval() > 0
			if gated {
				last, ok, err := st.LastPolled(src.Name())
				if err != nil {
					return err
				}
				if ok && now.Sub(last) < src.MinInterval() {
					return nil
				}
			}

			jobs, err := src.Fetch(ctx)
			if err != nil {
				// one dead board must not kill the tick, warn and move on
				log.Warn("source failed", "source", src.Name(), "err", err)
				return nil
			}
			fetchOK.Add(1)
			if len(jobs) == 0 {
				log.Warn("source returned 0 jobs", "source", src.Name())
			}
			if gated {
				if err := st.SetPolled(src.Name(), now); err != nil {
					return err
				}
			}

			newCount, matchCount := 0, 0
			for _, j := range jobs {
				// mark seen BEFORE filtering so loosening the filter
				// later can't re-alert old postings
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
					alerts = append(alerts, alert{job: j, key: j.DedupKey()})
					mu.Unlock()
				}
			}
			log.Info("source ok",
				"source", src.Name(), "jobs", len(jobs),
				"new", newCount, "matched", matchCount)
			return nil
		})
	}
	// do NOT bail here on error: alerts collected from sources that
	// finished are real and already marked seen. dropping them now
	// would lose them until the backlog resends. notify first,
	// surface the error in the exit code after
	fetchErr := g.Wait()
	if fetchErr != nil {
		log.Error("fan-out aborted early", "err", fetchErr)
	}
	if fetchOK.Load() == 0 && len(srcs) > 0 {
		// every fetch failed: network is dead or we're blocked. the
		// tick must exit nonzero so the dead-man switch notices
		fetchErr = errors.Join(fetchErr, errors.New("zero sources fetched successfully"))
	}

	notifyErr := dispatch(log, st, notifier, cfg.DisplayNames, alerts, dryRun, seedRun)

	log.Info("tick done",
		"sources", len(srcs), "alerts", len(alerts), "dry_run", dryRun,
		"seed", seedRun, "elapsed_ms", time.Since(start).Milliseconds())
	return errors.Join(fetchErr, notifyErr)
}

// dispatch sends (or logs, or suppresses) the collected alerts and
// stamps notified_at on every alert that was handled. jobs that fail
// to send stay unstamped and ride the backlog next tick
func dispatch(log *slog.Logger, st *store.Store, notifier *notify.Notifier,
	names map[string]string, alerts []alert, dryRun, seedRun bool) error {

	stamp := func(key string) {
		if err := st.MarkNotified(key, time.Now()); err != nil {
			log.Error("mark notified failed", "job", key, "err", err)
		}
	}

	if dryRun || seedRun {
		for _, a := range alerts {
			log.Info("suppressed (dry-run/seed), would notify",
				"company", a.job.Company, "title", a.job.Title,
				"location", a.job.Location, "url", a.job.URL)
			// stamped so these don't flood the backlog on the next
			// real tick, a dry or seed run counts as handled
			stamp(a.key)
		}
		return nil
	}

	// burst mode: individual alerts go quiet, one siren summarizes
	priority := notify.Emergency
	if len(alerts) > maxEmergencyPerTick {
		priority = notify.Normal
		log.Warn("alert burst, downgrading individuals and sending one digest", "count", len(alerts))
	}

	var failed int
	for _, a := range alerts {
		j := a.job
		j.Company = displayName(names, j.Company)
		if err := notifier.Notify(j, priority); err != nil {
			// loud but not fatal, unstamped rows retry via the backlog
			log.Error("notify failed", "job", a.key, "err", err)
			failed++
			continue
		}
		stamp(a.key)
		log.Info("notified", "company", j.Company, "title", j.Title, "url", j.URL)
	}

	if priority == notify.Normal && len(alerts) > failed {
		title := fmt.Sprintf("%d new job matches", len(alerts))
		if err := notifier.Send(title, digestBody(names, alerts), notify.Emergency); err != nil {
			log.Error("digest send failed", "err", err)
			failed++
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d notification(s) failed, will retry next tick", failed)
	}
	return nil
}

// digestBody summarizes a burst: "Stripe ×3, Ramp, Anduril +12 more"
func digestBody(names map[string]string, alerts []alert) string {
	counts := map[string]int{}
	var order []string
	for _, a := range alerts {
		name := displayName(names, a.job.Company)
		if counts[name] == 0 {
			order = append(order, name)
		}
		counts[name]++
	}
	sort.SliceStable(order, func(i, k int) bool { return counts[order[i]] > counts[order[k]] })

	var parts []string
	for i, name := range order {
		if i == 6 {
			parts = append(parts, fmt.Sprintf("+%d more", len(order)-i))
			break
		}
		if counts[name] > 1 {
			parts = append(parts, fmt.Sprintf("%s ×%d", name, counts[name]))
		} else {
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, ", ") + " · open pushover for links"
}

// displayName prettifies a company for the alert title. explicit map
// first, then capitalize single all-lowercase words (stripe -> Stripe),
// anything already styled (Google, eBay) passes through untouched
func displayName(names map[string]string, company string) string {
	if v, ok := names[company]; ok {
		return v
	}
	if company != "" && company == strings.ToLower(company) && !strings.ContainsAny(company, " -.") {
		r := []rune(company)
		r[0] = unicode.ToUpper(r[0])
		return string(r)
	}
	return company
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
