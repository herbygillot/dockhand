package engine

// Field case (macports-ports-46): verify of a hand-made subport branch
// submitted the portdir's MAIN port — devel/pcre's base name is pcre,
// the branch changed pcre2, and the VM built the untouched 8.45 and
// would have called the branch verified. A portdir's name is not the
// name of what a branch changed; only evaluation can say that.

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/build"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
)

// ChangedPortdirs derives the portdirs a branch changes against its
// merge base with the primary branch — from git alone, so a human
// commit's changes count the same as a minted one's — and holds that
// answer against what the tip's record claims.
//
// Plural, and the refusal that used to make it singular is gone. "One
// at a time for now" was a stand-in for a substrate that could carry
// only one subject; the substrate carries a cohort now, so a hand-made
// branch touching several portdirs verifies as the one change it is.
//
// The cross-check is why this is a method. Two sources describe the
// same change and neither is authoritative alone: the record says what
// the change staged when it was minted, and git says what the branch
// actually touches now. Where they disagree, both readings are wrong —
// staging the record's set under-stages a portdir a later commit
// added, which is a verification of something other than the branch,
// and staging git's set verifies a directory the change never claimed.
// So it refuses, naming both sides, and lets a person say which is
// true.
//
// A record that names no portdir at all is not a disagreement. A
// subject adopted at submit time carries a port and nothing else, and
// a branch nobody minted has no record to ask; both mean nobody said,
// and git's answer stands unopposed.
func (e *Engine) ChangedPortdirs(ctx context.Context, repo *git.Repo, branch, tip string) ([]string, error) {
	primary, err := repo.PrimaryBranch(ctx)
	if err != nil {
		return nil, err
	}
	base, err := repo.MergeBase(ctx, primary, tip)
	if err != nil {
		return nil, err
	}
	paths, err := repo.DiffNames(ctx, base, tip)
	if err != nil {
		return nil, err
	}
	// Sorted, and deduplicated on the way in. The order is not
	// cosmetic: it becomes the build order of a cohort whose record
	// does not state one, and Subjects[0] is the headline a refusal
	// names — so an answer that came back in map order would give the
	// same branch a different headline on two consecutive runs.
	//
	// Deterministic is all it is. This sorts PORTDIRS, so the order is
	// alphabetical by category and says nothing about which member
	// depends on which — a branch touching sysutils/jq and
	// textproc/oniguruma builds the dependent first, decided by a
	// category name. Nothing here topologically sorts anything and no
	// reader may assume it does. What makes that survivable is that
	// blame does not turn on the order: a member's install pulls its
	// siblings in as ordinary dependencies whatever position they hold,
	// and the judge recovers the culprit by matching the log's name
	// against the change's roster rather than against a position in it.
	// Ordering members by declared dependency is a real improvement and
	// a change of its own — the pre-flight already opens every Portfile
	// — and it is not smuggled in here.
	seen := map[string]bool{}
	var derived []string
	resources := false
	for _, p := range paths {
		parts := strings.SplitN(p, "/", 3)
		if len(parts) < 3 {
			continue
		}
		if parts[0] == build.ResourcesDir {
			// The tree's own infrastructure — port groups, mirror and
			// archive site lists, livecheck and compiler resources —
			// and not a category. Taken as one, its first two segments
			// name a portdir "_resources/port1.0" and a port "port1.0",
			// which stages, indexes, and fails in a guest against a port
			// nobody has ever heard of.
			//
			// It is skipped as a subject and not as content: staging
			// materializes _resources from the branch's own tip, so an
			// edited port group reaches the guest and governs whatever
			// ports are built beside it. What it is not is a port to
			// build.
			//
			// By name rather than by a leading underscore. The tree has
			// exactly one of these among 59 top-level directories, and a
			// rule about underscores would be a guess at a convention
			// MacPorts has not stated.
			resources = true
			continue
		}
		if seen[parts[0]+"/"+parts[1]] {
			continue
		}
		d := parts[0] + "/" + parts[1]
		seen[d] = true
		derived = append(derived, d)
	}
	sort.Strings(derived)
	if len(derived) == 0 {
		// Said apart, because "no portdir" sends a reader looking for a
		// portdir that should have been there. A branch under
		// _resources alone is not a malformed port change; it is not a
		// port change. Every one of the last forty commits touching
		// _resources upstream touched nothing else, so this is the case
		// that actually arrives.
		if resources {
			return nil, fmt.Errorf("verify: %s changes only %s/ against %s — tree resources, not a port; dockhand has nothing to build here",
				branch, build.ResourcesDir, git.Abbrev(base))
		}
		return nil, fmt.Errorf("verify: %s changes no portdir against %s; there is nothing to verify", branch, git.Abbrev(base))
	}
	// Said where the roster is derived and nowhere else, and about the
	// roster rather than the diff: a foreign commit under _resources
	// enlarges nothing that gets built.
	e.adviseForeignMembers(ctx, repo, branch, primary, base, tip, derived)
	n, err := e.Ledger(repo).Read(ctx, tip)
	if err != nil {
		// No record, or one this build cannot read. The second is a
		// refusal everywhere it matters — the ledger says so to the verbs
		// that write — and here it is only the absence of a second
		// opinion about what git already answered.
		return derived, nil
	}
	recorded := n.Portdirs()
	if len(recorded) == 0 {
		return derived, nil
	}
	if !sameSet(recorded, derived) {
		return nil, fmt.Errorf(
			"verify: %s changes %s against %s, but its record names %s; the two disagree, so nothing is staged — `dockhand discard %s` and re-mint, or verify the portdirs by hand",
			branch, strings.Join(derived, ", "), git.Abbrev(base), strings.Join(recorded, ", "), branch)
	}
	// The record's own order wins where it agrees, because it knows
	// something git does not: which subject is the headline, and the
	// order the members must be built in.
	return recorded, nil
}

