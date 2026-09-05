package render

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
)

// The cohort's words, pinned: the second commit's message, the pull
// request's cohort section, and the proposal line status prints.
//
// One sentence has to survive all three unchanged — the criterion the
// measurement produced — and these goldens are how that is checked. A
// commit body, a pull request and a terminal line that each paraphrased
// "install name libwidget.2.dylib → libwidget.3.dylib" would be three
// claims a reviewer has to reconcile, and the whole argument for a
// proposal is that a person can check the one claim behind it with
// otool by hand.

// theCriterion is the measurement's own sentence, as ABIDelta words it.
// It is a literal here rather than a call into verdict, because that is
// exactly the coupling these goldens exist to detect: if the judgment
// rewords it, this file has to be re-recorded and the change is
// reviewed as what it is.
const theCriterion = "install name /opt/local/lib/libwidget.2.dylib → /opt/local/lib/libwidget.3.dylib; " +
	"libwidget compatibility_version 2.0.0 → 3.0.0 (narrowed), measured between libwidget@2.4.1 " +
	"(binary archive) and @3.0 (built from source) on Testos"

const theLimits = "this criterion is necessary and not sufficient: an install name and a compatibility " +
	"version can sit still while symbols are removed, and a break confined to a header or to a " +
	"plugin's own contract is invisible to otool"

// theCohort is the commit the acceptance test's ninth step writes:
// members in dependency order, a member declined by name, and the ports
// examined and left out with the reason each one was.
func theCohort() CohortCommit {
	return CohortCommit{
		Port: "libwidget", Target: "3.0",
		Criterion: theCriterion,
		Limits:    theLimits,
		Quotes: []CohortQuote{{
			Source: "gis/gdal/Portfile",
			Quote: "# Please increase the revision of gdal-hdf5 and gdal-kea whenever\n" +
				"# libwidget's version is updated.",
		}},
		Members: []CohortMember{
			{Port: "gdal", Portdir: "gis/gdal", Reason: "depends_lib"},
			{Port: "grass", Portdir: "gis/grass", Reason: "depends_lib; nomaintainer"},
			{Port: "mapnik", Portdir: "gis/mapnik", Reason: "depends_lib; named by the comment in gis/gdal/Portfile"},
		},
		Declined: []CohortDecline{{
			Port: "php80-Judy", Portdir: "php/php-Judy",
			Reason: "plan: declined: the Portfile's shape does not say where a revision line belongs: " +
				"the version is carried by a `set` variable, which is not a line a revision belongs under",
		}},
		Listed: []record.Candidate{
			{Port: "gdal", Portdir: "gis/gdal", Proposed: true, Reason: "depends_lib"},
			{Port: "cmake", Portdir: "devel/cmake", Reason: "depends_build only: it links nothing this change publishes"},
			{Port: "djview-qt5", Portdir: "aqua/djview", Reason: "replaced by djview"},
			{Port: "osm2pgsql", Portdir: "gis/osm2pgsql", Reason: "already in flight on dockhand/osm2pgsql-2.0"},
		},
	}
}

// cohortGoldenDir holds the commit messages. They are pinned the same
// way a pull request body is and swept for staleness by a different
// test, so they sit apart from the bodies rather than under the sweep
// that would call them orphans.
const cohortGoldenDir = "testdata/cohort"

func TestCohortMessageIsOneCommitStatingOneCriterion(t *testing.T) {
	checkGoldenIn(t, cohortGoldenDir, "cohort_commit_message", CohortMessage(theCohort()))
}

