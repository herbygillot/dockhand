package render

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// update rewrites the goldens under testdata/golden from what the code
// renders now: `go test ./internal/render -update`. A golden is a pinned
// PR body, so a rewrite is reviewed as a change to what dockhand tells
// upstream, not as test maintenance.
var update = flag.Bool("update", false, "rewrite golden files")

// guest is what a fixture's platform ran in — the JobRecord half of
// the record.
//
// It is stated apart from the run because the record states it apart,
// and that split is exactly what these renderings have to read
// correctly: the test suite is asked of an environment and the handle
// names one, so neither can be read off a verdict any more.
type guest struct {
	Test bool
	// Handle and Released are two facts, not one. A release does not
	// erase the name: the handle says what was handed back, which is
	// what a person deletes by hand when the provider refused.
	Handle   string
	Released bool
	Started  time.Time
}

// templateNote builds a one-subject record from platform-keyed runs and
// the guests they ran in.
//
// The runs are keyed and stamped the way the ledger writes them —
// RunKey(port, release), with the platform on the run as well as in the
// key — because every projection reaches a run through its subject, and
// a fixture keyed by release alone would pin a note shape nothing
// writes. A queued run gets no job: nothing was submitted for it, and a
// record naming an environment no run entered is the corruption the
// readers are meant to notice.
func templateNote(runs map[string]record.Run, guests map[string]guest) record.Record {
	n := record.Record{
		Schema:   record.Schema,
		Sha:      "0123456789abcdef0123",
		Subjects: []record.Subject{{Port: "jq", Names: []string{"jq"}}},
		Jobs:     map[string]record.JobRecord{},
		Runs:     map[string]record.Run{},
	}
	for plat, r := range runs {
		r.Platform = plat
		n.Runs[record.RunKey("jq", plat)] = r
		if r.State == record.Queued {
			continue
		}
		g := guests[plat]
		n.Jobs[plat] = record.JobRecord{
			Job:      verify.Job{Provider: "fake", ID: "fake-" + plat, Started: g.Started},
			Test:     g.Test,
			Handle:   g.Handle,
			Released: g.Released,
		}
	}
	return n
}

// vouched is the opts a verified single-commit promotion passes: the
// shape most of these tests only vary one field of.
func vouched() PRBodyOpts {
	return PRBodyOpts{Version: "1.2.3", OwnCommits: 1, CheckedPRs: true}
}

// The body is the upstream PR template with only vouchable boxes
// checked: install passed and tested, a single minted commit.
func TestPRBodyChecksWhatItCanVouchFor(t *testing.T) {
	n := templateNote(map[string]record.Run{
		"Sonoma":   {State: record.Passed},
		"Monterey": {State: record.Unsupported, Detail: "declares known_fail on Monterey"},
	}, map[string]guest{"Sonoma": {Test: true}})
	body := PRBody(n, true, vouched())

	assert.Contains(t, body,
		"Verified with [dockhand]("+RepoURL+"):\n"+
			"  — Monterey: the port declines this platform (known_fail).\n"+
			"  — Sonoma: built and tested in a pristine VM.\n")
	assert.Contains(t, body, "Branch head `0123456789ab`.")
	assert.Contains(t, body, "- macOS Sonoma — built in a pristine VM, via dockhand")
	assert.Contains(t, body, "- [x] followed our [Commit Message Guidelines]")
	assert.Contains(t, body, "- [x] squashed and [minimized your commits]")
	assert.Contains(t, body, "- [x] tried existing tests with `sudo port test`?")
	assert.Contains(t, body, "- [x] checked that there aren't other open [pull requests]")
	assert.Contains(t, body,
		"- [x] tried a full install with ~~`sudo port -vst install`~~ `sudo port install` in a pristine VM")
	// What dockhand cannot vouch for stays with the human.
	assert.Contains(t, body, "- [ ] checked your Portfile with `port lint`?")
	// What dockhand can never answer is not left standing as an
	// unticked accusation.
	assert.NotContains(t, body, "tested basic functionality of all binary files")
	assert.NotContains(t, body, "haven't been broken")
}

func TestPRBodyWithoutTestsLeavesTheTestBoxOpen(t *testing.T) {
	n := templateNote(map[string]record.Run{"Sonoma": {State: record.Passed}}, nil)
	o := vouched()
	o.CheckedPRs = false
	body := PRBody(n, true, o)
	assert.Contains(t, body, "Sonoma: built in a pristine VM")
	assert.Contains(t, body, "- [ ] checked that there aren't other open [pull requests]")
	assert.Contains(t, body, "- [ ] tried existing tests with `sudo port test`?")
	assert.Contains(t, body, "- [x] tried a full install with")
}

// The sign-off names the build that wrote the body, so a sentence
// found to be wrong in a published PR can be traced to the version
// that wrote it — which is the whole reason this step exists.
func TestPRBodySignsOffEveryBodyWithItsVersion(t *testing.T) {
	signoff := "\nAutomated by [dockhand](" + RepoURL + ") 1.2.3\n"
	verified := templateNote(map[string]record.Run{"Sonoma": {State: record.Passed}}, nil)
	for name, body := range map[string]string{
		"verified":   PRBody(verified, true, vouched()),
		"unverified": PRBody(record.Record{}, false, PRBodyOpts{Version: "1.2.3", OwnCommits: 1}),
	} {
		assert.True(t, strings.HasSuffix(body, signoff), "%s body must end with the sign-off", name)
	}
}

// A version nothing set leaves the sign-off as it was rather than
// signing off with a blank where a version belongs.
func TestPRBodySignsOffWithoutAVersion(t *testing.T) {
	body := PRBody(record.Record{}, false, PRBodyOpts{OwnCommits: 1})
	assert.True(t, strings.HasSuffix(body, "\nAutomated by [dockhand]("+RepoURL+")\n"), body)
}

