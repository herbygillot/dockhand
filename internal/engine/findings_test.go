package engine

// The findings hook end to end, over the Fake with scripted manifests.
//
// The reverse index under it is the REAL one: the captured PortIndex
// slice, staged into a ports-tree-shaped repository, so a proposal's
// members are the ports that actually declare judy and their portdirs
// are the ones the index writes. That is what the whole step turns on —
// half the indexed names in a real tree match no portdir basename, and
// judy's seven php*-Judy dependents all live in php/php-Judy — and a
// synthetic index agreeing with a synthetic reader would have proved
// nothing about either.
//
// Five cases, and four of them are refusals: the measurement says
// something moved, says nothing moved, could not be made for want of a
// baseline, could not be made because the environment cannot describe
// an installation, and was never asked because nothing depends on the
// port. Only the first proposes.

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/intent/bumprevision"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// indexedRepo is a ports-tree-shaped repository carrying the captured
// PortIndex slice, with one dockhand branch on it for the named port.
func indexedRepo(t *testing.T, port string) (*git.Repo, string) {
	t.Helper()
	ctx := context.Background()
	root := testenv.PortIndexTree(t)
	repo := gittest.Init(t, realTools, root, map[string]string{
		"sysutils/" + port + "/Portfile": "version 1.0\n",
		// Two of judy's real dependents, at the portdirs the index gives
		// them, so a cohort can be planned over both. php/php-Judy is
		// the subport collapse — seven indexed names, one directory —
		// and it is deliberately the shape a revbump refuses to guess a
		// placement for, so one cohort exercises both halves.
		"sysutils/netdata/Portfile": memberPortfile("netdata", "revision            4\n"),
		"php/php-Judy/Portfile":     setVersionPortfile("php-Judy"),
		"sysutils/other/Portfile":   memberPortfile("other", ""),
	})
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	sha := gittest.Commit(t, repo, "dockhand/"+port+"-1.1", primary,
		"sysutils/"+port+"/Portfile", "version 1.1\n", port+": update to 1.1")
	return repo, sha
}

// indexed builds an engine whose tree is the captured index, so
// dependents come from the real thing.
func indexed(t *testing.T, repo *git.Repo, prov verify.Verifier) *Engine {
	t.Helper()
	e := testState(t, repo, nil)
	e.Verifier = func(context.Context) (verify.Verifier, error) { return prov, nil }
	e.Lister = e.Verifier
	e.Tree = func() (*tree.Tree, error) { return tree.Open(repo.Root) }
	return e
}

// measured is a Fake that describes an installation, with the
// comparison a test scripts.
func measured(m verify.Manifests) *verifytest.Fake {
	return &verifytest.Fake{
		CanManifest: true,
		States:      map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}},
		Inventory:   map[string]verify.Manifests{"fake-1": m},
	}
}

// dylib is one library row as the capture produces it.
func dylib(path, name, compat string) verify.Dylib {
	return verify.Dylib{Path: path, Arch: "arm64", InstallName: name,
		CompatVersion: compat, CurrentVersion: compat}
}

// installed is one side of a comparison.
func installed(port, version string, libs ...verify.Dylib) *verify.Manifest {
	return &verify.Manifest{Port: port, Version: version, Platform: "Testos",
		Files: []string{"/opt/local/lib"}, Dylibs: libs}
}

// noteOn writes a running note for one subject on the tip.
func noteOn(t *testing.T, repo *git.Repo, sha, port string) record.Record {
	t.Helper()
	n, err := ledger.Open(repo).LoadOrStart(context.Background(), sha)
	require.NoError(t, err)
	n.Subjects = []record.Subject{{Port: port, Names: []string{port},
		Portdir: "sysutils/" + port, Intent: "bump", Target: "1.1"}}
	startedFor(&n, port, "Testos", "fake-1", record.Run{State: record.Running, Linted: true})
	require.NoError(t, ledger.Open(repo).Write(context.Background(), n))
	return n
}

// findingOf reads one kind off a settled record.
func findingOf(n record.Record, kind string) (record.Finding, bool) {
	for _, f := range n.Findings {
		if f.Kind == kind {
			return f, true
		}
	}
	return record.Finding{}, false
}

