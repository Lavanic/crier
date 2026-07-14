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
	"regexp"
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

// how far back to check if an alert was the same posting on another
// portal, a week covers every repeat seen in the audit
const crossPostWindow = 7 * 24 * time.Hour

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
	f, err := filter.New(filter.Config{
		Include:                cfg.Filter.Include,
		ExcludeKeywords:        cfg.Filter.ExcludeKeywords,
		ExcludePatterns:        cfg.Filter.ExcludePatterns,
		ExcludeLocations:       cfg.Filter.ExcludeLocations,
		ExcludeCategories:      cfg.Filter.ExcludeCategories,
		CategoryRescueKeywords: cfg.Filter.CategoryRescueKeywords,
	})
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
			if f.Match(p.Job) {
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
				if f.Match(j) {
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

	alerts = dropCrossPosts(log, st, cfg.DisplayNames, alerts)

	notifyErr := dispatch(log, st, notifier, cfg.DisplayNames,
		sirenSet(cfg.PriorityCompanies), alerts, dryRun, seedRun)

	log.Info("tick done",
		"sources", len(srcs), "alerts", len(alerts), "dry_run", dryRun,
		"seed", seedRun, "elapsed_ms", time.Since(start).Milliseconds())
	return errors.Join(fetchErr, notifyErr)
}

// dropCrossPosts suppresses alerts the phone already got through a
// different portal or feed. best-effort, a db hiccup keeps every alert
func dropCrossPosts(log *slog.Logger, st *store.Store, names map[string]string, alerts []alert) []alert {
	if len(alerts) == 0 {
		return alerts
	}
	recent, err := st.NotifiedSince(time.Now().Add(-crossPostWindow))
	if err != nil {
		log.Error("cross-post lookup failed, keeping all alerts", "err", err)
		return alerts
	}
	seen := make(map[string]bool, len(recent))
	for _, r := range recent {
		seen[crossPostKey(names, r.Company, r.URL)] = true
	}
	kept := alerts[:0]
	for _, a := range alerts {
		k := crossPostKey(names, a.job.Company, a.job.URL)
		if seen[k] {
			log.Info("suppressed cross-post duplicate",
				"company", a.job.Company, "title", a.job.Title, "url", a.job.URL)
			if err := st.MarkNotified(a.key, time.Now()); err != nil {
				log.Error("mark notified failed", "job", a.key, "err", err)
			}
			continue
		}
		seen[k] = true
		kept = append(kept, a)
	}
	return kept
}

// crossPostKey is company + requisition id. display map then first
// word only, so "andurilindustries" and simplify's "Anduril" agree
func crossPostKey(names map[string]string, company, rawURL string) string {
	c := strings.ToLower(strings.TrimSpace(displayName(names, company)))
	c = strings.TrimPrefix(c, "the ")
	if i := strings.IndexByte(c, ' '); i > 0 {
		c = c[:i]
	}
	c = strings.TrimRight(c, ",.")
	return c + "|" + reqID(rawURL)
}

// workday's repost counter (_R54894-1). only 1-2 digits so ids with a
// longer numeric tail like REQ-12218 keep theirs
var repostSuffix = regexp.MustCompile(`-\d{1,2}$`)

// google sometimes appends a title slug to the numeric id
// (…/results/143333237156913862-software-engineer-ii), 8+ digits so
// year-leading slugs don't trip it
var leadingID = regexp.MustCompile(`^\d{8,}`)

// reqID pulls the requisition id from an apply url: last path segment,
// after the last underscore on workday. tries the parent segment too
// since apple and ashby put the id one level up (/details/{id}/{slug},
// /{uuid}/application). no digits anywhere = use the full url
func reqID(rawURL string) string {
	u := rawURL
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	u = strings.TrimRight(u, "/")
	for range 2 {
		i := strings.LastIndexByte(u, '/')
		seg := u[i+1:]
		u = u[:max(i, 0)]
		if j := strings.LastIndexByte(seg, '_'); j >= 0 {
			seg = seg[j+1:]
		}
		if m := leadingID.FindString(seg); m != "" {
			return m
		}
		seg = repostSuffix.ReplaceAllString(seg, "")
		if strings.ContainsAny(seg, "0123456789") {
			return strings.ToLower(seg)
		}
	}
	return rawURL
}

// sirenSet turns the priority_companies list into a lowercase lookup
func sirenSet(companies []string) map[string]bool {
	set := make(map[string]bool, len(companies))
	for _, c := range companies {
		set[strings.ToLower(strings.TrimSpace(c))] = true
	}
	return set
}

// dispatch sends (or logs, or suppresses) the collected alerts and
// stamps notified_at on every alert that was handled. jobs that fail
// to send stay unstamped and ride the backlog next tick.
// priority-company matches go out as sirens (pushover emergency,
// through dnd), everything else as a normal ping
func dispatch(log *slog.Logger, st *store.Store, notifier *notify.Notifier,
	names map[string]string, siren map[string]bool, alerts []alert, dryRun, seedRun bool) error {

	stamp := func(key string) {
		if err := st.MarkNotified(key, time.Now()); err != nil {
			log.Error("mark notified failed", "job", key, "err", err)
		}
	}
	sirens := 0
	for _, a := range alerts {
		if siren[strings.ToLower(displayName(names, a.job.Company))] {
			sirens++
		}
	}

	if dryRun || seedRun {
		for _, a := range alerts {
			log.Info("suppressed (dry-run/seed), would notify",
				"company", a.job.Company, "title", a.job.Title,
				"location", a.job.Location, "url", a.job.URL,
				"siren", siren[strings.ToLower(displayName(names, a.job.Company))])
			// stamped so these don't flood the backlog on the next
			// real tick, a dry or seed run counts as handled
			stamp(a.key)
		}
		return nil
	}

	// burst mode: too many sirens at once collapse into one emergency
	// digest and the individuals all drop to normal
	burst := sirens > maxEmergencyPerTick
	if burst {
		log.Warn("siren burst, downgrading individuals and sending one digest", "count", sirens)
	}

	var failed int
	for _, a := range alerts {
		j := a.job
		j.Company = displayName(names, j.Company)
		p := notify.Normal
		if !burst && siren[strings.ToLower(j.Company)] {
			p = notify.Emergency
		}
		if err := notifier.Notify(j, p); err != nil {
			// loud but not fatal, unstamped rows retry via the backlog
			log.Error("notify failed", "job", a.key, "err", err)
			failed++
			continue
		}
		stamp(a.key)
		log.Info("notified", "company", j.Company, "title", j.Title,
			"siren", p == notify.Emergency, "url", j.URL)
	}

	if burst && len(alerts) > failed {
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
	for _, feed := range cfg.Sources.Google {
		srcs = append(srcs, sources.NewGoogleCareers(
			client, feed.Name, feed.URL,
			time.Duration(feed.MinIntervalSec)*time.Second))
	}
	for _, feed := range cfg.Sources.Apple {
		srcs = append(srcs, sources.NewAppleJobs(
			client, feed.Name, feed.URL,
			time.Duration(feed.MinIntervalSec)*time.Second))
	}
	return srcs
}