func TestPRBodyUnverifiedChecksNothing(t *testing.T) {
	body := PRBody(record.Record{}, false, PRBodyOpts{Version: "1.2.3", Closes: "12345", OwnCommits: 1})
	assert.Contains(t, body, "Not verified: there is no verification record for this branch head.")
	assert.Contains(t, body, "Closes: https://trac.macports.org/ticket/12345")
	assert.NotContains(t, body, "###### Tested on")
	// A commit-message box is still checkable — dockhand wrote it — but
	// no build claim survives an unverified promotion, and the boxes
	// only a run could have answered are gone rather than unticked.
	assert.NotContains(t, body, "- [x] tried")
	assert.NotContains(t, body, "port lint")
	assert.NotContains(t, body, "sudo port test")
	assert.NotContains(t, body, "pristine VM")
}

func TestPRBodyManyCommitsAreTheUsersToVouchFor(t *testing.T) {
	n := templateNote(map[string]record.Run{"Sonoma": {State: record.Passed}}, nil)
	o := vouched()
	o.OwnCommits, o.CheckedPRs = 3, false
	body := PRBody(n, true, o)
	assert.Contains(t, body, "- [ ] followed our [Commit Message Guidelines]")
	assert.Contains(t, body, "- [ ] squashed and [minimized your commits]")
}

// Every checklist line in the body must be one of the upstream
// template's own lines (modulo the strikethrough rewrite): a drifted
// checklist would read as dockhand inventing its own ceremony.
func TestPRBodyKeepsTheTemplateShape(t *testing.T) {
	n := templateNote(map[string]record.Run{"Sonoma": {State: record.Passed}},
		map[string]guest{"Sonoma": {Test: true}})
	o := vouched()
	o.Closes = "7"
	body := PRBody(n, true, o)
	require.True(t, strings.HasPrefix(body, "#### Description\n"))
	for _, section := range []string{"###### Type(s)", "###### Tested on", "###### Verification"} {
		assert.Contains(t, body, section)
	}
	assert.Equal(t, 10, strings.Count(body, "- ["),
		"3 type boxes + 3 always-answerable + the Trac box a ticket makes applicable + 3 a run makes applicable")
}

func TestPRBodyChecksLintWhenTheRunLinted(t *testing.T) {
	n := templateNote(map[string]record.Run{"Tahoe": {State: record.Passed, Linted: true, Lint: "clean"}}, nil)
	body := PRBody(n, true, vouched())
	assert.Contains(t, body, "- [x] checked your Portfile with `port lint`?")
	// The checked box is only honest if the evidence line states what
	// backs it — the field-caught gap.
	assert.Contains(t, body, "Tahoe: linted clean, built in a pristine VM")
}

func TestPRBodyStatesLintWarnings(t *testing.T) {
	n := templateNote(map[string]record.Run{"Tahoe": {State: record.Passed, Linted: true, Lint: "2 warnings"}},
		map[string]guest{"Tahoe": {Test: true}})
	body := PRBody(n, true, vouched())
	assert.Contains(t, body, "Tahoe: linted with 2 warnings, built and tested in a pristine VM")
	assert.Contains(t, body, "- [x] checked your Portfile with `port lint`?")
}

// The from-source claim is the run's own field and never an assumption.
// A bump installs a version whose binary archive does not exist yet, so
// an ordinary pass proves only that the port built from whatever the
// archive server had; a re-derivation at an unchanged version is told to
// ignore the archive, and only that run may say so.
func TestPRBodyClaimsFromSourceOnlyWhereTheRunSaysSo(t *testing.T) {
	archive := templateNote(map[string]record.Run{"Sequoia": {State: record.Passed}}, nil)
	assert.Contains(t, PRBody(archive, true, vouched()), "  — Sequoia: built in a pristine VM.\n")

	source := templateNote(map[string]record.Run{"Sequoia": {State: record.Passed, FromSource: true}}, nil)
	assert.Contains(t, PRBody(source, true, vouched()), "  — Sequoia: built from source in a pristine VM.\n")

	both := templateNote(map[string]record.Run{"Sequoia": {State: record.Passed, FromSource: true}},
		map[string]guest{"Sequoia": {Test: true}})
	assert.Contains(t, PRBody(both, true, vouched()),
		"  — Sequoia: built from source and tested in a pristine VM.\n")
}

// The unverified sentence was one fixed string, and it stated a cause
// that is true of exactly one of the shapes reaching it. Each cause now
// names itself, and no two of them read alike.
func TestPRBodyNamesTheCauseOfAnUnverifiedPromotion(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  record.Run
		want string
	}{
		{"failed", record.Run{State: record.Failed, Detail: "Failed to build jq: boom"},
			"  — Sequoia: the build failed, and this was promoted anyway.\n"},
		{"blocked", record.Run{State: record.Blocked, Detail: "dependency oniguruma fails to build"},
			"  — Sequoia: blocked before this change was reached.\n"},
		{"blocked with a neighbour", record.Run{State: record.Blocked, Blamed: "libwidget"},
			"  — Sequoia: blocked by libwidget, so this change was never reached.\n"},
		{"queued", record.Run{State: record.Queued, Detail: "2 of 2 workers busy"},
			"  — Sequoia: verification was asked for and is still queued.\n"},
		{"submitting", record.Run{State: record.Submitting},
			"  — Sequoia: verification was starting when this was promoted.\n"},
		{"running", record.Run{State: record.Running},
			"  — Sequoia: verification was still running when this was promoted.\n"},
		{"canceled", record.Run{State: record.Canceled, Detail: "canceled: promoted without waiting"},
			"  — Sequoia: verification was canceled before it finished.\n"},
		{"superseded", record.Run{State: record.Superseded, Detail: "canceled: the branch moved to abc"},
			"  — Sequoia: the branch moved out from under the run, and its verification was abandoned.\n"},
		{"errored", record.Run{State: record.Errored, Detail: "job vanished"},
			"  — Sequoia: the environment could not answer, which is a fact about the machine and not about the port.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := templateNote(map[string]record.Run{"Sequoia": tc.run}, nil)
			body := PRBody(n, false, PRBodyOpts{Version: "1.2.3", OwnCommits: 1})
			assert.Contains(t, body, "Not verified:\n")
			assert.Contains(t, body, tc.want)
			// The detail is the local log's words, and a public body
			// states the cause rather than quoting them.
			if tc.run.Detail != "" {
				assert.NotContains(t, body, tc.run.Detail)
			}
		})
	}
}

