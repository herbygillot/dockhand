package engine

// `cycle --superseded`: the one thing in this tree that removes a
// branch for having been superseded.
//
// A supersede is recorded at mint and does nothing else. The newer
// branch is the change now; the older one keeps everything it learned
// and gains the field that says why it will learn nothing more. That is
// the whole of it, deliberately — and the deliberateness is the ruling.
// A supersede is dockhand's own inference from two branch names about
// one port, made without asking anybody, and inference is not grounds to
// delete work: the older branch may carry a verdict the newer one has
// not earned yet, a fork copy backing an open pull request, or simply
// the diff somebody wanted to look at again.
//
// So removal is a separate flag, and the flag is the person saying they
// meant it. Nothing else touches a superseded branch: not `cycle`'s
// merged-PR retirement, which retires on the forge's word rather than
// on ours; not `status`, which reports; and not the machine's publish
// slot, which steps over a superseded branch entirely rather than
// acting on one.
//
// It asks the notes and never the forge. Being superseded is a local
// fact about two branches in one namespace, so this sweep costs no gh
// call and works with no network — which is also what keeps it out of
// the reconciler, whose every phase is about what GitHub said.

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
)

// CleanSuperseded removes every branch a newer sibling replaced,
// reporting what it removed and, for everything kept, why.
//
// The fork copy is left alone — the same choice `discard` makes, and for
// the same reason. A superseded branch may have been promoted, its copy
// may back a pull request somebody is reading, and deleting the copy
// closes that pull request. Retiring the local branch is this verb's
// business; ring 3 is not, and a sweep that quietly closed a review
// would be spending it.
//
// A held branch is kept and says so. A hold is a person saying stop, and
// "stop" that a sweep walked past on the grounds that the branch was
// superseded anyway would be the hold meaning nothing at exactly the
// moment somebody placed it to prevent this.
func (e *Engine) CleanSuperseded(ctx context.Context, repo *git.Repo) ([]render.Line, error) {
	branches, err := repo.Branches(ctx, git.BranchNamespace)
	if err != nil {
		return nil, err
	}
	var said []render.Line
	// One row per branch this sweep had something to say about, in the
	// report's own column so `cycle` and `cycle --superseded` line up on
	// a terminal. The newline BranchLine carries is the printer's, and a
	// Line's text never holds one.
	row := func(branch, standing string) {
		said = append(said, render.Line{Stream: render.ToOut,
			Text: strings.TrimSuffix(fmt.Sprintf(render.BranchLine, branch, standing), "\n")})
	}
	removed := 0
	for _, br := range branches {
		n, ok := e.supersededNote(ctx, repo, br)
		if !ok {
			continue
		}
		if err := GateHold(n, br, "the removal"); err != nil {
			row(br, "kept — "+err.Error())
			continue
		}
		// dropFork false: see above. The demolition's own account comes
		// back as lines and is kept with the branch it is about, which is
		// why they are appended before this branch's own row rather than
		// pooled at the end.
		lines, derr := e.Discard(ctx, repo, br, false)
		said = append(said, lines...)
		if derr != nil {
			row(br, fmt.Sprintf("kept — removing it failed: %v", derr))
			continue
		}
		removed++
		row(br, "removed — superseded by "+n.SupersededBy)
	}
	if removed == 0 {
		said = append(said, render.Line{Stream: render.ToOut,
			Text: "no superseded branches removed in " + repo.Root})
	}
	return said, nil
}

// supersededNote answers whether this branch is one a newer sibling
// replaced, with the record that says so.
//
// Every failure is a no. A branch whose tip will not resolve was deleted
// under this pass, and a branch with no readable note has never been
// marked superseded by anything — neither is a branch to delete, and
// neither is worth a sentence in a sweep that is about the ones that
// are.
func (e *Engine) supersededNote(ctx context.Context, repo *git.Repo, branch string) (record.Record, bool) {
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return record.Record{}, false
	}
	n, err := e.Ledger(repo).Read(ctx, tip)
	if err != nil || n.SupersededBy == "" {
		return record.Record{}, false
	}
	return n, true
}
