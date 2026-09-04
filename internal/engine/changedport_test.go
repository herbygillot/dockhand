package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// The devel/pcre shape in miniature: a portdir named for its main
// port, carrying a subport, with the branch changing only the subport.
const subportPortfile = `PortSystem          1.0
name                demo
version             1.0
categories          sysutils
platforms           darwin
maintainers         nomaintainer
description         d
long_description    d
homepage            https://example.org
distfiles

subport demo2 {
    version         2.0
}
`

func subportRepo(t *testing.T) (*git.Repo, string) {
	t.Helper()
	ctx := context.Background()
	repo := gittest.Init(t, realTools, "", map[string]string{"sysutils/demo/Portfile": subportPortfile})
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	// The branch moves ONLY the subport's version — the change is
	// about demo2, whatever the portdir is called.
	edited := strings.Replace(subportPortfile, "    version         2.0", "    version         2.1", 1)
	sha := gittest.Commit(t, repo, "dockhand/demo2-2.1", primary, "sysutils/demo/Portfile",
		edited, "demo2: update to 2.1")
	return repo, sha
}

func TestChangedPortNamesTheSubportTheBranchMoves(t *testing.T) {
	testenv.PortTclsh(t)
	repo, sha := subportRepo(t)
	name, err := testState(t, repo, nil).changedPort(context.Background(), repo, sha, "sysutils/demo")
	require.NoError(t, err)
	assert.Equal(t, "demo2", name, "the changed context, never the portdir's base name")
}

func TestChangedPortFallsBackWhenNothingEvaluatedMoves(t *testing.T) {
	testenv.PortTclsh(t)
	repo, _ := subportRepo(t)
	primary, err := repo.PrimaryBranch(context.Background())
	require.NoError(t, err)
	// A change evaluation cannot see — a comment stands in for the
	// files-only patch, which moves no evaluated context either.
	sha, err := repo.Mint(context.Background(), git.MintRequest{
		Branch: "dockhand/demo-comment", Base: primary, Commits: []git.Commit{{
			Files: []git.File{{
				Path:    "sysutils/demo/Portfile",
				Content: append([]byte(subportPortfile), []byte("# touched\n")...),
			}},
			Message: "demo: comment only",
		}},
	})
	require.NoError(t, err)
	name, err := testState(t, repo, nil).changedPort(context.Background(), repo, sha, "sysutils/demo")
	require.NoError(t, err)
	assert.Equal(t, "demo", name, "an unevaluatable distinction falls back to the portdir's name")
}

// ---- several portdirs ------------------------------------------------
//
// "One at a time for now" is gone. What replaces it is the plural
// derivation and the cross-check that keeps it honest: git says what
// the branch touches, the record says what the change claims, and a
// disagreement is refused rather than staged half.

// cohortBranch is a hand-made branch touching two portdirs at once —
// exactly the shape the retired refusal used to send away.
func cohortBranch(t *testing.T) (*git.Repo, string) {
	t.Helper()
	ctx := context.Background()
	repo := gittest.Init(t, realTools, "", map[string]string{
		"sysutils/jq/Portfile":        "version 1.7\n",
		"textproc/oniguruma/Portfile": "version 6.9\n",
	})
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	sha, err := repo.Mint(ctx, git.MintRequest{
		Branch: "dockhand/jq-1.8", Base: primary, Commits: []git.Commit{{
			Files: []git.File{
				{Path: "sysutils/jq/Portfile", Content: []byte("version 1.8\n")},
				{Path: "textproc/oniguruma/Portfile", Content: []byte("version 6.9\nrevision 1\n")},
			},
			Message: "jq: update to 1.8, with oniguruma rebuilt",
		}},
	})
	require.NoError(t, err)
	return repo, sha
}

// noteWith writes a record naming the given subjects, the way a mint
// would have left one.
func noteWith(t *testing.T, repo *git.Repo, sha string, subjects ...record.Subject) {
	t.Helper()
	ctx := context.Background()
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha)
	require.NoError(t, err)
	n.Subjects = subjects
	require.NoError(t, ledger.Open(repo).Write(ctx, n))
}

func TestChangedPortdirsReturnsEveryPortdirABranchTouches(t *testing.T) {
	repo, sha := cohortBranch(t)
	got, err := testState(t, repo, nil).ChangedPortdirs(context.Background(), repo, "dockhand/jq-1.8", sha)
	require.NoError(t, err)
	assert.Equal(t, []string{"sysutils/jq", "textproc/oniguruma"}, got,
		"sorted, because this order becomes a cohort's build order and its headline")
}