// The three no-run shapes are three different facts, and the sentence
// that used to be printed for all of them is now printed for the one it
// is true of.
func TestPRBodyTellsTheNoRunShapesApart(t *testing.T) {
	noRecord := PRBody(record.Record{}, false, PRBodyOpts{OwnCommits: 1})
	assert.Contains(t, noRecord, "Not verified: there is no verification record for this branch head.\n")

	toBranch := templateNote(nil, nil)
	toBranch.Destination = record.ToBranch
	assert.Contains(t, PRBody(toBranch, false, PRBodyOpts{OwnCommits: 1}),
		"Not verified: this branch was minted with --no-verify, so no verification was ever asked for.\n")

	noProvider := templateNote(nil, nil)
	noProvider.Destination = record.ToVerdict
	assert.Contains(t, PRBody(noProvider, false, PRBodyOpts{OwnCommits: 1}),
		"Not verified: no verification environment on the submitting machine, so nothing was run.\n")
}

// An unverified promotion that nonetheless has a passing platform says
// so: the header answers whether the change is verified, and the list
// answers what was actually established, which is what makes the
// checked install box honest on a body headed "Not verified".
func TestPRBodyStatesWhatPassedEvenWhenTheChangeIsNotVerified(t *testing.T) {
	n := templateNote(map[string]record.Run{
		"Sequoia": {State: record.Passed},
		"Sonoma":  {State: record.Failed, Detail: "Failed to build jq: boom"},
	}, nil)
	body := PRBody(n, false, PRBodyOpts{Version: "1.2.3", OwnCommits: 1})
	assert.Contains(t, body, "Not verified:\n"+
		"  — Sequoia: built in a pristine VM.\n"+
		"  — Sonoma: the build failed, and this was promoted anyway.\n")
	assert.Contains(t, body, "- macOS Sequoia — built in a pristine VM, via dockhand")
	assert.Contains(t, body, "- [x] tried a full install with")
}

// A verified body keeps its silence about the states that are not
// verdicts: a run this very promotion canceled is local business, and
// promote's own gate is where a failure is answered for.
func TestPRBodyVerifiedStaysSilentAboutNonVerdictStates(t *testing.T) {
	n := templateNote(map[string]record.Run{
		"Sequoia": {State: record.Passed},
		"Sonoma":  {State: record.Canceled, Detail: "canceled: promoted without waiting"},
	}, nil)
	body := PRBody(n, true, vouched())
	assert.Contains(t, body, "  — Sequoia: built in a pristine VM.\n")
	assert.NotContains(t, body, "Sonoma")
}

// The tree the change was written against is the base commit's, which
// is what tells a reviewer a rebase from a month-old branch.
func TestPRBodyStatesTheAgeOfTheTreeTheChangeWasWrittenAgainst(t *testing.T) {
	n := templateNote(map[string]record.Run{"Sequoia": {State: record.Passed}}, nil)
	n.Base = record.Base{Sha: "fedcba9876543210fedc",
		CommittedAt: time.Date(2026, 8, 25, 9, 30, 0, 0, time.UTC)}
	assert.Contains(t, PRBody(n, true, vouched()),
		"\nBranch head `0123456789ab`, against the ports tree as of 2026-08-25.\n")

	// A base nothing could resolve leaves the line saying only what is
	// known, rather than dating the tree to year one.
	n.Base = record.Base{}
	assert.Contains(t, PRBody(n, true, vouched()), "\nBranch head `0123456789ab`.\n")
	assert.NotContains(t, PRBody(n, true, vouched()), "ports tree as of")
}

// The riders ride under one "Also", in the note's own words: the body
// vouches for what the record remembers, not for what the diff can be
// re-read to contain.
func TestPRBodyListsRidersUnderOneAlso(t *testing.T) {
	n := templateNote(map[string]record.Run{"Sequoia": {State: record.Passed}}, nil)
	n.Riders = []string{"modeline"}
	assert.Contains(t, PRBody(n, true, vouched()), "\nAlso: modeline.\n")

	n.Riders = []string{"modeline", "whitespace"}
	body := PRBody(n, true, vouched())
	assert.Contains(t, body, "\nAlso: modeline, whitespace.\n")
	assert.Equal(t, 1, strings.Count(body, "\nAlso: "), "one Also, however many riders")

	n.Riders = nil
	assert.NotContains(t, PRBody(n, true, vouched()), "Also:")
}

