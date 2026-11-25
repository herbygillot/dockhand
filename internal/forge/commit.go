package forge

import "regexp"

var (
	fullCommit        = regexp.MustCompile(`^[0-9a-f]{40}$`)
	abbreviatedCommit = regexp.MustCompile(`^[0-9a-f]{7,39}$`)
)

// FullCommit reports whether s is a commit as a forge names it whole:
// forty lowercase hex digits, as a Portfile pins one and a forge's API
// returns it. It is narrower than git.ValidObjectID, which accepts any
// object ID Git can read.
func FullCommit(s string) bool { return fullCommit.MatchString(s) }

// AbbreviatedCommit reports whether s is a commit abbreviated as a forge
// shows it: seven to thirty-nine lowercase hex digits.
func AbbreviatedCommit(s string) bool { return abbreviatedCommit.MatchString(s) }
