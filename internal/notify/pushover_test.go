package notify

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	if err := n.Notify(testJob); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"token":     testToken,
		"user":      testUser,
		"title":     "stripe: Software Engineer, New Grad",
		"message":   "San Francisco, CA",
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
	n.Priority = pushover.PriorityNormal
	if err := n.Notify(testJob); err != nil {
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

func TestNotifyTruncatesLongTitles(t *testing.T) {
	var got map[string]string
	fakePushover(t, &got)

	long := testJob
	long.Title = strings.Repeat("Very Long Title ", 30) // ~480 chars
	n := New(testToken, testUser)
	if err := n.Notify(long); err != nil {
		t.Fatal(err)
	}
	if len(got["title"]) > 250 {
		t.Errorf("title went out %d chars, pushover max is 250", len(got["title"]))
	}
	if !strings.HasSuffix(got["title"], "...") {
		t.Errorf("truncated title should end in ..., got %q", got["title"][200:])
	}
}