// The record knows something the diff does not: which member is the
// headline. So where the two agree, the record's order is the answer.
func TestChangedPortdirsTakesTheRecordsOrderWhereTheyAgree(t *testing.T) {
	repo, sha := cohortBranch(t)
	noteWith(t, repo, sha,
		record.Subject{Port: "oniguruma", Portdir: "textproc/oniguruma"},
		record.Subject{Port: "jq", Portdir: "sysutils/jq"})
	got, err := testState(t, repo, nil).ChangedPortdirs(context.Background(), repo, "dockhand/jq-1.8", sha)
	require.NoError(t, err)
	assert.Equal(t, []string{"textproc/oniguruma", "sysutils/jq"}, got)
}

// A record that claims less than the branch touches would under-stage:
// the build would come back green about a directory nobody put in the
// environment. Neither reading is taken.
func TestChangedPortdirsRefusesWhenTheRecordAndTheDiffDisagree(t *testing.T) {
	repo, sha := cohortBranch(t)
	noteWith(t, repo, sha, record.Subject{Port: "jq", Portdir: "sysutils/jq"})
	_, err := testState(t, repo, nil).ChangedPortdirs(context.Background(), repo, "dockhand/jq-1.8", sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the two disagree")
	assert.Contains(t, err.Error(), "textproc/oniguruma", "the refusal names what git found")
	assert.Contains(t, err.Error(), "but its record names sysutils/jq")
}

// A subject the ledger adopted from a run key carries a port and no
// portdir. That is nobody saying anything, not a disagreement.
func TestChangedPortdirsAcceptsARecordThatNamesNoPortdir(t *testing.T) {
	repo, sha := cohortBranch(t)
	noteWith(t, repo, sha, record.Subject{Port: "jq"}, record.Subject{Port: "oniguruma"})
	got, err := testState(t, repo, nil).ChangedPortdirs(context.Background(), repo, "dockhand/jq-1.8", sha)
	require.NoError(t, err)
	assert.Equal(t, []string{"sysutils/jq", "textproc/oniguruma"}, got)
}

// A branch that moves no portdir has nothing to build, and says so in
// its own words rather than through a count.
func TestChangedPortdirsRefusesABranchThatTouchesNoPortdir(t *testing.T) {
	ctx := context.Background()
	repo := gittest.Init(t, realTools, "", map[string]string{
		"README.md":            "hi\n",
		"sysutils/jq/Portfile": "version 1.7\n",
	})
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	sha := gittest.Commit(t, repo, "dockhand/readme", primary, "README.md", "hello\n", "readme: a word")
	_, err = testState(t, repo, nil).ChangedPortdirs(ctx, repo, "dockhand/readme", sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changes no portdir")
}

// Each directory is asked about itself. At several portdirs the user's
// target names one port and the note's headline answers for one
// directory, so neither can speak for the rest.
func TestSubjectsOfNamesEachPortdirFromTheRecord(t *testing.T) {
	repo, sha := cohortBranch(t)
	noteWith(t, repo, sha,
		record.Subject{Port: "jq", Portdir: "sysutils/jq"},
		record.Subject{Port: "oniguruma", Portdir: "textproc/oniguruma"})
	eng := testState(t, repo, nil)
	rels, err := eng.ChangedPortdirs(context.Background(), repo, "dockhand/jq-1.8", sha)
	require.NoError(t, err)
	got, err := eng.SubjectsOf(context.Background(), repo, "dockhand/jq-1.8", "dockhand/jq-1.8", sha, rels)
	require.NoError(t, err)
	assert.Equal(t, []Member{
		{Port: "jq", Portdir: "sysutils/jq"},
		{Port: "oniguruma", Portdir: "textproc/oniguruma"},
	}, got)
}

// One portdir still goes through SubjectOf, whose resolution order is
// what every single-subject verification has: the port the user named
// wins outright.
func TestSubjectsOfAtOnePortdirIsSubjectOf(t *testing.T) {
	repo, sha := cohortBranch(t)
	got, err := testState(t, repo, nil).SubjectsOf(context.Background(), repo,
		"jq2", "dockhand/jq-1.8", sha, []string{"sysutils/jq"})
	require.NoError(t, err)
	assert.Equal(t, []Member{{Port: "jq2", Portdir: "sysutils/jq"}}, got)
}

// The whole point of the plural, driven: a hand-made branch touching
// two portdirs is submitted as one change — one guest, both directories
// staged into it, both ports built in it, and one job in the note with
// a run per member.
func TestACohortBranchSubmitsOneGuestForBothMembers(t *testing.T) {
	ctx := context.Background()
	repo, sha := cohortBranch(t)
	noteWith(t, repo, sha,
		record.Subject{Port: "jq", Portdir: "sysutils/jq"},
		record.Subject{Port: "oniguruma", Portdir: "textproc/oniguruma"})
	fake := &verifytest.Fake{}
	eng := testState(t, repo, fake)

	rels, err := eng.ChangedPortdirs(ctx, repo, "dockhand/jq-1.8", sha)
	require.NoError(t, err)
	members, err := eng.SubjectsOf(ctx, repo, "dockhand/jq-1.8", "dockhand/jq-1.8", sha, rels)
	require.NoError(t, err)
	started, err := eng.SubmitRelease(ctx, repo, "dockhand/jq-1.8", sha, members,
		fake.Capabilities().Platforms[0], false)
	require.NoError(t, err)
	assert.True(t, started)

	require.Len(t, fake.Submitted, 1, "one cohort is one environment")
	req := fake.Submitted[0]
	assert.Equal(t, []string{"jq", "oniguruma"}, req.Ports, "in build order")
	require.Len(t, req.Portdirs, 2)
	assert.True(t, strings.HasSuffix(req.Portdirs[0], "sysutils/jq"), req.Portdirs[0])
	assert.True(t, strings.HasSuffix(req.Portdirs[1], "textproc/oniguruma"), req.Portdirs[1])

	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Len(t, n.Jobs, 1, "one job for the platform, whatever the number of subjects")
	assert.Equal(t, record.Running, runFor(n, "jq", "Testos").State)
	assert.Equal(t, record.Running, runFor(n, "oniguruma", "Testos").State)
}

// The contrast, pinned: one subject sends the request it has always
// sent. One port, one staged directory, one run — the plural is a loop
// whose length is one, and nothing about the guest's ask moves because
// the loop exists.
func TestOneSubjectSubmitsExactlyTodaysRequest(t *testing.T) {
	ctx := context.Background()
	repo, sha := cohortBranch(t)
	fake := &verifytest.Fake{}
	eng := testState(t, repo, fake)

	started, err := eng.SubmitRelease(ctx, repo, "dockhand/jq-1.8", sha,
		[]Member{{Port: "jq", Portdir: "sysutils/jq"}}, fake.Capabilities().Platforms[0], false)
	require.NoError(t, err)
	assert.True(t, started)

	require.Len(t, fake.Submitted, 1)
	req := fake.Submitted[0]
	assert.Equal(t, []string{"jq"}, req.Ports)
	require.Len(t, req.Portdirs, 1)
	assert.True(t, strings.HasSuffix(req.Portdirs[0], "sysutils/jq"), req.Portdirs[0])
	assert.Empty(t, req.FromSource)
	assert.False(t, req.NeedsXcode)
	assert.False(t, req.Test)

	n, err := ledger.Open(repo).Read(ctx, sha)
	require.NoError(t, err)
	assert.Len(t, n.Runs, 1, "one subject, one run")
	assert.Equal(t, record.Running, runFor(n, "jq", "Testos").State)
}

// _resources is the tree's own infrastructure — port groups, mirror and
// archive site lists — and its first two segments look exactly like a
// portdir. Taken as one it names a port "port1.0", which stages,
// indexes, and fails in a booted guest against a port that has never
// existed.
func TestChangedPortdirsDoesNotMistakeTreeResourcesForAPort(t *testing.T) {
	ctx := context.Background()
	repo := gittest.Init(t, realTools, "", map[string]string{"sysutils/jq/Portfile": "version 1.7\n"})
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	sha := gittest.Commit(t, repo, "dockhand/pg", primary,
		"_resources/port1.0/fetch/archive_sites.tcl", "# edited\n", "archive sites: a change")

	_, err = testState(t, repo, nil).ChangedPortdirs(ctx, repo, "dockhand/pg", sha)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "port1.0", "no phantom port may be derived from a resource path")
	assert.Contains(t, err.Error(), "_resources/", "the refusal names what the branch actually changed")
	assert.Contains(t, err.Error(), "not a port",
		"and says so: a resource-only branch is not a malformed port change, it is not a port change")
}

// Skipped as a subject, not as content. A branch that edits a port
// group and a port is a change to that port, and the port group still
// reaches the guest because staging materializes _resources from the
// branch's own tip.
func TestChangedPortdirsKeepsThePortWhenAResourceRidesAlong(t *testing.T) {
	ctx := context.Background()
	repo := gittest.Init(t, realTools, "", map[string]string{"sysutils/jq/Portfile": "version 1.7\n"})
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	sha, err := repo.Mint(ctx, git.MintRequest{
		Branch: "dockhand/jq-1.8", Base: primary, Commits: []git.Commit{{
			Files: []git.File{
				{Path: "sysutils/jq/Portfile", Content: []byte("version 1.8\n")},
				{Path: "_resources/port1.0/fetch/archive_sites.tcl", Content: []byte("# edited\n")},
			},
			Message: "jq: update to 1.8, and a tree resource with it",
		}},
	})
	require.NoError(t, err)

	got, err := testState(t, repo, nil).ChangedPortdirs(ctx, repo, "dockhand/jq-1.8", sha)
	require.NoError(t, err)
	assert.Equal(t, []string{"sysutils/jq"}, got,
		"the port is the subject; the resource is carried, not built")
}