// The measurement says an install name moved: the ABI finding records
// what was compared, and the proposal puts the dependents forward for a
// person to accept.
func TestAMeasuredABIChangeProposesTheDependents(t *testing.T) {
	repo, sha := indexedRepo(t, "judy")
	n := noteOn(t, repo, sha, "judy")
	fake := measured(verify.Manifests{
		BaselineSource: verify.BaselineArchive,
		Baseline: installed("judy", "1.0.5_0",
			dylib("/opt/local/lib/libJudy.1.0.0.dylib", "/opt/local/lib/libJudy.1.dylib", "1.0.0")),
		Installed: installed("judy", "1.1.0_0",
			dylib("/opt/local/lib/libJudy.2.0.0.dylib", "/opt/local/lib/libJudy.2.dylib", "2.0.0")),
	})

	require.NoError(t, indexed(t, repo, fake).settle(context.Background(), repo, &n))

	abi, ok := findingOf(n, "abi-change")
	require.True(t, ok, "the measurement is recorded whatever it concluded")
	assert.Equal(t, record.Accepted, abi.Disposition,
		"a measurement is not a question anybody can answer")
	assert.Contains(t, abi.Criterion, "install name /opt/local/lib/libJudy.1.dylib → /opt/local/lib/libJudy.2.dylib")

	cohort, ok := findingOf(n, "dependent-revbump")
	require.True(t, ok)
	assert.Equal(t, record.Proposed, cohort.Disposition,
		"the proposal is the question, and the machine gate holds until it is answered")
	assert.Equal(t, abi.Criterion, cohort.Criterion,
		"one claim, said once: the commit body and the pull request restate this sentence verbatim")

	// The real index: netdata and the seven php*-Judy subports, whose
	// portdir is the parent's and not their own name.
	dirs := map[string]string{}
	for _, c := range cohort.Candidates {
		dirs[c.Port] = c.Portdir
	}
	assert.Equal(t, "sysutils/netdata", dirs["netdata"])
	assert.Equal(t, "php/php-Judy", dirs["php80-Judy"], "a subport is staged at its parent's directory")
	assert.Equal(t, "php/php-Judy", dirs["php86-Judy"])
	assert.Len(t, cohort.Candidates, 8, "judy's whole declared roster is examined")

	// No link proof anywhere yet, and that is the honest state of this
	// run: only the headline was built, and the question a link proof
	// answers is whether the DEPENDENTS still bind to it. The proof
	// arrives with the cohort's own verification, on each member's own
	// run — see TestTheCohortsOwnVerificationProvesEachMemberSLink.
	assert.Nil(t, runFor(n, "judy", "Testos").Links,
		"nobody was built to link, which a missing key says and an empty list would not")
	assert.Equal(t, verify.BaselineArchive, runFor(n, "judy", "Testos").BaselineSource)
}

// Nothing moved. It is a real result and not an absence — an up-front
// cohort refuted by measurement rests on it — so the finding is
// recorded and nothing is proposed.
func TestAnUnchangedABIProposesNothingAndSaysSo(t *testing.T) {
	repo, sha := indexedRepo(t, "judy")
	n := noteOn(t, repo, sha, "judy")
	same := dylib("/opt/local/lib/libJudy.1.0.0.dylib", "/opt/local/lib/libJudy.1.dylib", "1.0.0")
	fake := measured(verify.Manifests{
		BaselineSource: verify.BaselineArchive,
		Baseline:       installed("judy", "1.0.5_0", same),
		Installed:      installed("judy", "1.0.6_0", same),
	})

	require.NoError(t, indexed(t, repo, fake).settle(context.Background(), repo, &n))

	_, ok := findingOf(n, "abi-unchanged")
	assert.True(t, ok, "an unchanged measurement is a result the pull request has to be able to state")
	_, proposed := findingOf(n, "dependent-revbump")
	assert.False(t, proposed, "nothing moved, so no dependent needs a revision bump on this evidence")
	assert.False(t, AnyProposed(n), "and nothing holds an unattended publication")
}

