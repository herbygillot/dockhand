package change

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/build"
	"github.com/herbygillot/dockhand/internal/record"
)

// Drift is how far a change's branch has fallen behind the base it was
// cut from, and whether the portdir it touches has moved underneath it.
// A change fact, read by the verification road before it spends a VM
// and by the publication road before it spends a reviewer.
//
// Compared is here because rule 7 governs every fact a decision reads
// and this is one: without it, BehindBy == 0 means both "up to date" and
// "the comparison could not run", and the two roads that consult it
// would spend a VM and a reviewer on the strength of a failure.
type Drift struct {
	Compared bool
	Err      error
	BehindBy int
	Base     record.Base
	OverTree bool
}

// ErrNoBase is Behind's soft answer for a change that records no base:
// there is nothing to measure from. It rides on Drift.Err with Compared
// false, never as a returned error, for the reason Compared exists.
var ErrNoBase = errors.New("change: the change records no base to measure from")

// Behind takes the change from the state ref and not the note's
// record.Record, which is the same rule Held and run.Pending follow:
// no decision reads the derived export. Every field it needs — Base,
// Content, Branch — is already on record.Change, and change does not
// import ledger, so the note's shape could only have reached it by the
// application reading a note and handing it over. That is the exact
// objection this design raised against an earlier publish.Facts.
//
// The branch parameter is what the change has fallen behind — the
// repository's primary branch at its LOCAL position, which is the base
// new work forks from (D21: the local position is the answer, staleness
// included) — and it is a parameter rather than a read because which
// branch that is belongs to the caller that opened the repository, and
// asking git here would make one fact into two.
//
// One git invocation answers both halves. The commits reachable from the
// branch but not from the base ARE how far behind it is, and the paths
// those commits touched are whether the change's own portdirs moved
// underneath it — so OverTree is a fact about the same range and not a
// second question asked of a second reading.
func Behind(ctx context.Context, repo *git.Repo, c record.Change, branch string) (Drift, error) {
	d := Drift{Base: c.Base}
	if repo == nil || branch == "" {
		return d, fmt.Errorf("%w: a drift needs a repository and a branch to measure against", ErrIncomplete)
	}
	if c.Base.Sha == "" {
		d.Err = ErrNoBase
		return d, nil
	}
	commits, err := repo.CommitsWithPaths(ctx, branch, c.Base.Sha)
	if err != nil {
		return d, err
	}
	mine := portdirs(c)
	d.Compared, d.BehindBy = true, len(commits)
	for _, commit := range commits {
		for _, path := range commit.Paths {
			if dir, ok := portdirOf(path); ok && mine[dir] {
				d.OverTree = true
				return d, nil
			}
		}
	}
	return d, nil
}

// portdirs is the set of portdirs a change's own subjects name.
func portdirs(c record.Change) map[string]bool {
	out := make(map[string]bool, len(c.Subjects))
	for _, s := range c.Subjects {
		if s.Portdir != "" {
			out[s.Portdir] = true
		}
	}
	return out
}

// ErrNoPortdir is the changed-portdir audit's answer for a branch that
// changes no portdir at all against its base: there is nothing to build,
// which is a refusal and not a failure.
var ErrNoPortdir = errors.New("change: the branch changes no portdir")

// ErrResourcesOnly is the same absence with its reason named: the branch
// touches only the ports tree's own resources — port groups, mirror and
// archive site lists, livecheck and compiler resources — which is not a
// malformed port change, it is not a port change. Said apart from
// ErrNoPortdir because "no portdir" sends a reader looking for a portdir
// that should have been there, and every one of the last forty upstream
// commits touching _resources touched nothing else, so this is the case
// that actually arrives.
var ErrResourcesOnly = errors.New("change: the branch changes only the tree's own resources")

// ErrPortdirsDisagree is the audit's refusal when git and the record
// name different portdirs. Typed as *PortdirDisagreement, because a
// person has to answer it and both sides are the answer.
var ErrPortdirsDisagree = errors.New("change: git and the record name different portdirs")

// PortdirDisagreement is what the audit found, as data: what the branch
// actually changes against its base, what the record claims it changes,
// and the base the comparison was made at.
//
// It is a value and not a sentence because both readings are wrong when
// they differ and only a person can say which is true — staging the
// record's set under-stages a portdir a later commit added, which
// verifies something other than the branch, and staging git's set
// verifies a directory the change never claimed.
type PortdirDisagreement struct {
	ID       record.ChangeID
	Base     string
	Derived  []string
	Recorded []string
}

func (e *PortdirDisagreement) Error() string {
	return fmt.Sprintf("change: the branch changes %s against %s, but its record names %s",
		strings.Join(e.Derived, ", "), git.Abbrev(e.Base), strings.Join(e.Recorded, ", "))
}

