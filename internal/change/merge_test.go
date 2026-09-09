package change

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
)

func unit(port, portdir, sha string, paths ...string) Prepared {
	p := Prepared{
		Portdir:  TreePath(portdir),
		Subjects: []record.Subject{{Port: port, Portdir: portdir, Intent: "bump-revision"}},
		Base:     record.Base{Sha: sha},
		Intent:   "bump-revision",
		Summary:  port + ": rebuild",
	}
	for _, rel := range paths {
		p.Files = append(p.Files, File{Path: portdir + "/" + rel, Content: []byte("x\n")})
	}
	return p
}

// A COHORT IS A PORT'S CHANGE AND ITS DEPENDENTS' CHANGES, ASSEMBLED.
// Prepare is singular and correct — one plan, one subject, one portdir,
// one file set, which is exactly a bump — so what a cohort needs is not
// a wider Prepare but an assembly. Every member then travels the same
// road a solo revbump does.
//
// The cohort road did not assemble: it planned candidates[0] and
// stopped, so a six-member cohort bumped one port.
func TestMergeAssemblesEveryMembersFilesAndSubjects(t *testing.T) {
	got, err := Merge(
		unit("Aseprite", "graphics/Aseprite", "base1", "Portfile"),
		unit("nheko", "net/nheko", "base1", "Portfile"),
		unit("mkvtoolnix", "multimedia/mkvtoolnix", "base1", "Portfile"),
	)
	require.NoError(t, err)

	assert.Len(t, got.Subjects, 3, "every member is a subject of the one change")
	assert.Equal(t, []string{
		"graphics/Aseprite/Portfile",
		"net/nheko/Portfile",
		"multimedia/mkvtoolnix/Portfile",
	}, paths(got), "three portdirs in one file set — the thing a shared prefix could not express")
}

// IDENTITY IS THE HEADLINE'S AND CONTENT IS EVERY MEMBER'S. A change is
// one commit with one message about one thing; only what it WRITES is
// plural.
func TestMergeTakesIdentityFromTheHeadline(t *testing.T) {
	head := unit("Aseprite", "graphics/Aseprite", "base1", "Portfile")
	head.Summary = "rebuild against cmark 0.31.2"
	head.Closes = "12345"

	got, err := Merge(head, unit("nheko", "net/nheko", "base1", "Portfile"))
	require.NoError(t, err)
	assert.Equal(t, TreePath("graphics/Aseprite"), got.Portdir)
	assert.Equal(t, "rebuild against cmark 0.31.2", got.Summary)
	assert.Equal(t, "12345", got.Closes)
}

// A MEMBER PREPARED AGAINST A DIFFERENT BASE IS A MEMBER PLANNED AGAINST
// A DIFFERENT TREE, and the commit would carry bytes nobody predicted.
func TestMergeRefusesMembersFromDifferentBases(t *testing.T) {
	_, err := Merge(
		unit("Aseprite", "graphics/Aseprite", "base1", "Portfile"),
		unit("nheko", "net/nheko", "base2", "Portfile"),
	)
	require.ErrorIs(t, err, ErrIncomplete)
	assert.Contains(t, err.Error(), "nheko")
	assert.Contains(t, err.Error(), "Aseprite")
}

// AND TWO MEMBERS WRITING ONE PATH ARE NAMED. GraftTree would refuse it
// later with "named twice in one tree", which says nothing about which
// members disagreed.
func TestMergeRefusesTwoMembersWritingOnePath(t *testing.T) {
	a := unit("jq", "sysutils/jq", "base1", "Portfile")
	b := unit("jq-devel", "sysutils/jq", "base1", "Portfile")

	_, err := Merge(a, b)
	require.ErrorIs(t, err, ErrIncomplete)
	assert.Contains(t, err.Error(), "sysutils/jq/Portfile")
	assert.Contains(t, err.Error(), "jq-devel")
}

// ONE UNIT MERGES TO ITSELF, which is what keeps a single-member cohort
// on the same road as a six-member one.
func TestMergeOfOneIsThatOne(t *testing.T) {
	one := unit("Aseprite", "graphics/Aseprite", "base1", "Portfile")
	got, err := Merge(one)
	require.NoError(t, err)
	assert.Equal(t, one.Subjects, got.Subjects)
	assert.Equal(t, paths(one), paths(got))

	_, err = Merge()
	assert.ErrorIs(t, err, ErrIncomplete)
}

func paths(p Prepared) []string {
	out := make([]string, 0, len(p.Files))
	for _, f := range p.Files {
		out = append(out, f.Path)
	}
	return out
}
