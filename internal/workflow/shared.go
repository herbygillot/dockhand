package workflow

import (
	"context"

	"github.com/herbygillot/dockhand/internal/git/changeset"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
)

// sharedFiles records the files under _resources a revision changes beside
// its port and what loads them, for the revision's record. The lookup is a
// search of the tree, git diff and grep as subprocesses, so a caller runs
// it against the immutable source before its write transaction and hands
// the result in; a failure there is reported and the revision is recorded
// without it, since it is a note and not a precondition.
func (e *Engine) sharedFiles(ctx context.Context, source record.Source) []record.SharedFile {
	shared, err := changeset.SharedUsers(ctx, e.Repo, source)
	if err != nil {
		progress.VerboseReport(ctx, "Shared files changed by %s were not listed: %v", source.Tree, err)
		return nil
	}
	return shared
}
