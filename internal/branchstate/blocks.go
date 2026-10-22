package branchstate

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
)

// BlockKind is what stops a submission, typed, each with its words
// (Block.Words), where submit appended a sentence at each of its sites
// (part 4, step 4).
type BlockKind string

const (
	// BlockUnconfirmed is a Portfile create marked with guesses not yet
	// confirmed.
	BlockUnconfirmed BlockKind = "unconfirmed"
	// BlockMerge is a merge commit, which MacPorts asks to rebase.
	BlockMerge BlockKind = "merge"
	// BlockCheckPending is a check of the commit's files still to finish.
	BlockCheckPending BlockKind = "check-pending"
	// BlockCheckOfOtherFiles is no check of the commit's files, where one
	// of other files passed, whose builds a check of these reuses.
	BlockCheckOfOtherFiles BlockKind = "check-of-other-files"
	// BlockNoCheck is no check of the commit's files at all.
	BlockNoCheck BlockKind = "no-check"
	// BlockUnmet is a changed target an environment can't build, as one
	// that needs Xcode where there's none.
	BlockUnmet BlockKind = "unmet"
	// BlockRemade is a changed target whose result no longer stands, its
	// environment made again since.
	BlockRemade BlockKind = "remade"
	// BlockUnchecked is a changed target no check built everywhere it's
	// required.
	BlockUnchecked BlockKind = "unchecked"
	// BlockFailsAtBase is a changed target that failed as its baseline
	// does, which --accept takes.
	BlockFailsAtBase BlockKind = "fails-at-base"
	// BlockFailed is a changed target that failed.
	BlockFailed BlockKind = "failed"
	// BlockNotAccepted is a failed extra or revision-only target not yet
	// accepted.
	BlockNotAccepted BlockKind = "not-accepted"
	// BlockOthersPushed is someone else's push to the pull request since
	// dockhand's.
	BlockOthersPushed BlockKind = "others-pushed"
	// BlockMaintainerEdits is someone's pull request that doesn't let
	// maintainers push to it.
	BlockMaintainerEdits BlockKind = "maintainer-edits"
	// BlockNoWriteAccess is someone's pull request that pushing to needs
	// write access you don't have.
	BlockNoWriteAccess BlockKind = "no-write-access"
	// BlockNoTitle is a pull request with no title to give it.
	BlockNoTitle BlockKind = "no-title"
)

// Block is one thing that stops a submission: its kind, and what its
// words name.
type Block struct {
	Kind BlockKind
	// Port is the port it's about, and Run the check that found it.
	Port string
	Run  string
	// Unmet is what an environment lacks, for BlockUnmet; Target the
	// target an environment change made build again, for BlockRemade;
	// Baselines the baselines that fail a port alike, for
	// BlockFailsAtBase; Planned the target not accepted, for
	// BlockNotAccepted.
	Unmet     model.Unmet
	Target    *evidence.TargetEvidence
	Baselines []string
	Planned   model.PlanTarget
	// Path and Marks are a Portfile and the guesses it still marks, for
	// BlockUnconfirmed; Mark the line that marks them.
	Path  string
	Marks []string
	Mark  string
	// Commit is the merge commit, for BlockMerge.
	Commit model.ObjectID
	// State is the pending check's, for BlockCheckPending.
	State model.RunState
	// Pushed is someone's push to a pull request: its number, its author,
	// its head, where dockhand last pushed, and how to fetch it.
	Pushed *Push
}

// Push is a pull request someone else pushed to, or someone's whose head
// a submission would push to.
type Push struct {
	Number       int
	Author, Head string
	// At is the pull request's head now, and Last where dockhand last
	// pushed it; Remote and Branch fetch it.
	At, Last       model.ObjectID
	Remote, Branch string
	// Repository is MacPorts' repository, and Permission your access to
	// it, for BlockNoWriteAccess.
	Repository, Permission string
}

