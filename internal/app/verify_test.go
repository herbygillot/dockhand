package app

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
)

// sink is what an operation said, kept. A recorder rather than a writer
// because what these tests assert is that a SENTENCE was said at all —
// the advisory a stale primary earns is a fact about the roster that
// changes nothing about it, so nothing else observable would show it.
type sink struct{ lines []string }

func (s *sink) Stage(string, string)              {}
func (s *sink) Say(_ progress.Level, text string) { s.lines = append(s.lines, text) }
func (s *sink) Stream(io.Reader)                  {}

// commitOn writes one commit by hand — outside every verb under test —
// and points a branch at it when it is given one. A commit with no
// branch is how an upstream commit that only a fetch has seen is made.
func commitOn(t *testing.T, repo *git.Repo, branch, parent, path, content, message string) string {
	t.Helper()
	ctx := t.Context()
	tree, err := repo.GraftTree(ctx, parent, []git.File{{Path: path, Content: []byte(content)}})
	require.NoError(t, err)
	sha, err := repo.CommitTree(ctx, tree, []string{parent}, message)
	require.NoError(t, err)
	if branch != "" {
		gittest.MoveBranch(t, repo, branch, sha)
	}
	return sha
}

func head(t *testing.T, repo *git.Repo) string {
	t.Helper()
	sha, err := repo.RevParse(t.Context(), "HEAD")
	require.NoError(t, err)
	return sha
}

func primaryOf(t *testing.T, repo *git.Repo) string {
	t.Helper()
	name, err := repo.PrimaryBranch(t.Context())
	require.NoError(t, err)
	return name
}

// THE ADVISORY THAT WAS RULED TO MOVE AND WENT NOWHERE. The derivation
// diffs against the LOCAL primary, which never fetches (D21), so a
// hand-made branch cut from origin/<primary> adopts every portdir the
// commits between the two positions touched — and the guest builds ports
// the branch never changed. Ruled 2026-09-04: the roster stands and the
// road that derived it says which members are somebody else's.
func TestBranchSubjectsSaysTheMembersAStalePrimaryAdded(t *testing.T) {
	ctx := t.Context()
	repo, st := fixture(t)
	name := primaryOf(t, repo)
	upstream := commitOn(t, repo, "", head(t, repo), "devel/oniguruma/Portfile", "version 6.9\n", "oniguruma: update to 6.9")
	gittest.Fetched(t, repo, "origin", name, upstream)
	tip := commitOn(t, repo, "dockhand/jq-1.8", upstream, "sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8")

	said := &sink{}
	v := Verify{Repo: repo, State: st, Me: me(), Now: now, Progress: said}
	subjects, base, err := v.branchSubjects(ctx, "dockhand/jq-1.8", tip)
	require.NoError(t, err)
	assert.Equal(t, []string{"devel/oniguruma", "sysutils/jq"}, portdirsOf(subjects),
		"the roster is what the diff against the local primary says; the advisory changes nothing")
	assert.NotEmpty(t, base.Sha)

	require.Len(t, said.lines, 1, "one line, about the roster, on the road that has a stream")
	assert.Contains(t, said.lines[0], "devel/oniguruma (from "+git.Abbrev(upstream)+" oniguruma: update to 6.9)",
		"the member, and the commit it came from")
	assert.NotContains(t, said.lines[0], "sysutils/jq", "the branch's own member is not accused")
	assert.Contains(t, said.lines[0], "fast-forward "+name, "the remedy is the user's, and the line names it")
}

func TestBranchSubjectsSaysNothingWhenThePrimaryIsCurrent(t *testing.T) {
	ctx := t.Context()
	repo, st := fixture(t)
	tip := commitOn(t, repo, "dockhand/jq-1.8", head(t, repo), "sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8")

	said := &sink{}
	v := Verify{Repo: repo, State: st, Me: me(), Now: now, Progress: said}
	subjects, _, err := v.branchSubjects(ctx, "dockhand/jq-1.8", tip)
	require.NoError(t, err)
	assert.Equal(t, []string{"sysutils/jq"}, portdirsOf(subjects))
	assert.Empty(t, said.lines, "a roster the branch earned is not an enlargement")
}

