package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// trimmed reels_media response: a job link, a plain photo, his
// linktree, and a repost of the first link
const storiesSample = `{
  "reels_media": [
    {
      "id": "50350974961",
      "items": [
        {
          "pk": "3961134710446842192",
          "taken_at": 1786412345,
          "story_link_stickers": [
            {
              "x": 0.5,
              "y": 0.81916606,
              "story_link": {
                "url": "https://l.instagram.com/?u=https%3A%2F%2Fcareers.peak6.com%2Fjobs%2Fbusiness-operation-services%2Fchicago-illinois-united-states-of-america%2Ftrading-bootcamp-micro-internship-summer-2027%2FJR105057%3Futm_campaign%3Dzero2sudo&e=AUA24kG"
              },
              "id": "18111522385978947"
            }
          ]
        },
        {
          "pk": "3961134710446842193",
          "taken_at": 1786412999
        },
        {
          "pk": "3961134710446842194",
          "story_link_stickers": [
            {
              "story_link": {
                "url": "https://l.instagram.com/?u=https%3A%2F%2Fjob-boards.greenhouse.io%2Fchicagotradingcampus%2Fjobs%2F4716937005"
              }
            },
            {
              "story_link": {
                "url": "https://l.instagram.com/?u=https%3A%2F%2Flinktr.ee%2Fzero2sudo"
              }
            }
          ]
        },
        {
          "pk": "3961134710446842195",
          "story_link_stickers": [
            {
              "story_link": {
                "url": "https://l.instagram.com/?u=https%3A%2F%2Fcareers.peak6.com%2Fjobs%2Fbusiness-operation-services%2Fchicago-illinois-united-states-of-america%2Ftrading-bootcamp-micro-internship-summer-2027%2FJR105057%3Futm_source%3Dstory2"
              }
            }
          ]
        }
      ]
    }
  ]
}`

func testSession() Session {
	return Session{SessionID: "sess-abc", DsUserID: "50350974961", CSRFToken: "csrf-xyz"}
}

func newTestStories(t *testing.T, srv *httptest.Server) *InstagramStories {
	t.Helper()
	s := NewInstagramStories(NewInstagramClient(), "zero2sudo", "50350974961",
		testSession(), 5*time.Minute, 0)
	s.baseURL = srv.URL
	return s
}

func TestInstagramFetchMapsLinkStickers(t *testing.T) {
	var gotPath, gotCookie, gotAppID, gotReelIDs string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotReelIDs = r.URL.Query().Get("reel_ids")
		gotCookie = r.Header.Get("Cookie")
		gotAppID = r.Header.Get("X-IG-App-ID")
		w.Write([]byte(storiesSample))
	}))
	defer srv.Close()

	jobs, err := newTestStories(t, srv).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if gotPath != "/api/v1/feed/reels_media/" {
		t.Errorf("path = %q", gotPath)
	}
	if gotReelIDs != "50350974961" {
		t.Errorf("reel_ids = %q", gotReelIDs)
	}
	// 400s without the app id, empty logged-out reel without the cookie
	if !strings.Contains(gotCookie, "sessionid=sess-abc") {
		t.Errorf("cookie = %q, want the session id in it", gotCookie)
	}
	if gotAppID != instagramAppID {
		t.Errorf("app id = %q", gotAppID)
	}

	// linktree dropped, stickerless story contributes nothing, and the
	// peak6 repost collapses onto the first, canonical url is the key
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2: %+v", len(jobs), jobs)
	}
	if jobs[0].Company != "peak6" {
		t.Errorf("first company = %q, want peak6", jobs[0].Company)
	}
	if jobs[0].Title != "Trading Bootcamp Micro Internship Summer 2027" {
		t.Errorf("first title = %q", jobs[0].Title)
	}
	if jobs[1].Company != "chicagotradingcampus" {
		t.Errorf("second company = %q", jobs[1].Company)
	}
	for _, j := range jobs {
		if j.Source != "instagram:zero2sudo" {
			t.Errorf("source = %q", j.Source)
		}
	}
}

// the graphql envelope is what a token-scraping transport returns,
// so swapping transports later must not need a second parser
func TestInstagramFetchAcceptsTheGraphQLEnvelope(t *testing.T) {
	const graphqlSample = `{
	  "data": {
	    "xdt_api__v1__feed__reels_media__connection": {
	      "edges": [
	        {"node": {"items": [
	          {"story_link_stickers": [
	            {"story_link": {"url": "https://job-boards.greenhouse.io/appian/jobs/7951022"}}
	          ]}
	        ]}}
	      ]
	    }
	  }
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(graphqlSample))
	}))
	defer srv.Close()

	jobs, err := newTestStories(t, srv).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Company != "appian" {
		t.Fatalf("got %+v, want one appian job", jobs)
	}
}

// a dead cookie still exits 0 and leaves the dead-man switch green,
// so the error text is the only thing that says the dragnet stopped
func TestInstagramFetchExplainsAuthFailure(t *testing.T) {
	tests := []struct {
		name string
		code int
		want string
	}{
		{"expired session", http.StatusUnauthorized, "session cookie rejected"},
		{"checkpointed", http.StatusForbidden, "session cookie rejected"},
		{"throttled", http.StatusTooManyRequests, "rate limited"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				w.WriteHeader(tt.code)
			}))
			defer srv.Close()

			_, err := newTestStories(t, srv).Fetch(context.Background())
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to mention %q", err, tt.want)
			}
			// the point of the no-retry client, one 429 must not become
			// four requests at instagram
			if hits != 1 {
				t.Errorf("made %d requests, want exactly 1 (no retries)", hits)
			}
		})
	}
}

func TestInstagramFetchWithoutSessionDoesNotCallOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not have made a request without a session cookie")
	}))
	defer srv.Close()

	s := NewInstagramStories(NewInstagramClient(), "zero2sudo", "50350974961",
		Session{}, 5*time.Minute, 0)
	s.baseURL = srv.URL
	if _, err := s.Fetch(context.Background()); err == nil {
		t.Fatal("expected an error when no session is configured")
	}
}

// a quiet night is not a failure, he doesn't post around the clock
func TestInstagramFetchEmptyReelIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"reels_media":[{"id":"50350974961","items":[]}]}`))
	}))
	defer srv.Close()

	jobs, err := newTestStories(t, srv).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Errorf("got %d jobs from an empty reel", len(jobs))
	}
}

// jitter keeps the polls off a grid, but has to stay in the window
// or the cadence stops meaning anything
func TestInstagramMinIntervalJitters(t *testing.T) {
	s := NewInstagramStories(NewInstagramClient(), "zero2sudo", "1",
		testSession(), 5*time.Minute, 150*time.Second)
	seen := make(map[time.Duration]bool)
	for range 50 {
		d := s.MinInterval()
		if d < 5*time.Minute || d >= 5*time.Minute+150*time.Second {
			t.Fatalf("MinInterval() = %v, outside the window", d)
		}
		seen[d] = true
	}
	if len(seen) < 2 {
		t.Error("MinInterval never varied, the polls would land on a grid")
	}

	// zero jitter has to stay exact, that's the escape hatch
	fixed := NewInstagramStories(NewInstagramClient(), "z", "1", testSession(), time.Minute, 0)
	if got := fixed.MinInterval(); got != time.Minute {
		t.Errorf("unjittered MinInterval() = %v, want 1m", got)
	}
}
