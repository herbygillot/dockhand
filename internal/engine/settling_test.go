package engine

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/require"
)

// The edit: a Portfile changed, a patch added, a script made executable,
// and a patch deleted.
var settlingEdit = model.Edit{Port: "foo", Files: []model.EditedFile{
	{Path: "Portfile", Before: "p1", After: "p2", BeforeMode: "100644", AfterMode: "100644"},
	{Path: "files/new.diff", After: "n2", AfterMode: "100644"},
	{Path: "files/run.sh", Before: "r1", After: "r1", BeforeMode: "100644", AfterMode: "100755"},
	{Path: "files/old.diff", Before: "o1", BeforeMode: "100644"},
}}

func settlingFiles(portfile, added, script, deleted WorkingFile) map[string]WorkingFile {
	return map[string]WorkingFile{"Portfile": portfile, "files/new.diff": added, "files/run.sh": script, "files/old.diff": deleted}
}

var (
	portfileBefore = WorkingFile{Present: true, Blob: "p1", Mode: "100644"}
	portfileAfter  = WorkingFile{Present: true, Blob: "p2", Mode: "100644"}
	addedAfter     = WorkingFile{Present: true, Blob: "n2", Mode: "100644"}
	scriptBefore   = WorkingFile{Present: true, Blob: "r1", Mode: "100644"}
	scriptAfter    = WorkingFile{Present: true, Blob: "r1", Mode: "100755"}
	deletedBefore  = WorkingFile{Present: true, Blob: "o1", Mode: "100644"}
	absent         = WorkingFile{}
)

// Each state of an unwritten edit's files, with modes, a deletion and an
// untracked file in the way, against what each settler does (#194, T1):
// all after records, all before writes or goes on, a cut write is
// finished or stops naming update, and any file neither stops with it
// named, or for tidy with a person is theirs.
func TestSettling(t *testing.T) {
	t.Parallel()
	allAfter := settlingFiles(portfileAfter, addedAfter, scriptAfter, absent)
	allBefore := settlingFiles(portfileBefore, absent, scriptBefore, deletedBefore)
	cut := settlingFiles(portfileAfter, addedAfter, scriptBefore, deletedBefore)
	everyFile := []string{"Portfile", "files/new.diff", "files/run.sh", "files/old.diff"}
	rest := []string{"files/run.sh", "files/old.diff"}
	for _, test := range []struct {
		name     string
		worktree map[string]WorkingFile
		want     map[Settler]Settling
	}{
		{"all after: the record was lost", allAfter, map[Settler]Settling{
			SettlerOwner: {Action: SettleRecord}, SettlerOther: {Action: SettleRecord}, SettlerTidyAttended: {Action: SettleRecord}}},
		{"all before: never written", allBefore, map[Settler]Settling{
			SettlerOwner: {Action: SettleWrite, Files: everyFile}, SettlerOther: {Action: SettleGoOn}, SettlerTidyAttended: {Action: SettleGoOn}}},
		{"some after, the rest before", cut, map[Settler]Settling{
			SettlerOwner: {Action: SettleWrite, Files: rest}, SettlerOther: {Action: SettleUnfinished, Files: rest}, SettlerTidyAttended: {Action: SettleUnfinished, Files: rest}}},
		{"a mode alone not as either side", settlingFiles(portfileAfter, addedAfter, WorkingFile{Present: true, Blob: "r1", Mode: "120000"}, absent), map[Settler]Settling{
			SettlerOwner: {Action: SettleChanged, Files: []string{"files/run.sh"}}, SettlerOther: {Action: SettleChanged, Files: []string{"files/run.sh"}},
			SettlerTidyAttended: {Action: SettleTheirs, Files: []string{"files/run.sh"}}}},
		{"an untracked file in the way of the added one (Z)", settlingFiles(portfileBefore, WorkingFile{Present: true, Blob: "x9", Mode: "100644"}, scriptBefore, deletedBefore), map[Settler]Settling{
			SettlerOwner: {Action: SettleChanged, Files: []string{"files/new.diff"}}, SettlerOther: {Action: SettleChanged, Files: []string{"files/new.diff"}},
			SettlerTidyAttended: {Action: SettleTheirs, Files: []string{"files/new.diff"}}}},
		{"the deleted file changed instead", settlingFiles(portfileAfter, addedAfter, scriptAfter, WorkingFile{Present: true, Blob: "o2", Mode: "100644"}), map[Settler]Settling{
			SettlerOwner: {Action: SettleChanged, Files: []string{"files/old.diff"}}, SettlerOther: {Action: SettleChanged, Files: []string{"files/old.diff"}},
			SettlerTidyAttended: {Action: SettleTheirs, Files: []string{"files/old.diff"}}}},
	} {
		for settler, want := range test.want {
			require.Equal(t, want, settling(settlingEdit, test.worktree, nil, settler), "%s, settler %d", test.name, settler)
		}
	}

	// An edit from before modes were kept compares blobs alone.
	old := model.Edit{Port: "foo", Files: []model.EditedFile{{Path: "Portfile", Before: "p1", After: "p2"}}}
	require.Equal(t, Settling{Action: SettleRecord}, settling(old, map[string]WorkingFile{"Portfile": {Present: true, Blob: "p2", Mode: "100755"}}, nil, SettlerOwner))
}

// Another file of the port, changed by a person, isn't read: the edit
// settles whole as its own files say (#194, T4, B ii).
func TestAnotherFileOfThePortLeavesTheEditToSettle(t *testing.T) {
	t.Parallel()
	worktree := settlingFiles(portfileAfter, addedAfter, scriptAfter, absent)
	worktree["files/other.diff"] = WorkingFile{Present: true, Blob: "z1", Mode: "100644"}
	require.Equal(t, Settling{Action: SettleRecord}, settling(settlingEdit, worktree, nil, SettlerOwner))
}

// Where the kept blobs were collected, a missing object is its own
// answer: never written, the edit is prepared again; all written, it
// settles; partly written, nothing is written and the files are named
// (#194, T6, AC).
func TestAnUnwrittenEditWhoseBlobsWereCollected(t *testing.T) {
	t.Parallel()
	missing := map[model.ObjectID]bool{"p2": true, "n2": true}
	require.Equal(t, Settling{Action: SettlePrepareAgain, Files: []string{"Portfile", "files/new.diff"}},
		settling(settlingEdit, settlingFiles(portfileBefore, absent, scriptBefore, deletedBefore), missing, SettlerOwner))
	require.Equal(t, Settling{Action: SettleGoOn},
		settling(settlingEdit, settlingFiles(portfileBefore, absent, scriptBefore, deletedBefore), missing, SettlerOther), "another command writes nothing and needs no blob")
	require.Equal(t, Settling{Action: SettleRecord},
		settling(settlingEdit, settlingFiles(portfileAfter, addedAfter, scriptAfter, absent), missing, SettlerOwner))
	require.Equal(t, Settling{Action: SettleBlobsGone, Files: []string{"Portfile"}},
		settling(settlingEdit, settlingFiles(portfileBefore, addedAfter, scriptAfter, absent), missing, SettlerOwner))
	require.Equal(t, Settling{Action: SettleWrite, Files: []string{"files/run.sh", "files/old.diff"}},
		settling(settlingEdit, settlingFiles(portfileAfter, addedAfter, scriptBefore, deletedBefore), missing, SettlerOwner), "a rest whose own blobs are kept, or that is a mode or a deletion, is written")
}