// No baseline: the comparison could not be made, which is never a
// stand-in for "nothing moved" — an absent baseline compares as every
// library removed, the strongest false break there is.
func TestAMissingBaselineIsUnavailableAndNamesWhy(t *testing.T) {
	repo, sha := indexedRepo(t, "judy")
	n := noteOn(t, repo, sha, "judy")
	fake := measured(verify.Manifests{
		BaselineSource: verify.BaselineNone,
		BaselineReason: "the port did not exist at the merge base",
		Installed: installed("judy", "1.1.0_0",
			dylib("/opt/local/lib/libJudy.2.0.0.dylib", "/opt/local/lib/libJudy.2.dylib", "2.0.0")),
	})

	require.NoError(t, indexed(t, repo, fake).settle(context.Background(), repo, &n))

	abi, ok := findingOf(n, "abi-unavailable")
	require.True(t, ok)
	assert.Contains(t, abi.Criterion, "no baseline for judy")
	assert.Contains(t, abi.Criterion, "the port did not exist at the merge base",
		"the environment's own account is repeated verbatim: \"none\" alone is the shape of a guess")
	assert.Contains(t, abi.Criterion, "nothing banks one yet",
		"what the comparison needed, said without offering a command that would not produce it")
	assert.NotContains(t, abi.Criterion, "dockhand verify",
		"the sentence this replaced sent a reader in a circle: there is no bank for it to fill")
	_, proposed := findingOf(n, "dependent-revbump")
	assert.False(t, proposed)
}

// A provider that declares it can describe an installation and
// implements no Manifester is refused by name. The declaration is what
// the submit read to ask for a manifest at all, and the two gates are
// deliberately apart: a provider reconfigured between them produces a
// request nobody can answer, and reporting that as an ABI result would
// be reporting a measurement that was never taken.
func TestAProviderThatCannotDescribeIsUnavailableByName(t *testing.T) {
	repo, sha := indexedRepo(t, "judy")
	n := noteOn(t, repo, sha, "judy")
	fake := measured(verify.Manifests{})

	require.NoError(t, indexed(t, repo, verifytest.Incapable{Fake: fake}).settle(context.Background(), repo, &n))

	abi, ok := findingOf(n, "abi-unavailable")
	require.True(t, ok)
	assert.Contains(t, abi.Criterion, "cannot describe an installation")
	assert.Nil(t, runFor(n, "judy", "Testos").Links,
		"nobody looked, which a missing key says and an empty list would not")
}

// A port nothing depends on is never measured. The one consumer of an
// ABI measurement is the cohort decision, so a bump of a leaf port
// settles exactly as it always has — no findings, and a note whose
// bytes have not moved.
func TestAPortWithNoDependentsIsNeverMeasured(t *testing.T) {
	repo, sha := indexedRepo(t, "jq")
	n := noteOn(t, repo, sha, "jq")
	fake := measured(verify.Manifests{
		BaselineSource: verify.BaselineArchive,
		Baseline: installed("jq", "1.7_0",
			dylib("/opt/local/lib/libjq.1.0.0.dylib", "/opt/local/lib/libjq.1.dylib", "1.0.0")),
		Installed: installed("jq", "1.8_0",
			dylib("/opt/local/lib/libjq.2.0.0.dylib", "/opt/local/lib/libjq.2.dylib", "2.0.0")),
	})

	require.NoError(t, indexed(t, repo, fake).settle(context.Background(), repo, &n))

	assert.Empty(t, n.Findings,
		"nothing declares jq in this index, so there is no cohort to propose and nothing to say about one")
	assert.Equal(t, record.Passed, runFor(n, "jq", "Testos").State)
}

// A dependent another branch is already carrying is excluded by name.
// Two branches revbumping one port is two revisions and a conflict at
// merge, and a scan that read only each branch's headline would miss a
// port carried as somebody else's cohort member.
func TestADependentAlreadyInFlightIsExcludedByName(t *testing.T) {
	repo, sha := indexedRepo(t, "judy")
	ctx := context.Background()
	// A second branch whose change carries netdata as a member.
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	other := gittest.Commit(t, repo, "dockhand/other-1.0", primary,
		"sysutils/other/Portfile", "version 1\n", "other: update")
	on, err := ledger.Open(repo).LoadOrStart(ctx, other)
	require.NoError(t, err)
	on.Subjects = []record.Subject{
		{Port: "other", Names: []string{"other"}, Portdir: "sysutils/other"},
		{Port: "netdata", Names: []string{"netdata"}, Portdir: "sysutils/netdata"},
	}
	require.NoError(t, ledger.Open(repo).Write(ctx, on))

	n := noteOn(t, repo, sha, "judy")
	fake := measured(verify.Manifests{
		BaselineSource: verify.BaselineArchive,
		Baseline: installed("judy", "1.0.5_0",
			dylib("/opt/local/lib/libJudy.1.0.0.dylib", "/opt/local/lib/libJudy.1.dylib", "1.0.0")),
		Installed: installed("judy", "1.1.0_0",
			dylib("/opt/local/lib/libJudy.2.0.0.dylib", "/opt/local/lib/libJudy.2.dylib", "2.0.0")),
	})
	require.NoError(t, indexed(t, repo, fake).settle(ctx, repo, &n))

	cohort, ok := findingOf(n, "dependent-revbump")
	require.True(t, ok)
	for _, c := range cohort.Candidates {
		if c.Port != "netdata" {
			continue
		}
		assert.False(t, c.Proposed)
		assert.Contains(t, c.Reason, "already in flight on dockhand/other-1.0")
		return
	}
	t.Fatal("netdata was not examined at all")
}

