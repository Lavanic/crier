package sources

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

// greenhouse/lever/ashby links are bare ids, so titleFromURL finds
// nothing. all three have a public read api crier already speaks, so
// just ask the board. keeps the untitled path rare instead of normal

// without a cache we'd ask greenhouse about the same posting every
// 5 min for the 24h his story is up. store implements it, nil is fine
type TitleCache interface {
	LookupTitle(url string) (title, location string, ok bool, err error)
	SaveTitle(url, title, location string) error
}

// per-fetch budget. cache hits don't count, so it only bites on the
// first poll after a big story dump
const maxEnrichPerFetch = 8

// hides the latency inside the tick without hammering boards we
// aren't otherwise polling
const enrichConcurrency = 4

type Enricher struct {
	client *http.Client
	cache  TitleCache
	// unit tests point these at a fake local server
	greenhouseBase string
	leverBase      string
	ashbyBase      string
}

func NewEnricher(cache TitleCache) *Enricher {
	return &Enricher{
		client:         NewEnrichClient(),
		cache:          cache,
		greenhouseBase: "https://boards-api.greenhouse.io",
		leverBase:      "https://api.lever.co",
		ashbyBase:      "https://api.ashbyhq.com",
	}
}

type pendingLookup struct {
	idx   int
	kind  string
	slug  string
	jobID string
}

// fills missing titles in place. a job we can't title stays empty
// and dies at the include gate, which is the point
func (e *Enricher) Fill(ctx context.Context, jobs []Job) {
	var todo []pendingLookup
	for i := range jobs {
		if jobs[i].Title != "" {
			continue
		}
		u, err := url.Parse(jobs[i].URL)
		if err != nil {
			continue
		}
		// empty kind means no board api to ask, those fall through to
		// scraping the posting's own page for a title
		kind, slug, jobID := boardSlug(u)
		if slug == "" || jobID == "" {
			kind, slug, jobID = "", "", ""
		}
		// serial on purpose, sqlite runs one connection and the rest of
		// the tick is already writing through MarkSeen
		if e.cache != nil {
			if title, loc, ok, err := e.cache.LookupTitle(jobs[i].URL); err == nil && ok {
				apply(&jobs[i], title, loc)
				continue
			}
		}
		todo = append(todo, pendingLookup{i, kind, slug, jobID})
		if len(todo) == maxEnrichPerFetch {
			break
		}
	}
	if len(todo) == 0 {
		return
	}

	type result struct {
		title, location string
		// a 404 is gone for good and worth remembering, a 500 is not
		cacheable bool
	}
	results := make([]result, len(todo))

	var wg sync.WaitGroup
	sem := make(chan struct{}, enrichConcurrency)
	for i, p := range todo {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			title, loc, err := e.lookup(ctx, jobs[p.idx].URL, p.kind, p.slug, p.jobID)
			if err != nil {
				var se *statusError
				results[i].cacheable = errors.As(err, &se) && se.Code == http.StatusNotFound
				return
			}
			results[i] = result{title: title, location: loc, cacheable: true}
		}()
	}
	wg.Wait()

	for i, p := range todo {
		r := results[i]
		apply(&jobs[p.idx], r.title, r.location)
		if e.cache != nil && r.cacheable {
			// best effort, a failed write just means we ask again
			_ = e.cache.SaveTitle(jobs[p.idx].URL, r.title, r.location)
		}
	}
}

// the board's answer beats anything guessed off the url slug
func apply(j *Job, title, location string) {
	if title != "" {
		j.Title = title
	}
	if location != "" {
		j.Location = location
	}
}