// A box goes when this promotion could not have answered its question.
// An unticked box under "Have you" says a step was available and not
// taken, and that is a claim in its own right.
func TestPRBodyDeletesTheBoxesItCouldNotHaveAnswered(t *testing.T) {
	// Nothing ran: lint, tests and the pristine-VM install were never on
	// offer, and no ticket was named anywhere.
	unrun := PRBody(templateNote(nil, nil), false, PRBodyOpts{OwnCommits: 1})
	for _, gone := range []string{"port lint", "sudo port test", "pristine VM", "Trac"} {
		assert.NotContains(t, unrun, gone)
	}
	assert.Contains(t, unrun, "- [x] followed our [Commit Message Guidelines]")

	// A run that failed still tried, so the three stay and stay unticked.
	failed := templateNote(map[string]record.Run{"Sequoia": {State: record.Failed}}, nil)
	body := PRBody(failed, false, PRBodyOpts{OwnCommits: 1})
	assert.Contains(t, body, "- [ ] checked your Portfile with `port lint`?")
	assert.Contains(t, body, "- [ ] tried existing tests with `sudo port test`?")
	assert.Contains(t, body, "- [ ] tried a full install with")

	// The Trac box appears when a ticket is named anywhere, and reads
	// the record rather than the promotion's own argument.
	named := templateNote(nil, nil)
	named.ClosesTicket = "71234"
	assert.Contains(t, PRBody(named, false, PRBodyOpts{OwnCommits: 1}),
		"- [x] referenced existing tickets on [Trac]")
	assert.Contains(t, PRBody(templateNote(nil, nil), false, PRBodyOpts{OwnCommits: 1, Closes: "71234"}),
		"- [ ] referenced existing tickets on [Trac]")

	// The two dockhand can never answer are gone from every body.
	for _, body := range []string{unrun, PRBody(templateNote(
		map[string]record.Run{"Sequoia": {State: record.Passed}}, nil), true, vouched())} {
		assert.NotContains(t, body, "tested basic functionality of all binary files")
		assert.NotContains(t, body, "haven't been broken")
	}
}

// The sha in the header is abbreviated here rather than by internal/git,
// which this package must not import, so the width the body prints is
// pinned on its own: twelve digits of a full sha, and a short revision
// whole.
// The body restates git's abbreviation rule rather than importing the
// package that shells out to git. Two statements of one rule drift
// unless something holds them together, and a golden pins only what
// render printed — so the agreement is asserted here, in the one place
// allowed to see both.
func TestAbbreviationAgreesWithGits(t *testing.T) {
	for _, sha := range []string{
		"0123456789abcdef0123456789abcdef01234567",
		"0123456789abc",
		"0123456789ab",
		"0123",
		"",
	} {
		assert.Equal(t, git.Abbrev(sha), abbrevSha(sha), "sha %q", sha)
	}
}

func TestPRBodyAbbreviatesTheShaToTwelve(t *testing.T) {
	assert.Equal(t, "0123456789ab", abbrevSha("0123456789abcdef0123"))
	assert.Equal(t, "0123", abbrevSha("0123"))
	assert.Empty(t, abbrevSha(""))
}

// goldenDir holds one pinned body per reachable PRBody variant.
const goldenDir = "testdata/golden"

// bodyVariant is one input shape promote can hand PRBody today.
// The golden is named for the cause or the claim it pins, so a diff
// names what changed.
type bodyVariant struct {
	name string
	runs map[string]record.Run
	// guests is what each platform's run ran in. The test suite and the
	// kept environment live here because the record puts them here: one
	// guest is entered by every subject in the change, so neither is a
	// property of a verdict any more.
	guests map[string]guest
	// note shapes the record beyond its runs — the destination a mint
	// recorded, the riders folded in, the base the change sits on. They
	// are applied here rather than added as fields of their own because
	// each one is read by exactly one variant, and a table column that
	// is empty nineteen times out of twenty hides the one that is not.
	note     func(*record.Record)
	noRecord bool
	verified bool
	opts     PRBodyOpts
}

// vouchedOpts is the opts every golden starts from: a single minted
// commit, the duplicate search having run, and a pinned version so the
// sign-off is a fixed string rather than whatever built the test.
var vouchedOpts = PRBodyOpts{Version: "1.2.3", OwnCommits: 1, CheckedPRs: true}

// withCloses, withCommits and unchecked vary one field of it. They
// return a value rather than mutating, so a variant cannot reach into
// the table's shared opts.
func withCloses(t string) PRBodyOpts { o := vouchedOpts; o.Closes = t; return o }
func withCommits(n int) PRBodyOpts   { o := vouchedOpts; o.OwnCommits = n; return o }
func unchecked() PRBodyOpts          { o := vouchedOpts; o.CheckedPRs = false; return o }
func withHead(sha string) PRBodyOpts { o := vouchedOpts; o.Head = sha; return o }

