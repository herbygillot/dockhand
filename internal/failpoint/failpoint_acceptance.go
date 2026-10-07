//go:build acceptance

package failpoint

import (
	"os"
	"strings"
	"sync"
	"syscall"
)

// Enabled says whether this build has failpoints: an acceptance build has.
const Enabled = true

// Hit kills the process where DOCKHAND_FAILPOINT names this step with
// :kill, as a kill -9 there would.
func Hit(step string) {
	name, action, ok := strings.Cut(os.Getenv("DOCKHAND_FAILPOINT"), ":")
	if !ok || name != step || action != "kill" {
		return
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
	select {}
}

// failed are the steps that have failed as asked, each once in a process.
var failed sync.Map

// Fails says the failure DOCKHAND_FAILPOINT asks of this step, once in the
// process: "fault", a fault in dockhand's own handling, or "error", one
// nothing classifies; "" where it asks none, or the step has failed once.
func Fails(step string) string {
	name, action, ok := strings.Cut(os.Getenv("DOCKHAND_FAILPOINT"), ":")
	if !ok || name != step || (action != "fault" && action != "error") {
		return ""
	}
	if _, done := failed.LoadOrStore(step, true); done {
		return ""
	}
	return action
}