func (e *Enricher) lookup(ctx context.Context, rawURL, kind, slug, jobID string) (title, location string, err error) {
	if kind == "" {
		// no board api for this host, read the posting page's own
		// metadata. beats the url slug too, it keeps the punctuation
		// ("IS&T", "Micro-Internship") that a slug flattens
		t, err := e.pageTitle(ctx, rawURL)
		return t, "", err
	}
	slug, jobID = url.PathEscape(slug), url.PathEscape(jobID)
	switch kind {
	case "greenhouse":
		var job struct {
			Title    string `json:"title"`
			Location struct {
				Name string `json:"name"`
			} `json:"location"`
		}
		endpoint := fmt.Sprintf("%s/v1/boards/%s/jobs/%s", e.greenhouseBase, slug, jobID)
		if err := getJSON(ctx, e.client, endpoint, &job); err != nil {
			return "", "", err
		}
		return job.Title, job.Location.Name, nil

	case "lever":
		var p leverPosting
		endpoint := fmt.Sprintf("%s/v0/postings/%s/%s", e.leverBase, slug, jobID)
		if err := getJSON(ctx, e.client, endpoint, &p); err != nil {
			return "", "", err
		}
		return p.Text, p.Categories.Location, nil

	case "ashby":
		// no single-posting endpoint, so pull the board and index it.
		// still one request, just a bigger one
		var resp ashbyResponse
		endpoint := fmt.Sprintf("%s/posting-api/job-board/%s", e.ashbyBase, slug)
		if err := getJSON(ctx, e.client, endpoint, &resp); err != nil {
			return "", "", err
		}
		for _, j := range resp.Jobs {
			if j.ID == jobID {
				return j.Title, j.Location, nil
			}
		}
		return "", "", fmt.Errorf("posting %s is not on ashby board %s", jobID, slug)
	}
	// workday needs a site path the url doesn't always carry
	return "", "", fmt.Errorf("no title lookup for %s boards", kind)
}

// og:title, with the attribute order both ways round since plenty of
// sites put content first
var (
	ogTitleRe    = regexp.MustCompile(`(?is)<meta[^>]+property=["']og:title["'][^>]+content=["']([^"']*)["']`)
	ogTitleAltRe = regexp.MustCompile(`(?is)<meta[^>]+content=["']([^"']*)["'][^>]+property=["']og:title["']`)
	titleTagRe   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	spaceRe      = regexp.MustCompile(`\s+`)
)

// only the head matters and job pages can run megabytes, so stop early
const maxTitleBytes = 256 << 10

func (e *Enricher) pageTitle(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := e.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &statusError{URL: rawURL, Code: resp.StatusCode, Status: resp.Status}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTitleBytes))
	if err != nil {
		return "", err
	}
	head := string(body)

	og := firstGroup(head, ogTitleRe, ogTitleAltRe)
	tag := firstGroup(head, titleTagRe)
	title := pickTitle(clean(og), clean(tag))
	if alphaTokens(title) < 2 || junkTitles[strings.ToLower(title)] {
		return "", fmt.Errorf("no usable title on %s", rawURL)
	}
	return title, nil
}

func firstGroup(s string, res ...*regexp.Regexp) string {
	for _, re := range res {
		if m := re.FindStringSubmatch(s); len(m) > 1 && m[1] != "" {
			return m[1]
		}
	}
	return ""
}

func clean(s string) string {
	return strings.TrimSpace(spaceRe.ReplaceAllString(html.UnescapeString(s), " "))
}

// og:title is usually the cleaner of the two, but some sites put the
// company there and the real role only in <title> (d.e.shaw does). if
// <title> ends with a separator then og, og is the site name
func pickTitle(og, tag string) string {
	if og == "" {
		return stripSiteSuffix(tag)
	}
	if tag == "" {
		return stripSiteSuffix(og)
	}
	for _, sep := range []string{" | ", " - ", " – ", " @ ", " · ", ": "} {
		if strings.HasSuffix(tag, sep+og) {
			// og IS the site name here, so cut exactly that much rather
			// than guessing where the title ends
			return strings.TrimSpace(strings.TrimSuffix(tag, sep+og))
		}
	}
	return stripSiteSuffix(og)
}

// lop off a trailing "| Acme Careers" when we're only guessing. three
// words max, past that it's more likely part of the role
// ("SWE - Infra | Platform Reliability And Observability Team")
func stripSiteSuffix(t string) string {
	for _, sep := range []string{" | ", " @ ", " · "} {
		if i := strings.LastIndex(t, sep); i > 0 {
			if tail := t[i+len(sep):]; len(strings.Fields(tail)) <= 3 {
				t = t[:i]
			}
		}
	}
	return strings.TrimSpace(t)
}

// a title that names the site instead of the role. these sail past the
// excludes and would alert on whatever the page happened to be
var junkTitles = map[string]bool{
	"careers": true, "career": true, "jobs": true, "job": true,
	"job search": true, "search jobs": true, "job details": true,
	"job detail": true, "job description": true, "home": true,
	"search": true, "open positions": true, "our openings": true,
	"job opportunities": true, "current openings": true,
	"access denied": true, "just a moment": true, "attention required": true,
}