// bodyVariants enumerates the branches in PRBody: the evidence line per
// run state and per lint/test/from-source record, the cause named for
// every shape an unverified promotion can arrive in, the sections and
// boxes that come and go with them, and the flags the checklist reads.
//
// The unverified half used to be three variants asserting that the note
// made no difference. It does now: the body names why a promotion
// carries no verification, and the one fixed sentence it used to print
// is left to the one shape it is true of.
var bodyVariants = []bodyVariant{
	{name: "verified_one_platform",
		runs:     map[string]record.Run{"Sequoia": {State: "passed"}},
		verified: true, opts: vouchedOpts},
	{name: "verified_several_platforms",
		runs: map[string]record.Run{
			"Sequoia": {State: "passed"},
			"Sonoma":  {State: "passed"},
			"Ventura": {State: "passed"},
		},
		verified: true, opts: vouchedOpts},
	{name: "verified_with_unsupported",
		runs: map[string]record.Run{
			"Sequoia":  {State: "passed"},
			"Monterey": {State: "unsupported", Detail: "declares known_fail on Monterey"},
		},
		verified: true, opts: vouchedOpts},
	{name: "verified_tested",
		runs:     map[string]record.Run{"Sequoia": {State: "passed"}},
		guests:   map[string]guest{"Sequoia": {Test: true}},
		verified: true, opts: vouchedOpts},
	// The archive claim and the from-source claim are different claims,
	// and only the run's own field may raise the second.
	{name: "verified_from_source",
		runs:     map[string]record.Run{"Sequoia": {State: "passed", FromSource: true}},
		verified: true, opts: vouchedOpts},
	{name: "verified_from_source_and_tested",
		runs:     map[string]record.Run{"Sequoia": {State: "passed", FromSource: true}},
		guests:   map[string]guest{"Sequoia": {Test: true}},
		verified: true, opts: vouchedOpts},
	{name: "verified_linted_without_summary",
		runs:     map[string]record.Run{"Sequoia": {State: "passed", Linted: true}},
		verified: true, opts: vouchedOpts},
	{name: "verified_lint_clean",
		runs:     map[string]record.Run{"Sequoia": {State: "passed", Linted: true, Lint: "clean"}},
		verified: true, opts: vouchedOpts},
	{name: "verified_lint_warnings",
		runs:     map[string]record.Run{"Sequoia": {State: "passed", Linted: true, Lint: "2 warnings"}},
		verified: true, opts: vouchedOpts},
	// A lint summary with no Linted record is a note nothing wrote:
	// the claim needs both halves, so neither the line nor the box
	// carries it. Its golden is byte-identical to verified_one_platform,
	// which is the point.
	{name: "verified_lint_summary_without_linted",
		runs:     map[string]record.Run{"Sequoia": {State: "passed", Lint: "clean"}},
		verified: true, opts: vouchedOpts},
	{name: "verified_tested_and_lint_clean",
		runs:     map[string]record.Run{"Sequoia": {State: "passed", Linted: true, Lint: "clean"}},
		guests:   map[string]guest{"Sequoia": {Test: true}},
		verified: true, opts: vouchedOpts},
	// The checklist boxes read the set: one tested, linted platform
	// checks them for the whole body while each evidence line keeps
	// its own record.
	{name: "verified_evidence_differs_by_platform",
		runs: map[string]record.Run{
			"Sequoia":  {State: "passed", Linted: true, Lint: "clean"},
			"Sonoma":   {State: "passed"},
			"Monterey": {State: "unsupported", Detail: "declares known_fail on Monterey"},
		},
		guests:   map[string]guest{"Sequoia": {Test: true}},
		verified: true, opts: vouchedOpts},
	// Every state that is not a verdict is local business: canceled
	// by this very promote, blocked on a neighbor, queued for a slot,
	// claimed but not yet started, superseded, errored. None of it
	// reaches the reviewer of a verified change, so the golden is
	// byte-identical to verified_one_platform.
	{name: "verified_omits_non_verdict_states",
		runs: map[string]record.Run{
			"Sequoia":  {State: "passed"},
			"Sonoma":   {State: "canceled", Detail: "canceled: promoted without waiting"},
			"Ventura":  {State: "blocked", Detail: "dependency oniguruma failed to build"},
			"Monterey": {State: "queued", Detail: "2 of 2 workers busy"},
			"Tahoe":    {State: "submitting"},
			"Big Sur":  {State: "errored", Detail: "worker vanished"},
		},
		verified: true, opts: vouchedOpts},
	{name: "verified_with_closes",
		runs:     map[string]record.Run{"Sequoia": {State: "passed"}},
		verified: true, opts: withCloses("71234")},
	// A ticket the record itself carries is the one the Trac box reads:
	// it is in the minted commit's trailer, which is what the box asks
	// about.
	{name: "verified_with_the_ticket_on_the_record",
		runs:     map[string]record.Run{"Sequoia": {State: "passed"}},
		note:     func(n *record.Record) { n.ClosesTicket = "71234" },
		verified: true, opts: withCloses("71234")},
	{name: "verified_with_tree_age",
		runs: map[string]record.Run{"Sequoia": {State: "passed"}},
		note: func(n *record.Record) {
			n.Base = record.Base{Sha: "fedcba9876543210fedc",
				CommittedAt: time.Date(2026, 8, 25, 9, 30, 0, 0, time.UTC)}
		},
		verified: true, opts: vouchedOpts},
	// The tip carries no note of its own and the ledger answered with a
	// record found over the identical tree: a reworded amend, a rebase.
	// The head is what was pushed, and the record's sha says where the
	// verification happened.
	{name: "verified_head_differs_from_the_record",
		runs:     map[string]record.Run{"Sequoia": {State: record.Passed}},
		verified: true, opts: withHead("fedcba9876543210fedc")},
	{name: "verified_with_riders",
		runs:     map[string]record.Run{"Sequoia": {State: "passed"}},
		note:     func(n *record.Record) { n.Riders = []string{"modeline"} },
		verified: true, opts: vouchedOpts},
	{name: "verified_many_commits",
		runs:     map[string]record.Run{"Sequoia": {State: "passed"}},
		verified: true, opts: withCommits(3)},
	{name: "verified_prs_unchecked",
		runs:     map[string]record.Run{"Sequoia": {State: "passed"}},
		verified: true, opts: unchecked()},
	// The record exists, its contract reaches past the branch, and no
	// run was ever written: the machine had no verification environment
	// to submit to. This is the one shape the old fixed sentence was
	// true of, and it keeps it.
	{name: "unverified_no_environment",
		note: func(n *record.Record) { n.Destination = record.ToVerdict },
		opts: vouchedOpts},
	// Minted with --no-verify: nobody ever asked for a verdict, and the
	// pump steps over it. A different fact from having nowhere to run.
	{name: "unverified_minted_to_branch",
		note: func(n *record.Record) { n.Destination = record.ToBranch },
		opts: vouchedOpts},
	// No note on the tip at all, which is the zero record the ledger
	// hands back. Nothing is known, so nothing but that is said.
	{name: "unverified_no_record",
		noRecord: true, opts: vouchedOpts},
	// --no-verify past a failed build: the refusal was overridden, and
	// the body says which platform failed. The failure's own detail is
	// the local log's words and stays local.
	{name: "unverified_failed_overridden",
		runs:   map[string]record.Run{"Sequoia": {State: "failed", Detail: "Failed to build jq: boom"}},
		guests: map[string]guest{"Sequoia": {Handle: "dockhand-jq-1"}},
		opts:   vouchedOpts},
	// One platform's failure does not erase another's pass: the header
	// answers whether the change is verified, the list answers what was
	// established, and the install box is checked because an install
	// really did happen.
	{name: "unverified_failed_beside_a_pass",
		runs: map[string]record.Run{
			"Sequoia": {State: "passed", Linted: true, Lint: "clean"},
			"Sonoma":  {State: "failed", Detail: "Failed to build jq: boom"},
		},
		opts: vouchedOpts},
	{name: "unverified_blocked_and_canceled",
		runs: map[string]record.Run{
			"Sequoia": {State: "blocked", Detail: "dependency oniguruma failed to build"},
			"Sonoma":  {State: "canceled", Detail: "canceled: promoted without waiting"},
		},
		opts: vouchedOpts},
	// Blamed names the neighbour whose failure this run inherited. It is
	// written only for a blamed port that is itself a member of the
	// cohort, so this is the cohort shape and not the one that ships at
	// a single subject.
	{name: "unverified_blocked_by_a_named_neighbour",
		runs: map[string]record.Run{"Sequoia": {State: "blocked", Blamed: "libwidget"}},
		opts: vouchedOpts},
	{name: "unverified_queued",
		runs: map[string]record.Run{"Sequoia": {State: "queued", Detail: "2 of 2 workers busy"}},
		opts: vouchedOpts},
	{name: "unverified_submitting",
		runs: map[string]record.Run{"Sequoia": {State: "submitting"}},
		opts: vouchedOpts},
	{name: "unverified_running",
		runs: map[string]record.Run{"Sequoia": {State: "running"}},
		opts: vouchedOpts},
	{name: "unverified_canceled",
		runs: map[string]record.Run{"Sequoia": {State: "canceled", Detail: "canceled by the user"}},
		opts: vouchedOpts},
	{name: "unverified_superseded",
		runs: map[string]record.Run{"Sequoia": {State: "superseded",
			Detail: "canceled: the branch moved to 0123456789ab"}},
		note: func(n *record.Record) { n.SupersededBy = "dockhand/jq-1.9" },
		opts: vouchedOpts},
	{name: "unverified_errored",
		runs: map[string]record.Run{"Sequoia": {State: "errored",
			Detail: "job vanished: its worker no longer exists"}},
		opts: vouchedOpts},
	{name: "unverified_with_riders",
		note: func(n *record.Record) {
			n.Destination = record.ToBranch
			n.Riders = []string{"modeline"}
		},
		opts: vouchedOpts},
	{name: "unverified_with_closes",
		note: func(n *record.Record) { n.Destination = record.ToVerdict },
		opts: withCloses("71234")},
	{name: "unverified_many_commits",
		note: func(n *record.Record) { n.Destination = record.ToVerdict },
		opts: withCommits(3)},
	{name: "unverified_prs_unchecked",
		note: func(n *record.Record) { n.Destination = record.ToVerdict },
		opts: unchecked()},

	// The cohort. Three dispositions, three bodies: a proposal a person
	// accepted is a list of revbumped ports with the link proof each
	// one's own run recorded; one they promoted past is said out loud
	// rather than dressed up as a cohort; and one they dismissed is a
	// decision a reviewer is entitled to see. All three restate the
	// criterion verbatim, once.
	{name: "verified_cohort",
		note:     func(n *record.Record) { *n = cohortNoteFor(record.Accepted, cohortLinks) },
		verified: true, opts: withCommits(2)},
	{name: "verified_cohort_proposed",
		note:     func(n *record.Record) { *n = cohortNoteFor(record.Proposed, nil) },
		verified: true, opts: vouchedOpts},
	{name: "verified_cohort_dismissed",
		note:     func(n *record.Record) { *n = cohortNoteFor(record.Dismissed, nil) },
		verified: true, opts: vouchedOpts},
}

