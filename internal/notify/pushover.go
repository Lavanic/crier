// Package notify sends the actual phone alert via pushover.
// priority 2 (emergency) + the critical alerts toggle in the ios app
// is what punches through silent mode and focus. emergency messages
// re-alert every Retry seconds until acked or Expire passes
package notify

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gregdel/pushover"

	"github.com/Lavanic/crier/internal/sources"
)

// re-exported so main doesn't import the pushover lib directly
const (
	Emergency = pushover.PriorityEmergency
	Normal    = pushover.PriorityNormal
)

// pushover hard limits, we truncate instead of letting the api reject.
// limits are CHARACTERS not bytes, hence the rune counting below
const (
	maxTitleLen   = 250
	maxMessageLen = 1024
	maxURLLen     = 512
)

type Notifier struct {
	app  *pushover.Pushover
	user *pushover.Recipient
}

func New(appToken, userKey string) *Notifier {
	return &Notifier{
		app:  pushover.New(appToken),
		user: pushover.NewRecipient(userKey),
	}
}

// Notify fires one alert for one job. title carries company + role,
// body carries location, source and the url is the apply page so the
// alert is tappable straight into the application
func (n *Notifier) Notify(j sources.Job, priority int) error {
	msg := &pushover.Message{
		Title:    truncate(fmt.Sprintf("%s: %s", j.Company, j.Title), maxTitleLen),
		Message:  truncate(body(j), maxMessageLen),
		Priority: priority,
	}
	// the lib errors on URLTitle-without-URL and on urls over 512
	// chars, guard both so a weird feed row can't kill its own alert
	if j.URL != "" && utf8.RuneCountInString(j.URL) <= maxURLLen {
		msg.URL = j.URL
		msg.URLTitle = "Open application"
	} else if j.URL != "" {
		msg.Message = truncate(body(j)+"\n"+j.URL, maxMessageLen)
	}
	return n.send(msg, j.DedupKey())
}

// Send fires a plain alert with no job attached, used for the
// too-many-matches digest
func (n *Notifier) Send(title, message string, priority int) error {
	msg := &pushover.Message{
		Title:    truncate(title, maxTitleLen),
		Message:  truncate(message, maxMessageLen),
		Priority: priority,
	}
	return n.send(msg, "digest")
}

func (n *Notifier) send(msg *pushover.Message, what string) error {
	if msg.Priority == pushover.PriorityEmergency {
		// emergency requires these two: re-buzz every 30s for up to
		// an hour until I ack. that's the "apply first" machinery
		msg.Retry = 30 * time.Second
		msg.Expire = time.Hour
	}
	if _, err := n.app.SendMessage(msg, n.user); err != nil {
		return fmt.Errorf("pushover send for %s: %w", what, err)
	}
	return nil
}

// body composes the message line: capped locations plus the source,
// so a 6-city posting doesn't push everything off the lock screen and
// I can tell a 60s-fresh direct hit from an aggregator find
func body(j sources.Job) string {
	loc := j.Location
	if parts := strings.Split(loc, " | "); len(parts) > 2 {
		loc = fmt.Sprintf("%s, %s +%d more", parts[0], parts[1], len(parts)-2)
	}
	if loc == "" {
		loc = "no location listed"
	}
	return loc + " · " + sourceLabel(j.Source)
}

func sourceLabel(source string) string {
	if handle, ok := strings.CutPrefix(source, "instagram:"); ok {
		return "via @" + handle
	}
	return source
}

// truncate by runes, not bytes. slicing bytes can cut a multibyte
// char in half and ship invalid utf-8 (real titles have en dashes),
// and pushover's limits are characters anyway
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