func (e *PortdirDisagreement) Unwrap() error { return ErrPortdirsDisagree }

// ChangedPortdirs derives the portdirs a change's tip touches against
// its base — from git alone, so a person's commit counts the same as a
// minted one's — and holds that answer against what the record claims.
//
// Field case (macports-ports-46): verify of a hand-made subport branch
// submitted the portdir's MAIN port — devel/pcre's base name is pcre,
// the branch changed pcre2, and the VM built the untouched 8.45 and
// would have called the branch verified. A portdir's name is not the
// name of what a branch changed; only evaluation can say that, and this
// answers the half that is git's: which DIRECTORIES moved.
//
// Plural, and the refusal that used to make it singular is gone. "One at
// a time for now" was a stand-in for a substrate that could carry only
// one subject; the substrate carries a cohort now, so a hand-made branch
// touching several portdirs verifies as the one change it is.
//
// THE CROSS-CHECK IS THE POINT. Two sources describe the same change and
// neither is authoritative alone: the record says what the change staged
// when it was minted, and git says what the branch actually touches now.
// Where they disagree it refuses, naming both sides, and lets a person
// say which is true. A record that names no portdir at all is not a
// disagreement — a subject adopted at submit time carries a port and
// nothing else, and both mean nobody said, so git's answer stands
// unopposed.
//
// The base is a PARAMETER and not a read of the record's own Base,
// because the question is what the branch changes against the tree it
// would land in, which is the caller's merge base with the primary
// branch — and a change whose base is a week old has not changed
// everything that landed since.
//
// It observes and refuses and does not print. The shipped derivation
// wrote an advisory to stderr about members the branch does not own when
// the local primary is behind its remote; that advisory is a SENTENCE
// about this data and belongs to the road that has a stream, which is
// rule 1 — this function returns what it found, and ForeignMembers below
// returns the advisory's own facts for the road that says it.
func ChangedPortdirs(ctx context.Context, repo *git.Repo, c record.Change, base string) ([]string, error) {
	if repo == nil || c.Tip == "" || base == "" {
		return nil, fmt.Errorf("%w: an audit needs a repository, a tip and a base", ErrIncomplete)
	}
	paths, err := repo.DiffNames(ctx, base, c.Tip)
	if err != nil {
		return nil, err
	}
	// Sorted, and deduplicated on the way in. The order is not cosmetic:
	// it becomes the build order of a cohort whose record does not state
	// one, and Subjects[0] is the headline a refusal names — so an answer
	// that came back in map order would give the same branch a different
	// headline on two consecutive runs.
	//
	// Deterministic is all it is. This sorts PORTDIRS, so the order is
	// alphabetical by category and says nothing about which member depends
	// on which. Nothing here topologically sorts anything and no reader
	// may assume it does. What makes that survivable is that blame does
	// not turn on the order: a member's install pulls its siblings in as
	// ordinary dependencies whatever position they hold, and the judge
	// recovers the culprit by matching the log's name against the change's
	// roster rather than against a position in it.
	seen := map[string]bool{}
	var derived []string
	resources := false
	for _, path := range paths {
		dir, ok := portdirOf(path)
		if !ok {
			// A path with no third segment lies under no portdir; one under
			// the tree's own resources is skipped as a SUBJECT and not as
			// content — staging materializes _resources from the branch's own
			// tip, so an edited port group reaches the guest and governs
			// whatever ports are built beside it. What it is not is a port to
			// build.
			if strings.HasPrefix(path, build.ResourcesDir+"/") {
				resources = true
			}
			continue
		}
		if seen[dir] {
			continue
		}
		seen[dir] = true
		derived = append(derived, dir)
	}
	slices.Sort(derived)
	if len(derived) == 0 {
		if resources {
			return nil, fmt.Errorf("%w: %s against %s", ErrResourcesOnly, build.ResourcesDir, git.Abbrev(base))
		}
		return nil, fmt.Errorf("%w against %s", ErrNoPortdir, git.Abbrev(base))
	}
	recorded := recordedPortdirs(c)
	if len(recorded) == 0 {
		return derived, nil
	}
	if !sameSet(recorded, derived) {
		return nil, &PortdirDisagreement{ID: c.ID, Base: base, Derived: derived, Recorded: recorded}
	}
	// The record's own order wins where it agrees, because it knows
	// something git does not: which subject is the headline, and the order
	// the members must be built in.
	return recorded, nil
}

// Foreign is one member of a derived roster that the branch does not
// own: a portdir a stale primary put in the diff, with the upstream
// commits that actually touched it, oldest first — the order they landed
// upstream, which is how a person reading a log expects to find them.
//
// It is DATA and not a sentence, for the reason ChangedPortdirs gives
// above: the shipped derivation wrote this to stderr from inside the
// derivation, and the road that has a stream is the one that may say it.
type Foreign struct {
	Portdir string
	From    []git.CommitPaths
}

