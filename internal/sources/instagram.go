package sources

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"time"
)

// link stickers off zero2sudo's story, read from instagram's own api:
//   GET /api/v1/feed/reels_media/?reel_ids={user id}
// session cookie + app id header, no headless browser.

// the site sends these on every call, the endpoint 400s without the
// app id. asbd is a build stamp, any current value passes
const (
	instagramAppID  = "936619743392459"
	instagramASBDID = "129477"
)

// Session is the burner's cookies, lifted from a logged-in browser.
// lives in config.local.yaml with the pushover creds
type Session struct {
	SessionID string
	DsUserID  string
	CSRFToken string
}

func (s Session) ok() bool { return s.SessionID != "" }

// KV is the bit of the store this source needs. instagram hands back a
// www-claim to echo on the next call, and the process exits every tick
type KV interface {
	GetKV(key string) (string, error)
	SetKV(key, value string) error
}

type InstagramStories struct {
	client      *http.Client
	name        string
	userID      string
	session     Session
	minInterval time.Duration
	jitter      time.Duration
	hours       ActiveHours
	enricher    *Enricher // nil just means no ats title lookup
	kv          KV        // nil just means we always send claim 0
	// unit tests point this at a fake local server
	baseURL string
}

var (
	_ Source   = (*InstagramStories)(nil)
	_ Windowed = (*InstagramStories)(nil)
)

func NewInstagramStories(client *http.Client, name, userID string, session Session,
	minInterval, jitter time.Duration) *InstagramStories {
	return &InstagramStories{
		client:      client,
		name:        name,
		userID:      userID,
		session:     session,
		minInterval: minInterval,
		jitter:      jitter,
		baseURL:     "https://www.instagram.com",
	}
}

// not in the constructor: these need the db, url parsing has to stay
// testable without one
func (s *InstagramStories) SetEnricher(e *Enricher)      { s.enricher = e }
func (s *InstagramStories) SetKV(kv KV)                  { s.kv = kv }
func (s *InstagramStories) SetActiveHours(h ActiveHours) { s.hours = h }

func (s *InstagramStories) Name() string { return "instagram:" + s.name }

// he posts on a human schedule, so polling at 4am buys nothing and
// looks exactly like what it is
func (s *InstagramStories) ActiveAt(t time.Time) bool { return s.hours.Contains(t) }

// fresh jitter every call. main compares this against the last poll
// time, so a new number each tick scatters the polls off a grid
func (s *InstagramStories) MinInterval() time.Duration {
	if s.jitter <= 0 {
		return s.minInterval
	}
	return s.minInterval + rand.N(s.jitter)
}

// no story id or timestamp on purpose, instagram flips those between
// string and number. the canonical link url is the dedup key anyway
type reelItem struct {
	StoryLinkStickers []struct {
		StoryLink struct {
			URL string `json:"url"`
		} `json:"story_link"`
	} `json:"story_link_stickers"`
	// older stories attach the link as a swipe-up cta instead
	StoryCTA []struct {
		Links []struct {
			WebURI string `json:"webUri"`
		} `json:"links"`
	} `json:"story_cta"`
}

func (i reelItem) links() []string {
	var out []string
	for _, s := range i.StoryLinkStickers {
		if s.StoryLink.URL != "" {
			out = append(out, s.StoryLink.URL)
		}
	}
	for _, c := range i.StoryCTA {
		for _, l := range c.Links {
			if l.WebURI != "" {
				out = append(out, l.WebURI)
			}
		}
	}
	return out
}

type reel struct {
	Items []reelItem `json:"items"`
}

// three envelopes, same item bodies. decoding all of them means a
// shape flip (or the graphql transport) needs no second parser
type reelsResponse struct {
	ReelsMedia []reel          `json:"reels_media"`
	Reels      map[string]reel `json:"reels"`
	Data       struct {
		Connection struct {
			Edges []struct {
				Node reel `json:"node"`
			} `json:"edges"`
		} `json:"xdt_api__v1__feed__reels_media__connection"`
	} `json:"data"`
}

func (r reelsResponse) items() []reelItem {
	var out []reelItem
	for _, m := range r.ReelsMedia {
		out = append(out, m.Items...)
	}
	for _, m := range r.Reels {
		out = append(out, m.Items...)
	}
	for _, e := range r.Data.Connection.Edges {
		out = append(out, e.Node.Items...)
	}
	return out
}