// Each variant's whole body is pinned: a checklist box flipping, a
// section appearing, or a phrase drifting is a change in what dockhand
// vouches for upstream, and the golden makes it a reviewed one.
func TestPRBodyGoldens(t *testing.T) {
	rendered := map[string]bool{}
	for _, v := range bodyVariants {
		require.False(t, rendered[v.name], "variant %q is listed twice", v.name)
		rendered[v.name] = true
		t.Run(v.name, func(t *testing.T) {
			checkGolden(t, v.name, PRBody(v.record(), v.verified, v.opts))
		})
	}
	// A golden no variant renders is a shape that stopped existing; it
	// leaves with the code that produced it rather than lingering as a
	// claim nobody checks.
	entries, err := os.ReadDir(goldenDir)
	require.NoError(t, err)
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".golden")
		assert.True(t, rendered[name], "stale golden %s: no variant renders it", filepath.Join(goldenDir, e.Name()))
	}
}

// record builds the variant's input: the templated note with whatever
// the variant shapes onto it, or the zero record for a tip that has no
// note at all.
func (v bodyVariant) record() record.Record {
	if v.noRecord {
		return record.Record{}
	}
	n := templateNote(v.runs, v.guests)
	if v.note != nil {
		v.note(&n)
	}
	return n
}