// A forced member's commit-message line reads the reason the engine
// reworded for the seat: forced into the build with its sibling
// deactivated first, not withheld. The seated sibling above it is an
// ordinary revbump line.
func TestCohortMessageSaysAForcedMemberWasForced(t *testing.T) {
	c := theCohort()
	c.Members = []CohortMember{
		{Port: "gegl", Portdir: "graphics/gegl", Reason: "depends_lib"},
		{Port: "gegl-devel", Portdir: "graphics/gegl-devel",
			Reason: "depends_lib; conflicts with gegl, which this cohort builds — " +
				"forced into the build at the maintainer's request, with gegl deactivated first"},
	}
	c.Declined, c.Listed, c.Quotes = nil, nil, nil
	checkGoldenIn(t, cohortGoldenDir, "cohort_commit_message_forced_member", CohortMessage(c))
}

// A cohort of one is a cohort, and its subject says so in the singular.
// The plural form said over one dependent is the kind of small wrongness
// a reviewer reads as carelessness about the rest.
func TestCohortMessageCountsInTheSingular(t *testing.T) {
	c := theCohort()
	c.Members = c.Members[:1]
	c.Declined, c.Listed, c.Quotes = nil, nil, nil
	checkGoldenIn(t, cohortGoldenDir, "cohort_commit_message_one_member", CohortMessage(c))
}

// cohortNoteFor builds a settled record carrying a cohort: the
// measurement, the proposal with the disposition given, the members as
// subjects, and each member's own link proof.
func cohortNoteFor(d record.Disposition, links map[string][]string) record.Record {
	n := record.Record{
		Schema: record.Schema, Sha: "0123456789abcdef0123",
		Subjects: []record.Subject{
			{Port: "libwidget", Names: []string{"libwidget"}, Portdir: "devel/libwidget", Target: "3.0"},
			{Port: "gdal", Names: []string{"gdal"}, Portdir: "gis/gdal", Intent: "bump-revision", Target: "rev4"},
			{Port: "grass", Names: []string{"grass"}, Portdir: "gis/grass", Intent: "bump-revision", Target: "rev2"},
		},
		Jobs: map[string]record.JobRecord{"Sequoia": {}},
		Runs: map[string]record.Run{},
		Findings: []record.Finding{
			{Kind: KindABIChanged, Ports: []string{"libwidget"}, Criterion: theCriterion,
				Disposition: record.Accepted},
			{Kind: KindCohort, Ports: []string{"gdal", "grass"}, Criterion: theCriterion,
				Disposition: d,
				Candidates: []record.Candidate{
					{Port: "gdal", Portdir: "gis/gdal", Proposed: true, Reason: "depends_lib"},
					{Port: "grass", Portdir: "gis/grass", Proposed: true, Reason: "depends_lib; nomaintainer"},
					{Port: "cmake", Portdir: "devel/cmake",
						Reason: "depends_build only: it links nothing this change publishes"},
				}},
		},
	}
	for _, s := range n.Subjects {
		n.Runs[record.RunKey(s.Port, "Sequoia")] = record.Run{
			State: record.Passed, Platform: "Sequoia", FromSource: true,
			Evidence: "built in a pristine VM", Links: links[s.Port]}
	}
	return n
}

// cohortLinks is what the environment observed about the members: gdal
// binds to the library that moved, and grass installed and bound to
// nothing it publishes — build-only in fact, whatever its depends_*
// fields said. An empty list is that answer; a nil would say nobody
// looked, and the two must stay tellable apart.
var cohortLinks = map[string][]string{
	"libwidget": {},
	"gdal":      {"/opt/local/lib/libgdal.36.dylib links against /opt/local/lib/libwidget.3.dylib"},
	"grass":     {},
}

// An ordinary change's body is byte-identical to what it always was: a
// record with no findings has no cohort section, and the goldens
// recorded before any of this existed are what says so.
func TestABodyWithNoFindingsCarriesNoCohortSection(t *testing.T) {
	n := cohortNoteFor(record.Accepted, nil)
	n.Findings = nil
	assert.Empty(t, CohortBody(n))
}