// Words are the block's sentence, as submit refuses with it, what it
// names worded by their owners.
func (b Block) Words(v Vocabulary) string {
	switch b.Kind {
	case BlockUnconfirmed:
		return fmt.Sprintf("%s still marks %s unconfirmed (%s): confirm each, remove its %q line, and tidy", b.Path, prose.Plural(len(b.Marks), "guess"), strings.Join(b.Marks, ", "), b.Mark)
	case BlockMerge:
		return fmt.Sprintf("commit %s is a merge; MacPorts asks for a rebase instead", b.Commit.Short())
	case BlockCheckPending:
		return fmt.Sprintf("%s checks this commit’s files and is %s. Run dockhand wait %s, then submit; or use --draft to submit now", b.Run, b.State, b.Run)
	case BlockCheckOfOtherFiles:
		return fmt.Sprintf("no completed check covers this commit’s files. Run dockhand check; it reuses eligible builds from %s and builds only what changed. Alternatively, use --draft or --no-check; --no-check is disclosed in the pull request", b.Run)
	case BlockNoCheck:
		return "no check has finished for this commit's files; run dockhand check first, submit a draft with --draft, or submit without a check with --no-check, which the pull request states"
	case BlockUnmet:
		return fmt.Sprintf("%s %s, which %s hasn't; a check with %s there builds it, or share the branch as a draft (--draft)", b.Port, v.Unmet(b.Unmet), v.Environment(b.Unmet.Environment), b.Unmet.Needs)
	case BlockRemade:
		return fmt.Sprintf("%s: the previous check is no longer valid because %s. Run dockhand check to rebuild in the affected environments, or share the branch with --draft", b.Port, v.Remade(*b.Target))
	case BlockUnchecked:
		return fmt.Sprintf("%s has changed, but these files have not been checked in every required environment. Run dockhand check, or share the branch with --draft", b.Port)
	case BlockFailsAtBase:
		return fmt.Sprintf("%s failed in %s and has the same failure at the base (%s). Use --accept %s to submit with this existing failure disclosed, fix the failure, or share the branch with --draft", b.Port, b.Run, strings.Join(b.Baselines, ", "), b.Port)
	case BlockFailed:
		return fmt.Sprintf("%s did not pass in %s; fix it, or share it as a draft (--draft); dockhand check --baseline sees whether master fails it the same way", b.Port, b.Run)
	case BlockNotAccepted:
		return fmt.Sprintf("%s (%s) did not pass in %s; acknowledge it with --accept %s if its failure is not this branch's doing", b.Port, kindWords(b.Planned), b.Run, b.Port)
	case BlockOthersPushed:
		p := b.Pushed
		return fmt.Sprintf("someone else pushed to #%d: it is at %s, and dockhand last pushed %s. Fetch it (git fetch %s %s) and compare; a commit built on it, as one made after git reset --hard %s, submits, and nothing will be pushed over it",
			p.Number, p.At.Short(), p.Last.Short(), p.Remote, p.Branch, p.At.Short())
	case BlockMaintainerEdits:
		p := b.Pushed
		return fmt.Sprintf("@%s's #%d doesn't let maintainers push to %s; suggest your changes in a review (dockhand review %d), or ask them to allow edits", p.Author, p.Number, p.Head, p.Number)
	case BlockNoWriteAccess:
		p := b.Pushed
		return fmt.Sprintf("pushing to @%s's %s needs write access to %s, and you have %s; suggest your changes in a review (dockhand review %d)", p.Author, p.Head, p.Repository, p.Permission, p.Number)
	case BlockNoTitle:
		return "the pull request needs a title, since the branch changes several ports: give one with --title"
	}
	return string(b.Kind)
}

// kindWords is how a target not accepted is named: an extra, a revision
// bump only, or a changed port.
func kindWords(target model.PlanTarget) string {
	if target.Role == model.Also {
		return "an extra from --also"
	}
	if target.Kind == model.RevisionOnly {
		return "revision bump only"
	}
	return "changed"
}