// adviseForeignMembers says, on stderr, which of a roster's portdirs
// came from commits the branch does not own — and changes nothing.
//
// The condition is a stale primary. The diff's base is the LOCAL
// primary, which never fetches (D21: the local position is the answer,
// staleness included), while a hand-made branch is ordinarily cut from
// origin/<primary>, which dockhand's own retire sweep advances when a
// PR merges. Everything upstream landed between the two positions is
// then in the branch's diff, and its portdirs are counted, built, and
// claimed as the branch's — a cohort submitted as `oniguruma6, jq,
// mise` when the branch touched two, the third being dockhand's own
// merged PR (field, 2026-09-03). A branch dockhand minted is immune:
// it forks from the local primary, so its merge base is its fork
// point.
//
// Ruled an advisory (2026-09-04): D21 stands, the roster stands, and
// one line says which members are somebody else's and where they came
// from. The remedy is the user's, and the line names it — a
// fast-forward of the local primary moves the merge base, and the
// foreign commits fall out of the diff on the next derivation with no
// re-cut of the branch.
//
// From refs alone. The remote-tracking ref is whatever the last fetch
// left, and the commits the branch carries that the primary lacks are
// the range from the diff's base to the branch's fork point on that
// ref: reachable from the tip and from origin/<primary>, and not from
// <primary>. A member is named only when no commit of the branch's
// own touches it — the branch editing a port upstream also moved is a
// roster the branch earned, not an enlargement. No ref means nothing
// to compare against, and a git error on the way says nothing at all:
// best effort, on the reflog's model, because this is corroboration
// beside the roster and a verification must not fail over the words
// beside it.
func (e *Engine) adviseForeignMembers(ctx context.Context, repo *git.Repo, branch, primary, base, tip string, derived []string) {
	remote := "refs/remotes/origin/" + primary
	if _, err := repo.RevParse(ctx, remote); err != nil {
		return
	}
	fork, err := repo.MergeBase(ctx, remote, tip)
	if err != nil || fork == base {
		return
	}
	foreign, err := repo.CommitsWithPaths(ctx, fork, base)
	if err != nil {
		return
	}
	ownPaths, err := repo.DiffNames(ctx, fork, tip)
	if err != nil {
		return
	}
	own := map[string]bool{}
	for _, p := range ownPaths {
		if d, ok := portdirOf(p); ok {
			own[d] = true
		}
	}
	inRoster := make(map[string]bool, len(derived))
	for _, d := range derived {
		inRoster[d] = true
	}
	// By portdir, each with the commits that touched it oldest first —
	// the order they landed upstream, which is how a person reading a
	// log expects to find them.
	from := map[string][]string{}
	for i := len(foreign) - 1; i >= 0; i-- {
		c := foreign[i]
		named := map[string]bool{}
		for _, p := range c.Paths {
			d, ok := portdirOf(p)
			if !ok || !inRoster[d] || own[d] || named[d] {
				continue
			}
			named[d] = true
			from[d] = append(from[d], git.Abbrev(c.Sha)+" "+c.Subject)
		}
	}
	if len(from) == 0 {
		return
	}
	var members []string
	for _, d := range derived {
		if commits, ok := from[d]; ok {
			members = append(members, fmt.Sprintf("%s (from %s)", d, strings.Join(commits, ", ")))
		}
	}
	fmt.Fprintf(e.Err, "%s: %s is behind origin/%s, so the change counts portdirs the branch does not own: %s; fast-forward %s and the roster is the branch's own again\n",
		branch, primary, primary, strings.Join(members, ", "), primary)
}