func TestProposalLinesNameTheTwoVerbsThatAnswer(t *testing.T) {
	n := cohortNoteFor(record.Proposed, nil)
	lines := ProposalLines(&n, "dockhand/libwidget-3.0")
	require.Len(t, lines, 2)
	assert.Equal(t, "ABI changed: "+theCriterion, lines[0])
	assert.Equal(t,
		"proposal: 2 dependents need a revision bump (gdal, grass) — "+
			"`dockhand bump-revision --for dockhand/libwidget-3.0` builds the cohort, "+
			"`dockhand dismiss dockhand/libwidget-3.0` records that you looked and said no",
		lines[1])

	// Answered, so only the measurement is left to state.
	n = cohortNoteFor(record.Accepted, nil)
	assert.Equal(t, []string{"ABI changed: " + theCriterion}, ProposalLines(&n, "dockhand/libwidget-3.0"))

	// A branch with nothing found says nothing extra, which is what
	// keeps every status golden recorded before this exactly as it was.
	n.Findings = nil
	assert.Empty(t, ProposalLines(&n, "dockhand/libwidget-3.0"))
	assert.Empty(t, ProposalLines(nil, "dockhand/libwidget-3.0"))
}

// The instruction comment's own proposal line: a person is shown the
// comment's first line and told what answers it. The whole quote is on
// the note and in the commit body — a branch listing is a column of
// standings, not the place for a verbatim multi-line quote.
func TestAnInstructionCommentGetsItsOwnProposalLine(t *testing.T) {
	n := record.Record{Findings: []record.Finding{{
		Kind: KindInstruction, Ports: []string{"libwidget"},
		Source:      "devel/libwidget/Portfile",
		Quote:       "# Please increase the revision of gdal whenever\n# libwidget's version is updated.",
		Disposition: record.Proposed,
	}}}
	lines := ProposalLines(&n, "dockhand/libwidget-3.0")
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], `"Please increase the revision of gdal whenever"`)
	assert.Contains(t, lines[0], "`dockhand dismiss dockhand/libwidget-3.0`")
	// Nothing has measured, so the step offered is the one that would.
	// The cohort verb is NOT offered here: with no ABI finding it
	// refuses and sends the reader to verify anyway, so naming it would
	// be one hop of a loop.
	assert.Contains(t, lines[0], "`dockhand verify dockhand/libwidget-3.0` measures whether anything moved")
	assert.NotContains(t, lines[0], "bump-revision")
}

// The same comment, once the measurement has come back and found
// nothing. The verb that would bump the cohort refuses on this note —
// it only ever bumps what an abi-change supports — so the line must not
// offer it: a reader who ran it was told nothing had measured whether
// anything moved, three lines under the measurement, and sent back to
// `verify`, which re-measures to the same answer forever.
func TestAnInstructionCommentOffersNoVerbThatWouldRefuse(t *testing.T) {
	n := record.Record{Findings: []record.Finding{{
		Kind: KindABIUnchanged, Ports: []string{"libwidget"},
		Criterion:   "no install name, compatibility version or library moved, measured between libwidget@2.4.1 (binary archive) and @2.4.2 (built from source) on Sequoia",
		Disposition: record.Accepted,
	}, {
		Kind: KindInstruction, Ports: []string{"libwidget"},
		Source:      "devel/libwidget/Portfile",
		Quote:       "# Please increase the revision of gdal whenever\n# libwidget's version is updated.",
		Disposition: record.Proposed,
	}}}
	lines := ProposalLines(&n, "dockhand/libwidget-3.0")
	require.Len(t, lines, 2, "the measurement's line, then the comment's")
	assert.Contains(t, lines[0], "ABI unchanged: no install name")
	assert.Contains(t, lines[1], "the measurement above found nothing to bump on")
	assert.Contains(t, lines[1], "`dockhand dismiss dockhand/libwidget-3.0`")
	assert.NotContains(t, lines[1], "bump-revision")
	assert.NotContains(t, lines[1], "dockhand verify")
}