// memberPortfile is a cohort member as the tests plan it: a real
// Portfile, because the plan is a shadow evaluation and MacPorts is
// what evaluates it.
func memberPortfile(name, extra string) string {
	return "PortSystem          1.0\n\n" +
		"name                " + name + "\n" +
		"version             2.0\n" + extra +
		"categories          sysutils\n" +
		"maintainers         nomaintainer\n" +
		"license             MIT\n" +
		"description         a synthetic cohort member\n" +
		"long_description    A synthetic dependent for dockhand's cohort tests.\n"
}

// setVersionPortfile carries its version in a `set` variable and writes
// no revision line: the shape whose own structure does not say where an
// inserted revision belongs, which a revbump declines by name rather
// than guessing at.
func setVersionPortfile(name string) string {
	return "PortSystem          1.0\n\n" +
		"name                " + name + "\n" +
		"set my_version      1.0.5\n" +
		"version             ${my_version}\n" +
		"categories          php\n" +
		"maintainers         nomaintainer\n" +
		"license             MIT\n" +
		"description         a synthetic cohort member\n" +
		"long_description    A synthetic dependent for dockhand's cohort tests.\n"
}

// messageOf reads a commit's whole message, which is what the cohort
// commit's body is asserted against.
func messageOf(t *testing.T, repo *git.Repo, sha string) string {
	t.Helper()
	out, err := exec.Command(testenv.Tool(t, "git"), "-C", repo.Root, "log", "-1", "--format=%B", sha).Output()
	require.NoError(t, err)
	return string(out)
}

// A ports tree with no PortIndex is the one state this feature used to
// pass over in silence.
//
// PortIndex is generated by portindex(1) and is not carried in a
// macports-ports clone, so a fresh checkout is exactly this. Before,
// every bump on one settled with no finding, no proposal and no
// warning — byte-identical to a leaf port that genuinely has nothing
// depending on it. Two states, opposite remedies, one silence: run
// portindex, or nothing to do.
func TestATreeWithNoIndexIsUnavailableByNameAndNotSilent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, macports.PortGroupDir), 0o755))
	repo := gittest.Init(t, realTools, root, map[string]string{
		"sysutils/judy/Portfile": "version 1.0\n",
	})
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	sha := gittest.Commit(t, repo, "dockhand/judy-1.1", primary,
		"sysutils/judy/Portfile", "version 1.1\n", "judy: update to 1.1")
	n := noteOn(t, repo, sha, "judy")

	require.NoError(t, indexed(t, repo, measured(judyMoved())).settle(ctx, repo, &n))

	abi, ok := findingOf(n, "abi-unavailable")
	require.True(t, ok, "a question that could not be asked is not a question with no answer")
	assert.Contains(t, abi.Criterion, "reverse index could not be read")
	assert.Contains(t, abi.Criterion, "run `portindex`", "and the remedy is the command that fixes it")
	assert.Equal(t, record.Accepted, abi.Disposition, "nobody can answer a missing index by hand")
	_, proposed := findingOf(n, "dependent-revbump")
	assert.False(t, proposed, "nothing was measured, so nothing is put forward")
}