// portdirOf is the portdir a path lies under — its first two segments —
// and false for a path with no third segment to lie under them, or one
// under the tree's own resources, which ChangedPortdirs explains is not
// a port.
func portdirOf(path string) (string, bool) {
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 3 || parts[0] == build.ResourcesDir {
		return "", false
	}
	return parts[0] + "/" + parts[1], true
}

// sameSet reports whether two portdir lists name the same directories,
// order and repeats aside. Order is a property of the record and not
// of the diff, so comparing it would refuse a change that agrees.
func sameSet(a, b []string) bool {
	in := make(map[string]bool, len(a))
	for _, s := range a {
		in[s] = true
	}
	for _, s := range b {
		if !in[s] {
			return false
		}
		delete(in, s)
	}
	return len(in) == 0
}

// Member is one subject of a verification: the port to build, and the
// directory the branch changed that it is built out of.
//
// The pair is what a submission needs and what only a derivation can
// supply. A portdir's base name is not its port's name (devel/pcre
// carries pcre2), and a port name does not say which directory a
// branch touched, so the two are carried together from the one place
// that established them.
type Member struct {
	Port    string
	Portdir string
}

// SubjectsOf names what a branch verification builds: one member per
// changed portdir, in the order the portdirs came back, headline
// first.
//
// One portdir is today's question and is answered by today's function,
// unchanged and unshared. That is deliberate: SubjectOf's resolution
// order — the port the user named, then the tip note's headline, then
// evaluation — is the behaviour every single-subject verification has,
// and a plural rewrite that happened to agree in the common case would
// still be a different function answering it.
//
// Several portdirs cannot use that order. The user's target names one
// port and a cohort builds all of them, and the note's headline
// answers for one portdir out of several — so each directory is asked
// about itself: the record's own subject for it when the record has
// one, and evaluation of the directory when it does not.
func (e *Engine) SubjectsOf(ctx context.Context, repo *git.Repo, target, branch, tip string, rels []string) ([]Member, error) {
	if len(rels) == 1 {
		name, err := e.SubjectOf(ctx, repo, target, branch, tip, rels[0])
		if err != nil {
			return nil, err
		}
		return []Member{{Port: name, Portdir: rels[0]}}, nil
	}
	byDir := map[string]string{}
	if n, err := e.Ledger(repo).Read(ctx, tip); err == nil {
		for _, s := range n.Subjects {
			if s.Portdir != "" && s.Port != "" {
				byDir[s.Portdir] = s.Port
			}
		}
	}
	out := make([]Member, 0, len(rels))
	for _, rel := range rels {
		name, ok := byDir[rel]
		if !ok {
			var err error
			if name, err = e.changedPort(ctx, repo, tip, rel); err != nil {
				return nil, err
			}
		}
		out = append(out, Member{Port: name, Portdir: rel})
	}
	return out, nil
}

