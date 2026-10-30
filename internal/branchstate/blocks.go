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
	// BlockAwaiting is a concern the person decided to wait on, its
	// subject in Port and what for in Words (the person's voice).
	BlockAwaiting BlockKind = "awaiting"
	// BlockLint is a port the branch adds that lint warned of, not yet
	// accepted (the docs dig's I13).
	BlockLint BlockKind = "lint"
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
	// BlockNoFork is no fork to push to, which a preview shows with the
	// rest of its plan, pushing nothing either way (rc1's quick stage).
	BlockNoFork BlockKind = "no-fork"
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
	Unmet  model.Unmet
	Target *evidence.TargetEvidence
	// Unbuilt are where a changed target wasn't built, for
	// BlockUnchecked, each with the check that planned it there where
	// that's why it's asked for.
	Unbuilt   []Place
	Baselines []string
	Planned   model.PlanTarget
	// Lint are what lint warned of, for BlockLint.
	Lint []string
	// Awaited is what the person awaits, for BlockAwaiting.
	Awaited string
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
	// NoFork is why no fork was found, and the way on, for BlockNoFork.
	NoFork string
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
		return fmt.Sprintf("%s contains %s marked unconfirmed (%s). Verify each item, remove its %q line, and run dockhand tidy", b.Path, prose.Plural(len(b.Marks), "guess"), strings.Join(b.Marks, ", "), b.Mark)
	case BlockMerge:
		return fmt.Sprintf("commit %s is a merge; MacPorts asks for a rebase instead", b.Commit.Short())
	case BlockCheckPending:
		return fmt.Sprintf("%s checks this commit’s files and is %s. Run dockhand wait %s, then submit; or use --draft to submit now", b.Run, b.State, b.Run)
	case BlockCheckOfOtherFiles:
		return fmt.Sprintf("no completed check covers this commit’s files. Run dockhand check; it reuses eligible builds from %s and builds only what changed. Alternatively, use --draft or --no-check; --no-check is disclosed in the pull request", b.Run)
	case BlockNoCheck:
		return "no check has finished for this commit's files; run dockhand check first, submit a draft with --draft, or submit without a check with --no-check, which the pull request states"
	case BlockUnmet:
		return fmt.Sprintf("%s %s. %s does not have it; run a check in an environment with %s, or submit a draft with --draft", b.Port, v.Unmet(b.Unmet), v.Environment(b.Unmet.Environment), b.Unmet.Needs)
	case BlockRemade:
		return fmt.Sprintf("%s: the previous check is no longer valid because %s. Run dockhand check to rebuild in the affected environments, or share the branch with --draft", b.Port, v.Remade(*b.Target))
	case BlockUnchecked:
		if len(b.Unbuilt) == 0 {
			return fmt.Sprintf("%s has changed, but these files have not been checked in every required environment. Run dockhand check, or share the branch with --draft", b.Port)
		}
		// Each place it wasn't built, and, outside build.default's
		// profile, the check that planned it there, where the block said
		// only "every required environment" (the exiv2 session's note 7).
		var places []string
		for _, u := range b.Unbuilt {
			place := v.Environment(u.Environment)
			switch {
			case u.Unrun != "":
				// A release whose build never ran, as a GitHub job no
				// runner took, is checked elsewhere or again later (rc8's
				// GitHub rows).
				place += ": " + u.Unrun + "; check that release with Tart, local, or a command provider, or try again later"
			case u.PlannedIn != "":
				place += ", planned by " + u.PlannedIn + ", but the check stopped before building it"
			}
			places = append(places, place)
		}
		return fmt.Sprintf("%s has changed, but these files haven't been built on %s. Run dockhand check, or share the branch with --draft", b.Port, strings.Join(places, "; "))
	case BlockFailsAtBase:
		return fmt.Sprintf("%s failed in %s and has the same failure at the base (%s). Use --accept %s to submit with this existing failure disclosed, fix the failure, or share the branch with --draft", b.Port, b.Run, strings.Join(b.Baselines, ", "), b.Port)
	case BlockFailed:
		return fmt.Sprintf("%s did not pass in %s. Fix the failure, submit a draft with --draft, or run dockhand check --baseline to see whether master fails in the same way", b.Port, b.Run)
	case BlockNotAccepted:
		return fmt.Sprintf("%s (%s) did not pass in %s. If the failure was not caused by this branch, acknowledge it with --accept %s", b.Port, kindWords(b.Planned), b.Run, b.Port)
	case BlockAwaiting:
		port, rule, _ := strings.Cut(b.Port, " ")
		return fmt.Sprintf("%s: you're awaiting %s. Decide it once you have it, dockhand decide %s -p %s --because \"…\", or share the branch with --draft", b.Port, b.Awaited, rule, port)
	case BlockLint:
		return fmt.Sprintf("%s is a new port, and lint --nitpick warned in %s: %s. Fix %s and check again, or take %s with --accept %s", b.Port, b.Run, strings.Join(b.Lint, "; "), map[bool]string{true: "it", false: "them"}[len(b.Lint) == 1], map[bool]string{true: "it", false: "them"}[len(b.Lint) == 1], b.Port)
	case BlockOthersPushed:
		p := b.Pushed
		return fmt.Sprintf("someone else pushed to #%d. The remote is at %s; Dockhand last pushed %s. Fetch it with git fetch %s %s and compare the changes, then incorporate it into your branch, for example with git merge %s, before submitting; Dockhand will not push over it",
			p.Number, p.At.Short(), p.Last.Short(), p.Remote, p.Branch, p.At.Short())
	case BlockMaintainerEdits:
		p := b.Pushed
		return fmt.Sprintf("@%s's #%d doesn't let maintainers push to %s; suggest your changes in a review (dockhand review %d), or ask them to allow edits", p.Author, p.Number, p.Head, p.Number)
	case BlockNoWriteAccess:
		p := b.Pushed
		return fmt.Sprintf("pushing to @%s's %s needs write access to %s, and you have %s; suggest your changes in a review (dockhand review %d)", p.Author, p.Head, p.Repository, p.Permission, p.Number)
	case BlockNoTitle:
		return "the pull request needs a title, since the branch changes several ports: give one with --title"
	case BlockNoFork:
		return b.NoFork
	}
	return string(b.Kind)
}

// Place is an environment a changed target wasn't built in, and the
// check that planned it there, where outside build.default's profile
// that's why it's asked for.
type Place struct {
	Environment model.Environment
	PlannedIn   string
	// Unrun is why its build never ran, in its provider's words
	// (evidence.Cell.Unrun).
	Unrun string
}

// unbuilt are where a target wasn't built, as its cells say.
func unbuilt(target evidence.TargetEvidence) []Place {
	var where []Place
	for _, cell := range target.Outcomes {
		if cell.Kind == evidence.CellNotRun {
			where = append(where, Place{Environment: cell.Environment, PlannedIn: cell.PlannedIn, Unrun: cell.Unrun})
		}
	}
	return where
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