// The unavailable measurement gets a line of its own rather than
// silence: "the check could not be made" and "the check found nothing"
// are the two answers a reader is most likely to confuse, and an absent
// line would be read as the second.
func TestAnUnavailableCheckSaysSoRatherThanNothing(t *testing.T) {
	n := record.Record{Findings: []record.Finding{{
		Kind: KindABIUnavailable, Ports: []string{"libwidget"},
		Criterion:   "ABI check unavailable: no baseline for libwidget: the port did not exist at the merge base",
		Disposition: record.Accepted,
	}}}
	assert.Equal(t,
		[]string{"ABI check unavailable: no baseline for libwidget: the port did not exist at the merge base"},
		ProposalLines(&n, "dockhand/libwidget-3.0"),
		"the refusal says what it refused once: the judgment writes that in, so the line does not say it twice")
}

// The proposal reaches the branch listing, under the standings and not
// among them: a proposal is not a verdict about the change, and a
// reader scanning for "passed" must not have to read past an advisory
// to find it.
func TestTheReportPrintsTheProposalUnderTheBranch(t *testing.T) {
	n := cohortNoteFor(record.Proposed, nil)
	var out, errOut strings.Builder
	Report{Repository: "/ports", Now: time.Now(), Branches: []BranchReport{
		{Branch: "dockhand/libwidget-3.0", Tip: n.Sha, Note: &n},
	}}.Text(&out, &errOut)

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	require.Greater(t, len(lines), 4)
	assert.Equal(t, "dockhand/libwidget-3.0", lines[0])
	assert.Contains(t, lines[1], "libwidget: passed (Sequoia)", "the verdicts come first")
	assert.Contains(t, lines[len(lines)-2], "ABI changed:")
	assert.Contains(t, lines[len(lines)-1], "`dockhand bump-revision --for dockhand/libwidget-3.0`")
}

