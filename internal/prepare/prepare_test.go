package prepare

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/herbygillot/dockhand/internal/record"
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

// A MEMBER'S STAGED PORTDIR AND ITS RECORD PORTDIR ARE DIFFERENT PATH
// SPACES, and conflating them is how the evaluator came to resolve a
// portdir against the process's working directory.
//
// tree.Target.Portdir is documented "absolute portdir path"; a record's
// Portdir is tree-relative. The subport is still decided by the record's
// own name, because that is the name the cohort was proposed under —
// the staged path's basename is the same word, but it is the record that
// is authoritative about which port a member is.
func TestAStagedTargetKeepsTheTwoPathSpacesApart(t *testing.T) {
	c := record.Candidate{Port: "Aseprite", Portdir: "graphics/Aseprite"}
	got := stagedTarget("/tmp/stage-1/graphics/Aseprite", c)
	assert.Equal(t, "/tmp/stage-1/graphics/Aseprite", got.Portdir,
		"the planner is pointed at a path this process made, never at a relative one")
	assert.Empty(t, got.Subport, "a member named for its own directory needs no subport")

	sub := record.Candidate{Port: "py312-foo", Portdir: "python/py-foo"}
	assert.Equal(t, "py312-foo", stagedTarget("/tmp/stage-1/python/py-foo", sub).Subport,
		"and a subport is decided by the RECORD's name, not by the staged path")
}

// AND A COHORT WITH NO CRITERION FALLS BACK RATHER THAN SAYING NOTHING.
// A proposal always carries one; the fallback is for a candidate list
// reaching this by another road.
func TestCohortReasonFallsBackWhenNoMeasurementIsCarried(t *testing.T) {
	cands := []record.Candidate{{Port: "Aseprite", Proposed: true, Reason: "depends_lib"}}
	assert.Equal(t, "depends_lib", cohortReason(cands, ""))
	assert.Equal(t, "rebuild against the headline change", cohortReason(nil, ""))
}

// A REVBUMP COMMIT STATES WHY USERS MUST REBUILD, which is the
// MEASUREMENT and not the membership.
//
// A candidate's own Reason says why that PORT is in the cohort
// ("depends_lib"); the criterion says why anybody must rebuild ("install
// name libcmark.0.30.3.dylib -> libcmark.0.31.2.dylib"). cohortReason's
// own doc described the second and read the first, so the first cohort
// dockhand ever proposed produced a commit titled "Aseprite:
// depends_lib" — which tells a MacPorts reviewer nothing they can check,
// where the whole argument for a proposal is that its one claim can be
// checked by hand with otool.
func TestCohortReasonStatesTheMeasurementAndNotTheMembership(t *testing.T) {
	cands := []record.Candidate{{Port: "Aseprite", Portdir: "graphics/Aseprite", Proposed: true, Reason: "depends_lib"}}
	criterion := "install name /opt/local/lib/libcmark.0.30.3.dylib → /opt/local/lib/libcmark.0.31.2.dylib"

	assert.Equal(t, criterion, cohortReason(cands, criterion))
	assert.NotContains(t, cohortReason(cands, criterion), "depends_lib",
		"membership is a different sentence for a different reader")
}