// Three of the variants exist to say that some input makes no
// difference to the body, and each says it by rendering to a file
// already on disk under another name. Prose in the variant table
// claimed that; this checks it, so an edit that starts distinguishing
// these cases fails here rather than passing with two goldens quietly
// rewritten together.
//
// There used to be a second group, and its whole point was that an
// unverified body ignored the note: a failed build and a blocked one
// and a machine with no provider all rendered the same bytes. That
// identity is what published a false cause, so the group is gone and
// its members are pinned apart below.
func TestPRBodyIdenticalVariantsStayIdentical(t *testing.T) {
	for _, group := range [][]string{
		{"verified_one_platform", "verified_omits_non_verdict_states", "verified_lint_summary_without_linted"},
	} {
		first, err := os.ReadFile(filepath.Join(goldenDir, group[0]+".golden"))
		require.NoError(t, err)
		for _, name := range group[1:] {
			got, err := os.ReadFile(filepath.Join(goldenDir, name+".golden"))
			require.NoError(t, err)
			assert.Equal(t, string(first), string(got),
				"%s must be byte-identical to %s; that identity is what the variant exists to state", name, group[0])
		}
	}
}

// No two unverified causes render alike. The bodies these name were one
// file rendered under several names until this step, which is exactly
// how a promotion blocked on a neighbour's build told upstream there was
// no verification environment on the machine.
func TestPRBodyUnverifiedCausesAreAllDifferent(t *testing.T) {
	seen := map[string]string{}
	for _, name := range []string{
		"unverified_no_environment",
		"unverified_minted_to_branch",
		"unverified_no_record",
		"unverified_failed_overridden",
		"unverified_blocked_and_canceled",
		"unverified_blocked_by_a_named_neighbour",
		"unverified_queued",
		"unverified_submitting",
		"unverified_running",
		"unverified_canceled",
		"unverified_superseded",
		"unverified_errored",
	} {
		body, err := os.ReadFile(filepath.Join(goldenDir, name+".golden"))
		require.NoError(t, err)
		if other, dup := seen[string(body)]; dup {
			t.Errorf("%s renders the same bytes as %s; each cause must name itself", name, other)
		}
		seen[string(body)] = name
	}
}

// checkGolden compares got with testdata/golden/<name>.golden, or
// rewrites it under -update. A mismatch prints the line diff and the
// remedy, so the failure reads as the change it is.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	checkGoldenIn(t, goldenDir, name, got)
}

// checkGoldenIn is checkGolden with the directory named, for the
// goldens that are not pull request bodies: a commit message is pinned
// the same way and swept for staleness by a different test, so it must
// not sit in the directory that sweep reads.
func checkGoldenIn(t *testing.T, dir, name, got string) {
	t.Helper()
	path := filepath.Join(dir, name+".golden")
	if *update {
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "no golden at %s; `go test ./internal/render -update` writes it", path)
	if string(want) != got {
		t.Errorf("body differs from %s:\n%s\nif the new body is intended, `go test ./internal/render -update` rewrites the golden", path, lineDiff(path, string(want), got))
	}
}

// lineDiff renders want against got as a unified diff with every line
// as context: the bodies are a few dozen lines, so trimming to hunks
// would hide more than it saves. Hand-rolled over a quadratic LCS
// because go-cmp is not vendored and the inputs are tiny.
func lineDiff(path, want, got string) string {
	a, b := diffLines(want), diffLines(got)
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out strings.Builder
	out.WriteString("--- want (" + path + ")\n+++ got\n")
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out.WriteString(" " + a[i] + "\n")
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			out.WriteString("-" + a[i] + "\n")
			i++
		default:
			out.WriteString("+" + b[j] + "\n")
			j++
		}
	}
	for ; i < len(a); i++ {
		out.WriteString("-" + a[i] + "\n")
	}
	for ; j < len(b); j++ {
		out.WriteString("+" + b[j] + "\n")
	}
	return out.String()
}

// diffLines splits text for the diff, marking a missing final newline
// as its own line so the one difference a line split hides still shows.
func diffLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	if !strings.HasSuffix(s, "\n") {
		lines = append(lines, `\ No newline at end of file`)
	}
	return lines
}

// A cohort's evidence lines name the member each one is about. Nine
// members that all built on Sequoia would otherwise produce nine
// identical lines, which reads as one claim repeated rather than nine
// ports vouched for — and the "Tested on" section still names one
// environment per platform, because that is how many there were.
func TestPRBodyNamesEachMemberOfACohortOnce(t *testing.T) {
	n := record.Record{
		Sha:      "0123456789abcdef0123",
		Subjects: []record.Subject{{Port: "libwidget"}, {Port: "widget-tools"}},
		Jobs: map[string]record.JobRecord{
			"Sequoia": {Job: verify.Job{Provider: "fake", ID: "fake-1"}, Test: true, Released: true},
		},
		Runs: map[string]record.Run{
			record.RunKey("libwidget", "Sequoia"):    {State: record.Passed, Platform: "Sequoia"},
			record.RunKey("widget-tools", "Sequoia"): {State: record.Passed, Platform: "Sequoia"},
		},
	}
	body := PRBody(n, true, vouched())
	assert.Contains(t, body,
		"  — libwidget on Sequoia: built and tested in a pristine VM.\n"+
			"  — widget-tools on Sequoia: built and tested in a pristine VM.\n")
	assert.Equal(t, 1, strings.Count(body, "- macOS Sequoia — built in a pristine VM, via dockhand"),
		"two verdicts, one environment")
}

