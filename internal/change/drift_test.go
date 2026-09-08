package change

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

func TestBehindMeasuresTheBaseAndWhetherThePortdirMovedUnderIt(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	c := minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	primaryName, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)

	d, err := Behind(ctx, repo, c, primaryName)
	require.NoError(t, err)
	assert.True(t, d.Compared)
	assert.Zero(t, d.BehindBy)
	assert.False(t, d.OverTree)
	assert.Equal(t, c.Base, d.Base)

	// Two commits land on the primary branch: one somewhere else, one on
	// this change's own portdir.
	base := primary(t, repo)
	elsewhere := byHandAt(t, repo, primaryName, base, "textproc/oniguruma6/Portfile", "version 6.9\n", "another port")
	d, err = Behind(ctx, repo, c, primaryName)
	require.NoError(t, err)
	assert.Equal(t, 1, d.BehindBy)
	assert.False(t, d.OverTree, "behind is not the same fact as moved underneath")

	byHandAt(t, repo, primaryName, elsewhere, "sysutils/jq/Portfile", "version 1.7.1\n", "the same port")
	d, err = Behind(ctx, repo, c, primaryName)
	require.NoError(t, err)
	assert.Equal(t, 2, d.BehindBy)
	assert.True(t, d.OverTree)
}

func TestBehindSaysItCouldNotCompareRatherThanSayingZero(t *testing.T) {
	repo, _ := newRepo(t)
	primaryName, err := repo.PrimaryBranch(context.Background())
	require.NoError(t, err)

	d, err := Behind(context.Background(), repo, record.Change{ID: "chg-01"}, primaryName)
	require.NoError(t, err)
	assert.False(t, d.Compared, "rule 7: without this, zero means both up to date and the comparison could not run")
	require.ErrorIs(t, d.Err, ErrNoBase)
	assert.Zero(t, d.BehindBy)

	_, err = Behind(context.Background(), repo, record.Change{Base: record.Base{Sha: "abc"}}, "")
	assert.ErrorIs(t, err, ErrIncomplete)
}

func TestChangedPortdirsDerivesFromGitAndHoldsTheRecordAgainstIt(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	base := primary(t, repo)
	c := minted(t, repo, st, "chg-01", "dockhand/jq-1.8")

	got, err := ChangedPortdirs(ctx, repo, c, base)
	require.NoError(t, err)
	assert.Equal(t, []string{"sysutils/jq"}, got)

	// A record that names no portdir at all is not a disagreement: nobody
	// said, so git's answer stands unopposed.
	silent := c
	silent.Subjects = []record.Subject{{Port: "jq"}}
	got, err = ChangedPortdirs(ctx, repo, silent, base)
	require.NoError(t, err)
	assert.Equal(t, []string{"sysutils/jq"}, got)
}

func TestChangedPortdirsRefusesWhenTheTwoSourcesDisagree(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	base := primary(t, repo)
	c := minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	c.Subjects = []record.Subject{{Port: "mise", Portdir: "sysutils/mise"}}

	_, err := ChangedPortdirs(ctx, repo, c, base)
	require.ErrorIs(t, err, ErrPortdirsDisagree)
	var d *PortdirDisagreement
	require.ErrorAs(t, err, &d)
	assert.Equal(t, []string{"sysutils/jq"}, d.Derived)
	assert.Equal(t, []string{"sysutils/mise"}, d.Recorded,
		"both readings are wrong when they differ, so both are handed back")
}

func TestChangedPortdirsKeepsTheRecordsOrderWhereTheyAgree(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	base := primary(t, repo)
	c := minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	// A cohort commit touching a second portdir, recorded headline first
	// — which is not the alphabetical order git derives.
	tip := byHandAt(t, repo, "dockhand/jq-1.8", c.Tip, "textproc/oniguruma6/Portfile", "version 6.9.9\n", "revbump")
	c.Tip = tip
	c.Subjects = []record.Subject{
		{Port: "jq", Portdir: "sysutils/jq"},
		{Port: "oniguruma6", Portdir: "textproc/oniguruma6"},
	}
	got, err := ChangedPortdirs(ctx, repo, c, base)
	require.NoError(t, err)
	assert.Equal(t, []string{"sysutils/jq", "textproc/oniguruma6"}, got,
		"the record knows which subject is the headline and the order the members must be built in")
}

func TestChangedPortdirsSaysAResourcesOnlyBranchApart(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	base := primary(t, repo)
	c := minted(t, repo, st, "chg-01", "dockhand/jq-1.8")

	c.Tip = byHandAt(t, repo, "", base, "_resources/port1.0/group/golang-1.0.tcl", "# a port group\n", "resources")
	c.Subjects = nil
	_, err := ChangedPortdirs(ctx, repo, c, base)
	require.ErrorIs(t, err, ErrResourcesOnly,
		"tree resources are not a malformed port change; they are not a port change")

	c.Tip = base
	_, err = ChangedPortdirs(ctx, repo, c, base)
	assert.ErrorIs(t, err, ErrNoPortdir)
}

// byHandAt is byHand for a path other than jq's Portfile: a commit
// written outside every verb this package uses, optionally landing on a
// branch.
func byHandAt(t *testing.T, repo *git.Repo, branch, parent, path, content, message string) string {
	t.Helper()
	ctx := context.Background()
	tree, err := repo.GraftTree(ctx, parent, []git.File{{Path: path, Content: []byte(content)}})
	require.NoError(t, err)
	sha, err := repo.CommitTree(ctx, tree, []string{parent}, message)
	require.NoError(t, err)
	if branch != "" {
		plant(t, repo, "update-ref", "refs/heads/"+branch, sha)
	}
	return sha
}