// A member the body claims a bump for is never listed with nothing
// beside it. Each member's line ends in what its own run said: the link
// proof, "links nothing that moved" where the sweep ran and found none,
// or — where the run never reached the sweep — which reason that was.
// Found live on macports-ports#34500: gthumb had failed to build, was
// promoted over as best effort, and stood under "Revision bumped in
// this change" with no evidence and no reason for the absence, the
// fact being three paragraphs up in the verification block.
//
// One member per state, in one body, so the shapes are read side by
// side: the proof, the empty proof, the failure, the block, the
// withholding the proposal already worded, the withholding it did not,
// and the pass nobody swept — which stays silent, because a check that
// could not be made is said once for the change and not per member.
func TestEachMemberSaysWhyItCarriesNoProof(t *testing.T) {
	const proof = "/opt/local/lib/libgdal.36.dylib links against /opt/local/lib/libwidget.3.dylib"
	n := record.Record{
		Schema: record.Schema, Sha: "0123456789abcdef0123",
		Jobs: map[string]record.JobRecord{"Sequoia": {}, "Sonoma": {}},
		Runs: map[string]record.Run{
			record.RunKey("libwidget", "Sequoia"): {State: record.Passed, Platform: "Sequoia", Links: []string{}},
			// Proven on one platform and failed on another: the proof is
			// the answer, and the failure is the verification block's.
			record.RunKey("gdal", "Sequoia"):   {State: record.Passed, Platform: "Sequoia", Links: []string{proof}},
			record.RunKey("gdal", "Sonoma"):    {State: record.Failed, Platform: "Sonoma"},
			record.RunKey("grass", "Sequoia"):  {State: record.Passed, Platform: "Sequoia", Links: []string{}},
			record.RunKey("gthumb", "Sequoia"): {State: record.Failed, Platform: "Sequoia"},
			record.RunKey("mapnik", "Sequoia"): {State: record.Blocked, Platform: "Sequoia", Blamed: "gthumb"},
			record.RunKey("gegl-devel", "Sequoia"): {State: record.Withheld, Platform: "Sequoia",
				Detail: "it conflicts with gegl, which this cohort builds"},
			record.RunKey("qgis", "Sequoia"):      {State: record.Withheld, Platform: "Sequoia"},
			record.RunKey("osm2pgsql", "Sequoia"): {State: record.Passed, Platform: "Sequoia"},
		},
		Findings: []record.Finding{
			{Kind: KindABIChanged, Ports: []string{"libwidget"}, Criterion: theCriterion,
				Disposition: record.Accepted},
			{Kind: KindCohort, Criterion: theCriterion, Disposition: record.Accepted,
				Candidates: []record.Candidate{
					{Port: "gdal", Portdir: "gis/gdal", Proposed: true, Reason: "depends_lib"},
					{Port: "grass", Portdir: "gis/grass", Proposed: true, Reason: "depends_lib; nomaintainer"},
					{Port: "gthumb", Portdir: "gnome/gthumb", Proposed: true, Reason: "depends_lib; nomaintainer"},
					{Port: "mapnik", Portdir: "gis/mapnik", Proposed: true, Reason: "depends_lib"},
					// The proposal's own wording of a withholding, as
					// verdict writes it for a member that conflicts with
					// one already seated. Solo is what says the reason
					// already carries the sentence.
					{Port: "gegl-devel", Portdir: "graphics/gegl-devel", Proposed: true, Solo: true,
						Reason: "depends_lib; conflicts with gegl, which this cohort builds — bumped here, and not built"},
					{Port: "qgis", Portdir: "gis/qgis", Proposed: true, Reason: "depends_lib"},
					{Port: "osm2pgsql", Portdir: "gis/osm2pgsql", Proposed: true, Reason: "depends_lib"},
				}},
		},
	}

	body := CohortBody(n)
	_, after, found := strings.Cut(body, "Revision bumped in this change:\n")
	require.True(t, found, "the accepted cohort lists its members:\n%s", body)
	block, _, _ := strings.Cut(after, "\n\n")
	assert.Equal(t, []string{
		"  — gdal (gis/gdal): depends_lib; " + proof,
		"  — grass (gis/grass): depends_lib; nomaintainer; links nothing that moved",
		"  — gthumb (gnome/gthumb): depends_lib; nomaintainer; the build failed, so nothing was measured",
		"  — mapnik (gis/mapnik): depends_lib; blocked before it was reached, so nothing was measured",
		"  — gegl-devel (graphics/gegl-devel): depends_lib; conflicts with gegl, which this cohort builds — bumped here, and not built",
		"  — qgis (gis/qgis): depends_lib; not built here",
		"  — osm2pgsql (gis/osm2pgsql): depends_lib",
	}, strings.Split(strings.TrimRight(block, "\n"), "\n"))
}

// The sentence in the proof's place is the strongest fact the member's
// runs hold, across however many platforms it ran on, and nothing at
// all for a state best effort does not publish over.
func TestUnmeasuredNamesTheStrongestOutcome(t *testing.T) {
	for _, tc := range []struct {
		name   string
		states []record.RunState
		solo   bool
		want   string
	}{
		{"failed", []record.RunState{record.Failed}, false, "the build failed, so nothing was measured"},
		{"blocked", []record.RunState{record.Blocked}, false, "blocked before it was reached, so nothing was measured"},
		{"withheld, unworded by the proposal", []record.RunState{record.Withheld}, false, "not built here"},
		{"withheld, worded by the proposal", []record.RunState{record.Withheld}, true, ""},
		{"a failure outranks a block", []record.RunState{record.Blocked, record.Failed}, false,
			"the build failed, so nothing was measured"},
		{"a block outranks a withholding", []record.RunState{record.Withheld, record.Blocked}, false,
			"blocked before it was reached, so nothing was measured"},
		{"a pass nobody swept", []record.RunState{record.Passed}, false, ""},
		{"a refusal is the port's own answer", []record.RunState{record.Unsupported}, false, ""},
		{"a non-outcome is not published over", []record.RunState{record.Canceled, record.Errored, record.Running}, false, ""},
		{"no run at all", nil, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, unmeasured(tc.states, tc.solo))
		})
	}
}