// THE CROSS-CHECK, REACHED. change.ChangedPortdirs holds git's answer
// against the record's and refuses where they differ, and its only
// production caller passed a record carrying no subjects at all — so the
// disagreement it exists to raise could not be raised. A followed branch
// is the case: the record's roster was written at mint, and the person's
// own commit is what the branch is now.
func TestAuditFollowedRefusesABranchThatLeftItsRecordsRoster(t *testing.T) {
	ctx := t.Context()
	repo, st := fixture(t)
	base := head(t, repo)
	tip := commitOn(t, repo, "dockhand/jq-1.8", base, "sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8")
	c := record.Change{
		ID: "chg-01", Branch: "dockhand/jq-1.8", Tip: tip,
		Subjects: []record.Subject{{Port: "jq", Portdir: "sysutils/jq"}},
	}
	v := Verify{Repo: repo, State: st, Me: me(), Now: now, Progress: &sink{}}
	require.NoError(t, v.auditFollowed(ctx, c, tip), "the record and the branch describe one change")

	// Answering review feedback with an edit to another portdir, committed
	// onto the same branch.
	moved := commitOn(t, repo, "dockhand/jq-1.8", tip, "devel/oniguruma/Portfile", "version 6.9\n", "oniguruma: ride along")
	err := v.auditFollowed(ctx, c, moved)
	require.ErrorIs(t, err, change.ErrPortdirsDisagree)
	var d *change.PortdirDisagreement
	require.ErrorAs(t, err, &d, "both sides are the answer, because only a person can say which is true")
	assert.Equal(t, []string{"devel/oniguruma", "sysutils/jq"}, d.Derived)
	assert.Equal(t, []string{"sysutils/jq"}, d.Recorded)

	// The shape that could not refuse: the audit asked with a record that
	// names no portdir is git's answer standing unopposed, which is what
	// the follow road used to ask and why the cross-check was unreachable.
	_, unopposed := change.ChangedPortdirs(ctx, repo, record.Change{Tip: moved}, base)
	require.NoError(t, unopposed, "a record that claims nothing cannot disagree with anything")
}

func portdirsOf(subjects []record.Subject) []string {
	out := make([]string, 0, len(subjects))
	for _, s := range subjects {
		out = append(out, s.Portdir)
	}
	return out
}

// A DISAGREEMENT A STALE PRIMARY CAUSED SAYS SO. The derived side is the
// diff against the LOCAL primary, so the refusal a followed branch earns
// can name portdirs the person never touched — and the advisory is what
// makes that refusal answerable rather than a hunt through their own
// commits for edits that are not theirs.
func TestAuditFollowedNamesTheStalePrimaryBehindItsRefusal(t *testing.T) {
	ctx := t.Context()
	repo, st := fixture(t)
	name := primaryOf(t, repo)
	upstream := commitOn(t, repo, "", head(t, repo), "devel/oniguruma/Portfile", "version 6.9\n", "oniguruma: update to 6.9")
	gittest.Fetched(t, repo, "origin", name, upstream)
	tip := commitOn(t, repo, "dockhand/jq-1.8", upstream, "sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8")
	c := record.Change{
		ID: "chg-01", Branch: "dockhand/jq-1.8", Tip: tip,
		Subjects: []record.Subject{{Port: "jq", Portdir: "sysutils/jq"}},
	}

	said := &sink{}
	v := Verify{Repo: repo, State: st, Me: me(), Now: now, Progress: said}
	err := v.auditFollowed(ctx, c, tip)
	require.ErrorIs(t, err, change.ErrPortdirsDisagree)
	require.Len(t, said.lines, 1, "the refusal stands; the advisory is what makes it answerable")
	assert.Contains(t, said.lines[0], "devel/oniguruma (from ")
}

// A SUBPORT'S SNAPSHOT CARRIES THE SUBPORT'S OWN NAME. A portdir holding
// subports is indistinguishable from any other directory, so the caller
// that consulted the index is the only thing that can say which port
// inside it was meant — and without that, `dockhand verify
// terraform-1.16` would write a record naming "terraform" and blame the
// parent for a subport's verdict.
//
// The portdir's base name is still the answer when nothing resolved a
// subport, which is every port whose directory is its name.
func TestASnapshotNamesTheSubportAndNotItsDirectory(t *testing.T) {
	repo, _ := fixture(t)
	dir := filepath.Join(repo.Root, "sysutils", "jq")

	named, _, err := snapshotSubject(t.Context(), repo, dir, "jq-devel")
	require.NoError(t, err)
	require.Len(t, named, 1)
	assert.Equal(t, "jq-devel", named[0].Port, "the port the person named")
	assert.Equal(t, "sysutils/jq", named[0].Portdir, "in the directory that holds it")

	plain, _, err := snapshotSubject(t.Context(), repo, dir, "")
	require.NoError(t, err)
	require.Len(t, plain, 1)
	assert.Equal(t, "jq", plain[0].Port, "and the directory's base name when nothing said otherwise")
}
