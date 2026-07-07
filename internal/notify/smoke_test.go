//go:build smoke

package notify

import (
	"os"
	"testing"

	"github.com/gregdel/pushover"

	"github.com/Lavanic/crier/internal/sources"
)

// sends ONE real priority 0 (normal) message to my actual phone.
// deliberately not priority 2, no need to blast the emergency siren
// to prove the plumbing works. needs creds in the env:
//
//	CRIER_PUSHOVER_APP_TOKEN=... CRIER_PUSHOVER_USER_KEY=... make smoke
func TestSmokePushover(t *testing.T) {
	token := os.Getenv("CRIER_PUSHOVER_APP_TOKEN")
	user := os.Getenv("CRIER_PUSHOVER_USER_KEY")
	if token == "" || user == "" {
		t.Skip("CRIER_PUSHOVER_APP_TOKEN / CRIER_PUSHOVER_USER_KEY not set, skipping live send")
	}

	n := New(token, user)
	n.Priority = pushover.PriorityNormal
	err := n.Notify(sources.Job{
		Source:   "smoke-test",
		Company:  "crier",
		JobID:    "0",
		Title:    "test alert, plumbing works",
		Location: "your phone :)",
		URL:      "https://github.com/Lavanic/crier",
	})
	if err != nil {
		t.Fatalf("live pushover send failed: %v", err)
	}
	t.Log("sent, check your phone for a normal-priority ping")
}