// A cohort member that was never built is named on a verified body too.
//
// The gate refuses this shape now — Promotable answers for every
// subject, and a member with no pass anywhere has nothing behind it —
// so what this pins is the second line of the same defence. A body is
// rendered from whatever record it is handed, including one whose
// subjects nothing named (a note the gate answers from the runs alone),
// and the suppression that keeps this promotion's own cancellations out
// of a verified body must not take the blamed sentence with it: that
// would publish "verified with dockhand" over a port nothing built and
// delete the line saying so.
func TestPRBodyNamesACohortMemberThatNeverBuilt(t *testing.T) {
	n := record.Record{
		Sha:      "0123456789abcdef0123",
		Subjects: []record.Subject{{Port: "libwidget"}, {Port: "jq"}},
		Jobs:     map[string]record.JobRecord{"Sequoia": {Job: verify.Job{Provider: "fake", ID: "fake-1"}}},
		Runs: map[string]record.Run{
			record.RunKey("libwidget", "Sequoia"): {State: record.Passed, Platform: "Sequoia"},
			record.RunKey("jq", "Sequoia"): {State: record.Blocked, Platform: "Sequoia",
				Blamed: "libwidget"},
		},
	}
	require.False(t, n.Promotable(),
		"a member with no pass anywhere is what the gate now refuses; this body is rendered past it")
	body := PRBody(n, true, vouched())
	assert.Contains(t, body,
		"  — libwidget on Sequoia: built in a pristine VM.\n"+
			"  — jq on Sequoia: blocked by libwidget, so this change was never reached.\n")

	// The suppression it is carved out of still holds: a subject that DID
	// build keeps its unfinished runs local.
	n.Runs[record.RunKey("jq", "Sonoma")] = record.Run{State: record.Passed, Platform: "Sonoma"}
	n.Jobs["Sonoma"] = record.JobRecord{Job: verify.Job{Provider: "fake", ID: "fake-2"}}
	n.Runs[record.RunKey("libwidget", "Sonoma")] = record.Run{State: record.Canceled, Platform: "Sonoma"}
	assert.NotContains(t, PRBody(n, true, vouched()), "canceled")
}

// The pristine-VM claim is the environment's own, read from the run
// where the settling provider stamped it. render stated it from a
// literal that was true of the only provider that ships, and would have
// gone on stating it over the first backend that proves less.
func TestPRBodyStatesTheEnvironmentsOwnClaim(t *testing.T) {
	weak := templateNote(map[string]record.Run{
		"Sequoia": {State: record.Passed, FromSource: true,
			Evidence: "built on a shared runner"},
	}, map[string]guest{"Sequoia": {Test: true}})
	body := PRBody(weak, true, vouched())
	assert.Contains(t, body, "  — Sequoia: built from source and tested on a shared runner.\n")
	assert.Contains(t, body, "- macOS Sequoia — built on a shared runner, via dockhand")
	// Neither claim render words is a pristine-VM claim any more. The one
	// remaining mention is upstream's own checklist line, which is the
	// template's wording and not dockhand's sentence.
	assert.NotContains(t, body, "in a pristine VM.")
	assert.NotContains(t, body, "pristine VM, via dockhand")

	// A run settled before the field existed keeps the phrase it was
	// rendered with, so no published body loses its claim to a migration.
	old := templateNote(map[string]record.Run{"Sequoia": {State: record.Passed}}, nil)
	assert.Contains(t, PRBody(old, true, vouched()), "  — Sequoia: built in a pristine VM.\n")
}

// The head a promotion publishes and the commit its record hangs on are
// two facts, and this line stated the second under the first one's name.
// EvidenceFor answers a tip with no note of its own with a record found
// over the identical tree at another sha — the reworded amend, the
// rebase — and that sha is on the notes ref and on no branch.
func TestPRBodyNamesTheHeadBeingPublished(t *testing.T) {
	n := templateNote(map[string]record.Run{"Sequoia": {State: record.Passed}}, nil)
	o := vouched()

	o.Head = n.Sha
	assert.Contains(t, PRBody(n, true, o), "\nBranch head `0123456789ab`.\n")

	o.Head = "fedcba9876543210fedc"
	assert.Contains(t, PRBody(n, true, o),
		"\nBranch head `fedcba987654`, verified at `0123456789ab` (identical tree).\n")

	// A caller that says nothing still gets the record's own sha, which
	// is what this line was before promote learned to pass its tip.
	o.Head = ""
	assert.Contains(t, PRBody(n, true, o), "\nBranch head `0123456789ab`.\n")

	// A tip with no record at all: the head is the only sha there is.
	o.Head = "fedcba9876543210fedc"
	assert.Contains(t, PRBody(record.Record{}, false, o), "\nBranch head `fedcba987654`.\n")
}

// The three run-derived boxes are printed for a run that reached a
// verdict, not for a run RECORD. A promotion that overtook a queued or
// canceled run was no more in a position to lint, test or install than
// one with no record at all, and three unchecked boxes over it are the
// same false implication the deletions exist to retire.
func TestPRBodyDeletesTheRunBoxesWhenNothingRan(t *testing.T) {
	for _, state := range []record.RunState{
		record.Queued, record.Submitting, record.Running,
		record.Canceled, record.Superseded, record.Errored, record.Blocked,
	} {
		body := PRBody(templateNote(map[string]record.Run{"Sequoia": {State: state}}, nil),
			false, PRBodyOpts{OwnCommits: 1})
		for _, gone := range []string{"port lint", "sudo port test", "tried a full install"} {
			assert.NotContains(t, body, gone, "state %s", state)
		}
	}
	// An unsupported run is a verdict: the platform was asked and
	// answered, so the boxes stand.
	answered := templateNote(map[string]record.Run{
		"Monterey": {State: record.Unsupported, Detail: "declares known_fail on Monterey"},
	}, nil)
	assert.Contains(t, PRBody(answered, false, PRBodyOpts{OwnCommits: 1}),
		"- [ ] checked your Portfile with `port lint`?")
}

// A single change's lines carry no port: the PR is about that port and
// its title says so, and prefixing every line would be noise in the one
// place candour is the whole point.
func TestPRBodyDoesNotNameTheSubjectOfASingleChange(t *testing.T) {
	body := PRBody(templateNote(map[string]record.Run{"Sequoia": {State: record.Passed}}, nil), true, vouched())
	assert.Contains(t, body, "  — Sequoia: built in a pristine VM.\n")
	assert.NotContains(t, body, "jq on Sequoia")
}