// A directory that is not a ports tree says nothing, and that is the
// other half of the same rule.
//
// Nobody was ever in a position to look: dockhand is running somewhere
// else, resolution says so loudly on its own, and a finding here would
// appear under every branch of every run outside a tree.
func TestSomewhereThatIsNotAPortsTreeSaysNothing(t *testing.T) {
	ctx := context.Background()
	repo, sha := engineRepo(t)
	n := noteOn(t, repo, sha, "judy")
	e := testState(t, repo, measured(judyMoved()))
	e.Verifier = func(context.Context) (verify.Verifier, error) { return measured(judyMoved()), nil }
	e.Lister = e.Verifier
	e.Tree = func() (*tree.Tree, error) { return tree.Open(repo.Root) }

	require.NoError(t, e.settle(ctx, repo, &n))

	assert.Empty(t, n.Findings, "there is no tree here to have an index")
}

// The cohort's own verification settles against the same tip, so
// findCohort runs a second time over the same dependents. The proposal
// on that record has an answer already — a commit that is on the branch
// — and asking again would be asking a person twice, on a tip whose
// machine gate would then hold for a question they have answered.
func TestAnAnsweredProposalIsNotProposedAgain(t *testing.T) {
	ctx := context.Background()
	repo, sha := indexedRepo(t, "judy")
	n := noteOn(t, repo, sha, "judy")
	n.Findings = []record.Finding{{
		Kind: "dependent-revbump", Ports: []string{"netdata"},
		Criterion:   "install name /opt/local/lib/libJudy.1.dylib → /opt/local/lib/libJudy.2.dylib",
		Candidates:  []record.Candidate{{Port: "netdata", Portdir: "sysutils/netdata", Proposed: true}},
		Disposition: record.Accepted,
	}}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))

	require.NoError(t, indexed(t, repo, measured(judyMoved())).settle(ctx, repo, &n))

	_, ok := findingOf(n, "abi-change")
	assert.True(t, ok, "the measurement is a fact about the change and is still recorded")
	assert.False(t, AnyProposed(n), "and no second question lands on a tip that answered the first")
	assert.Len(t, n.Findings, 2, "one measurement and the answered proposal, and nothing new")
}

// The head of the chain: a comment in a Portfile becomes a finding on
// the minted note.
//
// Every other test in this file starts from a hand-written record, and
// that is the joint they cannot see. The plan is where an instruction
// comment is read — with the Portfile's bytes in hand, which is the
// only moment they are the bytes the change was planned against — and
// the mint is where a plan's findings become a note's. A real planner
// runs here for exactly that reason: a plan.Findings the mint dropped
// would leave the machine gate with nothing to hold and the cohort
// decision with no quote to weigh, both of them silently.
func TestAPortfilesInstructionCommentSurvivesIntoTheMintedNote(t *testing.T) {
	testenv.PortTclsh(t)
	ctx := context.Background()
	const comment = "# Please revbump other whenever netdata is updated.\n"
	repo := gittest.Init(t, realTools, testenv.PortIndexTree(t), map[string]string{
		"sysutils/netdata/Portfile": comment + memberPortfile("netdata", "revision            4\n"),
	})
	dir := filepath.Join(repo.Root, "sysutils", "netdata")

	var out, errOut bytes.Buffer
	e := indexed(t, repo, measured(verify.Manifests{}))
	eng := *e
	eng.Out, eng.Err = &out, &errOut
	ev, err := eng.Session(ctx)
	require.NoError(t, err)
	defer ev.Close()

	p, err := bumprevision.BumpRevision{Reason: "rebuild against judy 1.1"}.
		Plan(ctx, port.New(tree.Target{Portdir: dir}, ev), nil)
	require.NoError(t, err)
	require.Len(t, p.Findings, 1, "the planner read the comment while it had the bytes")

	require.NoError(t, runPlan(t, ctx, &eng, p, Policy{Destination: record.ToBranch}))

	tip, err := repo.RevParse(ctx, "dockhand/"+p.Slug)
	require.NoError(t, err)
	n, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)

	f, ok := findingOf(n, "instruction-comment")
	require.True(t, ok, "the mint is where a plan's findings become a note's")
	assert.Equal(t, record.Proposed, f.Disposition,
		"a maintainer's instruction is a question, and the machine gate holds until it is answered")
	assert.Equal(t, "sysutils/netdata/Portfile", f.Source)
	assert.Equal(t, strings.TrimRight(comment, "\n"), f.Quote, "verbatim, byte for byte")
	assert.False(t, f.At.IsZero(), "and stamped with the moment the note learned it")
	assert.True(t, AnyProposed(n))
}
