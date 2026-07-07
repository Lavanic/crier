// Package notify sends the actual phone alert via pushover.
// priority 2 (emergency) + the critical alerts toggle in the ios app
// is what punches through silent mode and focus. emergency messages
// re-alert every Retry seconds until acked or Expire passes
package notify

import (
	"fmt"
	"time"

	"github.com/gregdel/pushover"

	"github.com/Lavanic/crier/internal/sources"
)

// pushover hard limits, we truncate instead of letting the api reject
const (
	maxTitleLen   = 250
	maxMessageLen = 1024
)

type Notifier struct {
	app  *pushover.Pushover
	user *pushover.Recipient
	// exported so the smoke test can send a polite priority 0 first,
	// real runs keep the emergency default
	Priority int
}

func New(appToken, userKey string) *Notifier {
	return &Notifier{
		app:      pushover.New(appToken),
		user:     pushover.NewRecipient(userKey),
		Priority: pushover.PriorityEmergency,
	}
}

// Notify fires one alert for one job. title carries company + role,
// body carries location, and the url is the apply page so the alert
// is tappable straight into the application
func (n *Notifier) Notify(j sources.Job) error {
	body := j.Location
	if body == "" {
		body = "no location listed"
	}
	msg := &pushover.Message{
		Title:    truncate(fmt.Sprintf("%s: %s", j.Company, j.Title), maxTitleLen),
		Message:  truncate(body, maxMessageLen),
		URL:      j.URL,
		URLTitle: "Open application",
		Priority: n.Priority,
	}
	if n.Priority == pushover.PriorityEmergency {
		// emergency requires these two: re-buzz every 30s for up to
		// an hour until I ack. that's the "apply first" machinery
		msg.Retry = 30 * time.Second
		msg.Expire = time.Hour
	}
	if _, err := n.app.SendMessage(msg, n.user); err != nil {
		return fmt.Errorf("pushover send for %s: %w", j.DedupKey(), err)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
