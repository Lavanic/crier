package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// httptest spins up a real http server on localhost with a random
// port, so these tests exercise the actual client stack offline

func TestGetJSONDecodesAndSendsBrowserHeaders(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Write([]byte(`{"title": "Software Engineer I"}`))
	}))
	defer srv.Close()

	var out struct {
		Title string `json:"title"`
	}
	if err := getJSON(context.Background(), NewHTTPClient(), srv.URL, &out); err != nil {
		t.Fatal(err)
	}
	if out.Title != "Software Engineer I" {
		t.Errorf("decoded title = %q", out.Title)
	}
	if !strings.Contains(gotUA, "Mozilla") {
		t.Errorf("user agent = %q, want a browser-looking one", gotUA)
	}
}

func TestGetJSONRetriesOn500(t *testing.T) {
	// fail the first request, succeed the second. proves the
	// retryablehttp wiring actually retries
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	var out struct{}
	if err := getJSON(context.Background(), NewHTTPClient(), srv.URL, &out); err != nil {
		t.Fatalf("wanted success after retry, got %v", err)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("server saw %d calls, want 2 (fail then retry)", n)
	}
}

func TestGetJSONErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	var out struct{}
	err := getJSON(context.Background(), NewHTTPClient(), srv.URL, &out)
	if err == nil {
		t.Fatal("expected an error for a 404, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error %q should mention the status code", err)
	}
}

func TestDedupKey(t *testing.T) {
	j := Job{Source: "greenhouse", Company: "stripe", JobID: "12345"}
	if got := j.DedupKey(); got != "greenhouse:stripe:12345" {
		t.Errorf("DedupKey = %q", got)
	}
}
