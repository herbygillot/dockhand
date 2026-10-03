//go:build acceptance

package failpoint

import (
	"os"
	"strings"
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
