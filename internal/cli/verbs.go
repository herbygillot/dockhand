package cli

import (
	"github.com/herbygillot/dockhand/internal/tui"
	"github.com/herbygillot/dockhand/internal/workflow/view"
)

// tableVerbs is what the live status table runs for a key on a row. It lives
// here, beside the commands and flags it names, so a renamed flag breaks the
// table at compile time or in this package's tests rather than silently.
func tableVerbs() tui.Verbs {
	return tui.Verbs{Args: verbArgs, Confirms: verbConfirms}
}

// verbArgs builds the command for a verb on a row: exact by contribution ID
// when the row is a tracked change, by job for standalone work. Verbs that
// start work detach, since the table's own processing carries it on.
func verbArgs(verb string, row view.Contribution) ([]string, string) {
	var args []string
	switch {
	case verb == "bump":
		// A bump continues the port's open contribution by itself, and starts
		// afresh when the row's contribution has retired.
		args = []string{verb, row.Port}
	case row.ChangeID != "":
		args = []string{verb, "--change", string(row.ChangeID)}
	case verb == "cancel":
		if row.Active == nil {
			return nil, "nothing is pending"
		}
		args = []string{verb, "--job", string(row.Active.JobID)}
	case verb == "verify":
		args = []string{verb, row.Port}
	default:
		return nil, "not a tracked contribution; " + verb + " needs one"
	}
	if verb == "bump" || verb == "verify" || verb == "publish" {
		args = append(args, "--detach")
	}
	return args, ""
}

// verbConfirms names the verbs that cost minutes, push, or discard.
func verbConfirms(verb string) bool {
	switch verb {
	case "verify", "publish", "cancel", "abandon", "bump":
		return true
	}
	return false
}