// SubjectOf names what a branch verification builds. The portdir's
// base name is NOT the answer — devel/pcre's branch may change pcre2,
// and building the parent verifies nothing about the change
// (field-caught: the VM built the untouched pcre 8.45 and would have
// called the pcre2 branch verified). Resolution, most direct authority
// first: the port the user themselves named (a target that matched the
// branch as dockhand/<target>-*, the mint's own naming); the tip note's
// recorded port (written from the plan's subport at bump time); and for
// a hand-made branch with neither, the context the branch's own diff
// moves under evaluation.
func (e *Engine) SubjectOf(ctx context.Context, repo *git.Repo, target, branch, tip, rel string) (string, error) {
	if target != branch {
		return target, nil
	}
	if n, err := e.Ledger(repo).Read(ctx, tip); err == nil && n.Headline().Port != "" {
		// The headline and not "the record's port": a change's subjects
		// are ordered, and the first is the one the branch is named for
		// and the one a verification is about.
		return n.Headline().Port, nil
	}
	return e.changedPort(ctx, repo, tip, rel)
}

// changedPort names the one context a branch's change is about, by
// evaluation: the portdir is materialized at the merge base and at the
// tip, both snapshot totally (D13), and the diff names the contexts
// that moved. Exactly one moved context is the answer; several is a
// refusal naming them, because a verification must know what it
// verifies. No moved context at all — a patch-file-only change
// evaluates identically — falls back to the portdir's base name,
// which is today's behavior for exactly the branches it is right for.
//
// Its two refusals are prefixed `lifecycle:` and stay that way. The
// package that carried the words is gone, but the words themselves
// reached users before it did, and moving code is not a licence to
// reword what the move happens to pass through; renaming the prefix is
// a change to what dockhand says and belongs to a step that says so.
func (e *Engine) changedPort(ctx context.Context, repo *git.Repo, tip, rel string) (string, error) {
	primary, err := repo.PrimaryBranch(ctx)
	if err != nil {
		return "", err
	}
	base, err := repo.MergeBase(ctx, primary, tip)
	if err != nil {
		return "", err
	}
	// A session of its own, closed here: it evaluates two materialized
	// snapshots under temporary directories this function removes, and
	// the run's own evaluator would outlive them.
	ev, err := e.Session(ctx)
	if err != nil {
		return "", err
	}
	defer ev.Close()

	root, err := e.Temp()
	if err != nil {
		return "", err
	}
	snapshotAt := func(sha, purpose string) (info.Snapshot, error) {
		stage, remove, err := root.MakeDir(purpose)
		if err != nil {
			return nil, err
		}
		defer remove()
		if err := repo.Materialize(ctx, sha, rel, stage); err != nil {
			return nil, err
		}
		h := port.New(tree.Target{Portdir: filepath.Join(stage, filepath.FromSlash(rel))}, ev)
		snap, err := h.Snapshot(ctx)
		if err != nil {
			return nil, fmt.Errorf("lifecycle: evaluating %s at %s: %w", rel, git.Abbrev(sha), err)
		}
		return snap, nil
	}
	before, err := snapshotAt(base, "portname-base")
	if err != nil {
		return "", err
	}
	after, err := snapshotAt(tip, "portname-tip")
	if err != nil {
		return "", err
	}

	d := before.Diff(after)
	moved := map[string]bool{}
	for key := range d.Changed {
		moved[key.Subport] = true
	}
	for key := range d.Added {
		moved[key.Subport] = true
	}
	switch len(moved) {
	case 1:
		for name := range moved {
			return name, nil
		}
	case 0:
		return filepath.Base(rel), nil
	}
	names := make([]string, 0, len(moved))
	for name := range moved {
		names = append(names, name)
	}
	sort.Strings(names)
	return "", &AmbiguousContextError{Contexts: names}
}

// AmbiguousContextError is a branch whose change moved several
// evaluation contexts at once: the verification needs one subject and
// the branch names more than one.
//
// It shares its band with the ambiguous branch target, because it is
// the same shape one level down — the request did not say enough, and
// naming the one settles it. The `lifecycle:` prefix outlived the
// package that gave it: the code moved to the engine and the sentence
// did not, because the words are what a user reads and a move does not
// get to reword them.
type AmbiguousContextError struct{ Contexts []string }

func (e *AmbiguousContextError) Error() string {
	return fmt.Sprintf("lifecycle: the branch changes %d contexts (%s); name the one to verify: `dockhand verify <subport>`",
		len(e.Contexts), strings.Join(e.Contexts, ", "))
}

// DockhandExit: the declined band's ambiguity code — say which.
func (e *AmbiguousContextError) DockhandExit() int { return exitcode.Ambiguous }

// Code names the refusal for a machine.
func (e *AmbiguousContextError) Code() string { return "ambiguous-context" }