// ForeignMembers names which of a derived roster's portdirs came from
// commits the branch does not own — and changes nothing.
//
// The condition is a STALE PRIMARY. The diff's base is the LOCAL
// primary, which never fetches (D21: the local position is the answer,
// staleness included), while a hand-made branch is ordinarily cut from
// origin/<primary>, which dockhand's own retire sweep advances when a
// pull request merges. Everything upstream landed between the two
// positions is then in the branch's diff, and its portdirs are counted,
// built, and claimed as the branch's — a cohort submitted as
// `oniguruma6, jq, mise` when the branch touched two, the third being
// dockhand's own merged pull request (field, 2026-09-03). A branch
// dockhand minted is immune: it forks from the local primary, so its
// merge base is its fork point.
//
// Ruled an advisory (2026-09-04): D21 stands, the roster stands, and the
// road that derived it says which members are somebody else's and where
// they came from. The remedy is the user's — a fast-forward of the local
// primary moves the merge base, and the foreign commits fall out of the
// diff on the next derivation with no re-cut of the branch.
//
// FROM REFS ALONE. The remote-tracking ref is whatever the last fetch
// left, and the commits the branch carries that the primary lacks are
// the range from the diff's base to the branch's fork point on that ref:
// reachable from the tip and from origin/<primary>, and not from
// <primary>. A member is named only when no commit of the branch's own
// touches it — the branch editing a port upstream also moved is a roster
// the branch earned, not an enlargement.
//
// BEST EFFORT, on the reflog's model: no remote-tracking ref means
// nothing to compare against, and a git error on the way says nothing at
// all. This is corroboration beside a roster, and a verification must
// not fail over the words beside it — which is why it returns one value
// and no error, and why an empty answer means only that there is nothing
// to say.
func ForeignMembers(ctx context.Context, repo *git.Repo, primary, base, tip string, derived []string) []Foreign {
	if repo == nil || primary == "" || base == "" || tip == "" || len(derived) == 0 {
		return nil
	}
	// The remote-tracking ref, spelled here because it is nobody's
	// authority: refs/remotes/ is this machine's cache of what a fetch
	// last saw, no ref dockhand writes lives under it, and the store's
	// three owned namespaces are elsewhere. Reading it is a reading, and
	// this whole function is one.
	remote := "refs/remotes/origin/" + primary
	if _, err := repo.RevParse(ctx, remote); err != nil {
		return nil
	}
	fork, err := repo.MergeBase(ctx, remote, tip)
	if err != nil || fork == base {
		return nil
	}
	foreign, err := repo.CommitsWithPaths(ctx, fork, base)
	if err != nil {
		return nil
	}
	ownPaths, err := repo.DiffNames(ctx, fork, tip)
	if err != nil {
		return nil
	}
	own := map[string]bool{}
	for _, p := range ownPaths {
		if dir, ok := portdirOf(p); ok {
			own[dir] = true
		}
	}
	inRoster := make(map[string]bool, len(derived))
	for _, dir := range derived {
		inRoster[dir] = true
	}
	from := map[string][]git.CommitPaths{}
	for i := len(foreign) - 1; i >= 0; i-- {
		c := foreign[i]
		named := map[string]bool{}
		for _, p := range c.Paths {
			dir, ok := portdirOf(p)
			if !ok || !inRoster[dir] || own[dir] || named[dir] {
				continue
			}
			named[dir] = true
			from[dir] = append(from[dir], git.CommitPaths{Sha: c.Sha, Subject: c.Subject})
		}
	}
	var out []Foreign
	for _, dir := range derived {
		if commits, ok := from[dir]; ok {
			out = append(out, Foreign{Portdir: dir, From: commits})
		}
	}
	return out
}

// recordedPortdirs is what the record says the change touches, in the
// record's own order, without repeats: a cohort's members may share a
// portdir and the roster must not name it twice.
func recordedPortdirs(c record.Change) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range c.Subjects {
		if s.Portdir == "" || seen[s.Portdir] {
			continue
		}
		seen[s.Portdir] = true
		out = append(out, s.Portdir)
	}
	return out
}

// portdirOf is the portdir a path lies under — its first two segments —
// and false for a path with no third segment to lie under them, or one
// under the tree's own resources.
//
// By name rather than by a leading underscore. The tree has exactly one
// of these among 59 top-level directories, and a rule about underscores
// would be a guess at a convention MacPorts has not stated.
func portdirOf(path string) (string, bool) {
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 3 || parts[0] == build.ResourcesDir {
		return "", false
	}
	return parts[0] + "/" + parts[1], true
}

// sameSet reports whether two portdir lists name the same directories,
// order and repeats aside. Order is a property of the record and not of
// the diff, so comparing it would refuse a change that agrees.
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
