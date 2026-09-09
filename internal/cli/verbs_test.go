package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A COHORT'S SUBJECT NAMES THE CHANGE IT IS FOR, not a member of it.
//
// change.Merge takes identity from parts[0], so the subject was the
// first member's plan — "Aseprite: <the whole ABI measurement>", 263
// characters naming a port that is in the cohort only because cmark
// moved. promote --title defaults to the tip's subject, so that was the
// pull request title too.
func TestACohortSubjectIsTheChangeItIsFor(t *testing.T) {
	assert.Equal(t, "cmark: update to 0.31.2, bump dependents",
		cohortSummary("cmark: update to 0.31.2"))
	assert.Less(t, len(cohortSummary("cmark: update to 0.31.2")), 72,
		"and it fits where a subject line has to fit")
}

// A RE-ACCEPT READS A TIP THAT HAS BEEN THROUGH HERE BEFORE, and one
// cohort commit on top of another must not stutter.
func TestACohortSubjectDoesNotStutterOnReAccept(t *testing.T) {
	once := cohortSummary("cmark: update to 0.31.2")
	assert.Equal(t, once, cohortSummary(once))
}