// mirror what chrome actually sends on an /api/v1 xhr. we used to send
// X-Requested-With, which the web app never sends, so the one header we
// added by hand was the one that gave us away
func (s *InstagramStories) headers(claim string) func(*http.Request) {
	return func(req *http.Request) {
		cookie := "sessionid=" + s.session.SessionID
		if s.session.DsUserID != "" {
			cookie += "; ds_user_id=" + s.session.DsUserID
		}
		if s.session.CSRFToken != "" {
			cookie += "; csrftoken=" + s.session.CSRFToken
			req.Header.Set("X-CSRFToken", s.session.CSRFToken)
		}
		req.Header.Set("Cookie", cookie)
		req.Header.Set("X-IG-App-ID", instagramAppID)
		req.Header.Set("X-ASBD-ID", instagramASBDID)
		req.Header.Set("X-IG-WWW-Claim", claim)
		req.Header.Set("Referer", "https://www.instagram.com/")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		// browser-only, a bare http client omits every one of them
		req.Header.Set("Sec-Ch-Ua", `"Chromium";v="130", "Google Chrome";v="130", "Not?A_Brand";v="99"`)
		req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
		req.Header.Set("Sec-Ch-Ua-Platform", `"macOS"`)
		req.Header.Set("Sec-Fetch-Dest", "empty")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
}

// the real client echoes back whatever the last response set. never
// echoing it is the kind of thing only a script does
const claimKeyPrefix = "ig_www_claim:"

func (s *InstagramStories) loadClaim() string {
	if s.kv == nil {
		return "0"
	}
	v, err := s.kv.GetKV(claimKeyPrefix + s.name)
	if err != nil || v == "" {
		return "0"
	}
	return v
}

func (s *InstagramStories) saveClaim(h http.Header) {
	v := h.Get("X-IG-Set-WWW-Claim")
	if v == "" || s.kv == nil {
		return
	}
	// best effort, a lost claim is not worth failing a fetch over
	_ = s.kv.SetKV(claimKeyPrefix+s.name, v)
}

func (s *InstagramStories) Fetch(ctx context.Context) ([]Job, error) {
	if !s.session.ok() {
		return nil, fmt.Errorf("instagram %s: no session cookie, add one to config.local.yaml", s.name)
	}
	endpoint := fmt.Sprintf("%s/api/v1/feed/reels_media/?reel_ids=%s",
		s.baseURL, url.QueryEscape(s.userID))

	var resp reelsResponse
	if err := getJSONResp(ctx, s.client, endpoint, &resp,
		s.saveClaim, s.headers(s.loadClaim())); err != nil {
		return nil, s.describe(err)
	}

	// zero links is normal, he doesn't post around the clock
	var jobs []Job
	seen := make(map[string]bool)
	for _, item := range resp.items() {
		for _, raw := range item.links() {
			job, ok := jobFromStoryLink(s.name, raw)
			if !ok || seen[job.JobID] {
				continue
			}
			seen[job.JobID] = true
			jobs = append(jobs, job)
		}
	}

	// bare ats links carry no role words, go ask the board
	if s.enricher != nil {
		s.enricher.Fill(ctx, jobs)
	}
	return jobs, nil
}

// a dead cookie is otherwise invisible: the tick still exits 0 and
// the dead-man switch stays green. the error text is the only warning
func (s *InstagramStories) describe(err error) error {
	var se *statusError
	if errors.As(err, &se) {
		switch se.Code {
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Errorf("instagram %s: session cookie rejected (%d), log the burner back in and update config.local.yaml", s.name, se.Code)
		case http.StatusBadRequest:
			// what a "we suspect automated behavior" challenge looks like
			// from out here. it stays 400 until someone clears it in-app
			return fmt.Errorf("instagram %s: challenge (400), log the burner in through a browser and clear the warning", s.name)
		case http.StatusTooManyRequests:
			return fmt.Errorf("instagram %s: rate limited, widen min_interval_seconds if this repeats", s.name)
		}
	}
	return fmt.Errorf("instagram %s: %w", s.name, err)
}
