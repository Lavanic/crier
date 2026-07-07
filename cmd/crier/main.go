// crier polls ATS job boards + aggregator feeds and fires a pushover
// critical alert for every new posting that matches my filters.
// runs as a short-lived process under a systemd timer, so
// one invocation = one poll tick, then exit.
package main

import (
	"fmt"
	"os"
)

func main() {
	// placeholder so `make build` works from chunk 1
	// real pipeline (config -> sources -> filter -> store -> notify)
	// gets wired up in a later chunk
	fmt.Fprintln(os.Stderr, "crier: not wired up yet")
	os.Exit(1)
}
