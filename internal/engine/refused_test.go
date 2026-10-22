package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
)

// What dockhand can't write, it says in its own words, with steps only
// where they work: git's refusal said "baseline {…}: portfile: unsupported
// source edit: calculated checksum algorithm", advised checksums, which
// refused the same way, and said a plan kept a branch it never started
// (the git run's findings 1 to 4). The engine composes it once, from what
// refused it by type (R3-38, where author.go composed it).
func TestAnEditDockhandCantMakeSaysWhyAndWhatWorks(t *testing.T) {
	t.Parallel()
	unlocated := fmt.Errorf("baseline {OS:darwin Version:25 Architecture:arm64}: %w", &Unlocated{Name: "git-htmldocs-2.56.0.tar.xz", Reason: ErrUnsupported.Because("no command written in the Portfile makes it, as an eval'd one isn't")})
	edit := refusal.Next{Verb: refusal.VerbEdit, Args: []string{"git"}}

	err := byHand(unlocated, UpdateRequest{Action: model.EditUpdate, Port: "git"}, "dockhand/git-ab12", false)
	require.EqualError(t, err, `Could not update git automatically: the checksums for git-htmldocs-2.56.0.tar.xz can't be found in the Portfile to edit: no command written in the Portfile makes it, as an eval'd one isn't
Kept: the branch, unchanged.`)
	require.Equal(t, refusal.Unsupported, refusal.KindOf(err))
	require.Equal(t, []refusal.Next{{External: "edit the version, then run dockhand checksums git to print the checksums to copy into the Portfile"}, edit}, refusal.Of(err).Next)

	err = byHand(&ChecksumsToWrite{Err: unlocated, Checksums: []portfile.Checksum{{Name: "git-2.56.0.tar.xz", RMD160: "aaaa", SHA256: "bbbb", Size: 8}}}, UpdateRequest{Action: model.EditChecksums, Port: "git"}, "dockhand/git-ab12", true)
	require.EqualError(t, err, `Could not update the checksums for git automatically: the checksums for git-htmldocs-2.56.0.tar.xz can't be found in the Portfile to edit: no command written in the Portfile makes it, as an eval'd one isn't
Kept: dockhand/git-ab12, with nothing changed.`)
	require.Equal(t, []refusal.Next{{External: `use these checksums in the Portfile:
    checksums           git-2.56.0.tar.xz \
                        rmd160  aaaa \
                        sha256  bbbb \
                        size    8`}, edit}, refusal.Of(err).Next)

	err = byHand(ErrUnsupported.Because("a version it can't find"), UpdateRequest{Action: model.EditUpdate, Port: "git", Plan: true, FromMaster: true}, "", false)
	require.EqualError(t, err, "Could not update git automatically: a version it can't find", "a plan keeps nothing, having changed nothing")
	require.Equal(t, []refusal.Next{{External: "edit the version, run port checksum git, then copy the reported checksums into the Portfile"}, edit}, refusal.Of(err).Next)

	// An edit whose evaluation isn't the change intended is refused as one
	// dockhand can't make, with the way on, where rust's came bare (the
	// rust and cargo run).
	err = byHand(ErrFidelity.Because("rust-src, another port of the same Portfile, fetches its own source (its distfiles differ from rust's), which updating rust doesn't move"), UpdateRequest{Action: model.EditUpdate, Port: "rust", Plan: true}, "", false)
	require.EqualError(t, err, "Could not update rust automatically: rust-src, another port of the same Portfile, fetches its own source (its distfiles differ from rust's), which updating rust doesn't move")
	require.Len(t, refusal.Of(err).Next, 2)

	// Started, with no edit kept or one kept, an update that failed says
	// which; one that started nothing is as it came.
	failed := fmt.Errorf("the obsolete stub's update failed")
	require.EqualError(t, refusedUpdate(failed, UpdateRequest{Started: true, Branch: model.Branch{Name: "jq-1.8.2"}}, Update{}), "the obsolete stub's update failed\nKept: jq-1.8.2, with nothing changed.")
	require.EqualError(t, refusedUpdate(failed, UpdateRequest{Started: true, Branch: model.Branch{Name: "jq-1.8.2"}}, Update{Files: []string{"textproc/jq/Portfile"}}), "the obsolete stub's update failed\nKept: jq-1.8.2, with uncommitted changes to textproc/jq/Portfile.")
	require.Equal(t, failed, refusedUpdate(failed, UpdateRequest{}, Update{}))
}
