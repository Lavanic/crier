package notify

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gregdel/pushover"

	"github.com/Lavanic/crier/internal/sources"
)

// the pushover lib exposes its base url as a package var, point it at
// a fake server and we can verify exactly what would hit their api
// without sending anything real
func fakePushover(t *testing.T, capture *map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			r.ParseForm()
		}
		got := map[string]string{}
		for k := range r.Form {
			got[k] = r.FormValue(k)
		}
		*capture = got
		// the lib refuses responses missing the quota headers the
		// real api always sends
		w.Header().Set("X-Limit-App-Limit", "10000")
		w.Header().Set("X-Limit-App-Remaining", "9999")
		w.Header().Set("X-Limit-App-Reset", "1754024400")
		w.Write([]byte(`{"status":1,"request":"fake-id","receipt":"fake-receipt"}`))
	}))
	old := pushover.APIEndpoint
	pushover.APIEndpoint = srv.URL
	t.Cleanup(func() {
		pushover.APIEndpoint = old
		srv.Close()
	})
	return srv
}

// the lib format-checks creds before sending (30 alphanumeric chars)
// so the fakes have to look real
const (
	testToken = "azGDORePK8gMaC0QOYAMyEEuzJnyUi"
	testUser  = "uQiRzpo4DXghDmr9QzzfQu27cmVRsG"
)

var testJob = sources.Job{
	Source:   "greenhouse",
	Company:  "stripe",
	JobID:    "12345",
	Title:    "Software Engineer, New Grad",
	Location: "San Francisco, CA",
	URL:      "https://stripe.com/jobs/12345",
}

func TestNotifySendsEmergencyWithRetryAndURL(t *testing.T) {
	var got map[string]string
	fakePushover(t, &got)

	n := New(testToken, testUser)
	if err := n.Notify(testJob, Emergency); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"token":     testToken,
		"user":      testUser,
		"title":     "stripe: Software Engineer, New Grad",
		"message":   "San Francisco, CA · greenhouse",
		"priority":  "2",
		"retry":     "30",
		"expire":    "3600",
		"url":       "https://stripe.com/jobs/12345",
		"url_title": "Open application",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("form field %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestNotifyNormalPrioritySkipsRetry(t *testing.T) {
	var got map[string]string
	fakePushover(t, &got)

	n := New(testToken, testUser)
	if err := n.Notify(testJob, Normal); err != nil {
		t.Fatal(err)
	}
	if got["priority"] != "0" {
		t.Errorf("priority = %q, want 0", got["priority"])
	}
	// retry/expire are emergency-only params
	if got["retry"] != "" || got["expire"] != "" {
		t.Errorf("retry/expire should be unset for priority 0, got %q/%q", got["retry"], got["expire"])
	}
}

func TestNotifyTruncatesByRunesNotBytes(t *testing.T) {
	var got map[string]string
	fakePushover(t, &got)

	long := testJob
	// en dashes force multibyte boundaries, the old byte-slicing
	// truncate shipped invalid utf-8 here
	long.Title = strings.Repeat("Software – Engineer ", 30) // 600 runes
	n := New(testToken, testUser)
	if err := n.Notify(long, Normal); err != nil {
		t.Fatal(err)
	}
	title := got["title"]
	if utf8.RuneCountInString(title) > 250 {
		t.Errorf("title is %d runes, pushover max is 250", utf8.RuneCountInString(title))
	}
	if !utf8.ValidString(title) {
		t.Error("truncation produced invalid utf-8")
	}
	if !strings.HasSuffix(title, "…") {
		t.Errorf("truncated title should end in …")
	}
}

func TestNotifyGuardsBadURLs(t *testing.T) {
	var got map[string]string
	fakePushover(t, &got)
	n := New(testToken, testUser)

	// empty url: the lib errors on url_title-without-url, so both
	// must be omitted rather than killing the alert
	j := testJob
	j.URL = ""
	if err := n.Notify(j, Normal); err != nil {
		t.Fatal(err)
	}
	if got["url"] != "" || got["url_title"] != "" {
		t.Errorf("empty URL should omit url fields, got url=%q url_title=%q", got["url"], got["url_title"])
	}

	// oversize url (lib max 512): moved into the body, alert survives
	j.URL = "https://example.com/" + strings.Repeat("x", 600)
	if err := n.Notify(j, Normal); err != nil {
		t.Fatal(err)
	}
	if got["url"] != "" {
		t.Error("oversize URL should not be sent in the url field")
	}
	if !strings.Contains(got["message"], "https://example.com/") {
		t.Error("oversize URL should land in the message body")
	}
}

func TestNotifyCapsLocationList(t *testing.T) {
	var got map[string]string
	fakePushover(t, &got)

	multi := testJob
	multi.Location = "Winnipeg, MB | Toronto, ON | Kitchener, ON | Vancouver, BC"
	n := New(testToken, testUser)
	if err := n.Notify(multi, Normal); err != nil {
		t.Fatal(err)
	}
	if want := "Winnipeg, MB, Toronto, ON +2 more · greenhouse"; got["message"] != want {
		t.Errorf("message = %q, want %q", got["message"], want)
	}
}

func TestSendDigest(t *testing.T) {
	var got map[string]string
	fakePushover(t, &got)

	n := New(testToken, testUser)
	if err := n.Send("12 new job matches", "Stripe ×3, Ramp +2 more", Emergency); err != nil {
		t.Fatal(err)
	}
	if got["title"] != "12 new job matches" || got["priority"] != "2" {
		t.Errorf("digest title/priority = %q/%q", got["title"], got["priority"])
	}
	if got["url"] != "" {
		t.Error("digest should carry no url")
	}
}
