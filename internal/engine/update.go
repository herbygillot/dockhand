package engine

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/failpoint"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/macports/portcreate"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/macports/version"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/scratch"
	"github.com/herbygillot/dockhand/internal/sourcecompare"
	"github.com/herbygillot/dockhand/internal/store"
)

// HandBranch reports whether a Start request starts its branch for an edit
// dockhand can't make, for the person to make it by hand: a person's own
// update does, and one nobody looks over, or one of a batch, leaves
// nothing (field testing's cleanup, af03bbab: pgdog, reposurgeon, and
// termusic each left an empty branch status listed for hours). The one
// decision of it, for serve's ship and the command's (the architecture
// review of 5897a82, R3-11).
func (r UpdateRequest) HandBranch() bool {
	return r.Start != nil && !r.Unattended && !r.Batch
}

// UpdateRequest asks to edit one port's files in a branch's worktree.
type UpdateRequest struct {
	Branch model.Branch
	// Batch is one port's update among several, which no person is at
	// for this one: it keeps no branch for an edit by hand (HandBranch).
	Batch bool
	// Action is model.EditUpdate, a new version; model.EditChecksums, the
	// checksums of the version the Portfile names; or model.EditRevbump, a
	// new revision. The edit it records is of the same kind.
	Action model.EditKind
	Port   string
	// Version is the release a bump moves to; the newest when empty.
	Version string
	// Release is that release where it was found already, as outdated
	// finds one: the update takes it rather than asking upstream again,
	// and checks it against the Portfile as any. Its version is Version's.
	Release *model.Release
	// EditIntent is what the person authorized for the edit, which it
	// records: SharedRelease moves every subport that shares the port's
	// release, and KeepOldChecksums refreshes a legacy checksum group's
	// values in place (the architecture review's C4c: embedded, not
	// copied field by field).
	model.EditIntent
	// Closes are the Trac tickets the update closes, by number, which the
	// branch keeps for tidy's Closes: trailer (the docs dig's I41).
	Closes []string
	// Subject is the reason for a revision bump, the commit subject after
	// the port's name: "rebuild for poppler 25.09.0"; Excepted are the
	// dependents --except left out of the same rebuild, which its edit
	// keeps for the pull request's description.
	Subject  string
	Excepted []string
	// Plan prepares the edit and changes nothing.
	Plan bool
	// FromMaster plans against master as fetched now, with no branch: a
	// look before starting one. It goes only with Plan.
	FromMaster bool
	// Start, for a version update, prepares it against master as fetched
	// now and starts this branch from that master only once there is an
	// edit to make: a port already current starts nothing. An edit
	// dockhand can't make by itself starts it too, for the person to make
	// by hand. It takes the place of Branch.
	Start *StartRequest
	// Started is a branch started for this update before it was asked,
	// which an update that doesn't go keeps (refusedUpdate).
	Started bool
	// KeepRevision leaves the revision of a stealth update as it is, for a
	// change that needs no rebuild.
	KeepRevision bool
	// CompareUpstream fetches the current version's archives beside the
	// new ones and compares them, for a version update (Design v3 §6.12).
	CompareUpstream bool
	// LookForOthers names the port's other open pull requests, for a
	// version update, as submit's preview does: someone starting an update
	// should hear of one in flight. It holds nothing, and stops nothing.
	LookForOthers bool
	// Unattended is a version update no person looks over before it is
	// submitted, bump's. It looks for the port's other open pull requests
	// once its release is found and before it prepares the edit, and is
	// held there on one, or on not knowing (HeldBeforeEdit), since either
	// would hold the submission after the check: nothing is downloaded or
	// built for it. Submit looks again before publishing.
	Unattended bool
	// HandEdit asks, where dockhand refuses the update as an edit it
	// doesn't make, for its best attempt, written as the person's hand edit
	// with the refusal recorded (model.FindingHandEdit). Attended only:
	// serve and bump never take it.
	HandEdit bool
	// FollowMoves points github.setup at the repository GitHub now answers
	// the release's by, where it moved: the person's choice, never made
	// unasked (part 2's new holds), save by the port's own holds, moves =
	// "follow", for an unattended update, standingFollow, which never
	// follows a port rename (portRename).
	FollowMoves    bool
	standingFollow bool
	// WithObsolete also moves the obsolete stub the Portfile keeps for the
	// port, one replaced_by it that fetches nothing, to the same version,
	// in the same commit: terraform beside terraform-1.16, whose version
	// follows the newest subport's by convention (field testing,
	// 2026-10-02, the person's word).
	WithObsolete bool
	// subject is the commit subject the edit is recorded with, where it's
	// another edit's: an obsolete stub moved with its replacement is the
	// replacement's update.
	subject string
	// answered are the HTTPS answers the command making the update has had
	// already, create's for the homepage it wrote, which aren't asked
	// again (plainHTTP).
	answered map[string]bool
}

// PortVersion is a port's version and revision.
type PortVersion struct {
	Version  string
	Revision int
}

func (v PortVersion) String() string {
	if v.Revision == 0 {
		return v.Version
	}
	return fmt.Sprintf("%s_%d", v.Version, v.Revision)
}

// ErrUnsupported is an edit dockhand can't make by itself, such as a
// version it can't find in the Portfile; the error says why, after the
// sentinel's own words.
var ErrUnsupported = editprep.ErrUnsupported

// ErrFidelity is an edit whose evaluation isn't the change intended, which
// update refuses as it refuses an edit it can't make, with how to make it
// by hand.
var ErrFidelity = editprep.ErrFidelity

// Unlocated is a checksum declaration dockhand can't find in the Portfile
// to edit, an unsupported edit that names the archive and why.
type Unlocated = editprep.Unlocated

// ChecksumsToWrite is a checksum refresh dockhand couldn't write, with the
// archives' checksums for a person to write.
type ChecksumsToWrite = editprep.ChecksumsToWrite

// Update reports an update or checksum refresh.
type Update struct {
	Branch model.Branch
	// Base is the commit the update was prepared on: the branch's base, or
	// master as fetched for a plan from master.
	Base model.ObjectID
	// Port is the name the Portfile evaluates to.
	Port          string
	Before, After PortVersion
	// CrossesMajor is an update to a new major version, semgrep's 0.14.0
	// to 1.179.0, which read as a plain bump (field testing, 2026-10-02).
	CrossesMajor bool
	// LeavesStable is an update from a stable version to a prerelease,
	// jq's 1.8.1 to 2.0-rc1, which only a version the person names takes
	// (the docs dig's I32); LeavesStableWords say it.
	LeavesStable bool
	// ClosedBefore says an earlier update of the port to this version was
	// closed unmerged, which an attended update goes ahead of and says,
	// and an unattended look doesn't propose again (the docs dig's I1);
	// empty where none was.
	ClosedBefore string
	// Release is where a bump found its version.
	Release *model.Release
	// Files are the paths the edit changes, sorted.
	Files []string
	// Diff is the edit as a patch.
	Diff string
	// Subject is the commit subject the edit would be committed with.
	Subject string
	// Obsolete is the obsolete stub the Portfile keeps for the port, where
	// it has one: replaced_by the port, fetching nothing, and whether the
	// update moved it too (WithObsolete).
	Obsolete *ObsoleteStub
	// family is the port's Portfile's ports before the edit, as the edit
	// evaluated them, and familyAfter after it.
	family, familyAfter macports.Snapshot
	// Distfiles counts the port's archives whose checksums were written,
	// and Regenerated the dependency blocks written again, a Git crate's
	// archive among them, each with its entries.
	Distfiles   int
	Regenerated []editprep.Regenerated
	// Compared counts the distinct archives a checksum refresh fetched to
	// compare, and ChecksumsLeft what it left as it was, with why.
	Compared      int
	ChecksumsLeft []editprep.ChecksumLeft
	// Differing are the archives whose checksums weren't as the Portfile
	// declared them, for a checksum refresh.
	Differing []string
	// PatchProblems name the port's patches that no longer apply, and
	// PatchesUnchecked those no check reached before the build;
	// PatchesApplied counts those checked that apply.
	PatchProblems    []string
	PatchesUnchecked []string
	PatchesApplied   int
	// Instructions are the Portfile's comments that read as a maintainer's
	// instruction about updating it (portfile.Instructions), each with its
	// line: quoted, never read for intent (the docs dig's I7).
	Instructions []string
	// PatchesDropped are the patches the update took out, with their
	// files, since the new source already holds them; PatchesRefreshed
	// those it re-emitted to apply as written, each with what it did.
	PatchesDropped   []string
	PatchesRefreshed []string
	// Current is true when there was nothing to change.
	Current bool
	// Started is true when the update started its branch
	// (UpdateRequest.Start).
	Started bool
	// Applied is true when the working files were written.
	Applied bool
	// HandEdit is the refusal a hand edit's attempt was written for, in
	// its words (update --hand-edit): the files are the person's to
	// finish, recorded as theirs, not as dockhand's edit.
	HandEdit string
	// Left is what the hand edit's attempt couldn't write, with why.
	Left []editprep.ChecksumLeft
	// Upstream is what comparing the old and new upstream archives found,
	// when the update compared them.
	Upstream *model.UpstreamComparison
	// Others are the port's other open pull requests, where the update
	// looked for them (UpdateRequest.LookForOthers), and OthersProblem why
	// they couldn't be looked for.
	Others        []forge.PullRequestSummary
	OthersProblem string
	// Renamed is the GitHub repository the update's release came from as
	// GitHub names it now, where that's another name than the Portfile's:
	// returntocorp/semgrep answered as semgrep/semgrep, by a redirect
	// discovery follows unsaid (field testing, 2026-10-02). Empty where
	// it's the same, or couldn't be asked.
	Renamed string
	// Followed is the repository github.setup was pointed at, where the
	// update followed a move (FollowMoves).
	Followed string
	// RenamedFrom is the branch's name before the update renamed it for
	// the version it now moves to; empty where it wasn't renamed.
	RenamedFrom string
	// PlainHTTP are the port's URLs over plain HTTP, homepage and
	// master_sites, each with whether its https form answers, since
	// MacPorts prefers HTTPS. They're said, never changed: that's the
	// maintainer's call, and no part of the edit.
	PlainHTTP []PlainURL
	// Stealth is a checksum refresh's stealth update, when it found one.
	Stealth *Stealth
	// DistSubdirRemoved is true when a version update removed the
	// dist_subdir an earlier stealth update set.
	DistSubdirRemoved bool
	// Fixed are the lint findings the edit fixed on the lines it wrote,
	// each in words: whitespace, a swallowing continuation, a lone
	// openmaintainer, and a version update's move from GitHub's tarball.
	Fixed []string
}

// Update edits a port's files in the branch's worktree, as the worktree
// stands, committed or not, and commits nothing: `dockhand check` takes it
// from there. The edit is prepared from a capture of the tracked files, and
// written only if none of the files it touches changed in the meantime.
// Where it doesn't go, it says why, what it kept, and what moves it on
// (refusedUpdate).
func (e *Engine) Update(ctx context.Context, request UpdateRequest) (Update, error) {
	// A port whose holds say moves = "follow" follows a moved repository
	// unattended, its standing answer to the move's hold (Then's item 2).
	if request.Unattended && !request.FollowMoves && e.holdsFor(request.Port).followMoves {
		request.FollowMoves, request.standingFollow = true, true
	}
	update, err := e.updateWithObsolete(ctx, request)
	if err == nil && request.Action == model.EditUpdate && !update.Current && update.Port != "" {
		closed, ok, closedErr := e.ClosedAt(ctx, update.Port, update.After.Version)
		if closedErr != nil {
			return update, closedErr
		}
		if ok {
			update.ClosedBefore = ClosedWords(closed, update.After.Version)
		}
	}
	return update, refusedUpdate(err, request, update)
}

// updateWithObsolete is the update, and its obsolete stub's, where it
// moves that too.
func (e *Engine) updateWithObsolete(ctx context.Context, request UpdateRequest) (Update, error) {
	update, err := e.update(ctx, request)
	// GitHub's rate limit is said as itself, with when it lifts and that
	// nothing changed, not as the request it stopped, "Get …/git/commits/…:
	// GitHub's rate limit …" (the rc6 full stage, D-R4). An update writes
	// its files only once it has its release, so nothing has.
	if limited := new(forge.RateLimitError); errors.As(err, &limited) {
		return update, fmt.Errorf("%w; nothing was changed", limited)
	}
	if err != nil || request.Action != model.EditUpdate || update.Current || update.Port == "" {
		return update, err
	}
	update.Obsolete = obsoleteStub(update)
	if update.Obsolete == nil || update.Obsolete.Version == update.After.Version {
		return update, nil
	}
	// One the edit moved already, as a version line inside the main
	// port's `if {${subport} eq ${name}}` moves with a shared release,
	// has nothing left to update: terraform's did, and updating it again
	// found no version input and said the whole update failed (field
	// testing at a3c43ef7, terraform-1.16 1.16.6). It moved with or
	// without --with-obsolete, and is said so: the plan said it "stays at
	// 1.16.0" where its diff moved it (the v0.3.1-rc1 quick stage).
	if moved, ok := update.familyAfter.Ports[update.Obsolete.Port]; ok && moved.Version == update.After.Version {
		update.Obsolete.Moved = true
		return update, nil
	}
	if !request.WithObsolete {
		return update, nil
	}
	follow := UpdateRequest{Branch: update.Branch, Action: model.EditUpdate, Port: update.Obsolete.Port, Version: update.After.Version,
		Plan: request.Plan, FromMaster: request.FromMaster, subject: update.Subject}
	if request.Plan {
		// A plan changes nothing, so the stub's edit is planned on the
		// files as they are, and said beside the port's.
		follow.Branch = request.Branch
	}
	moved, err := e.update(ctx, follow)
	if err != nil {
		return update, fmt.Errorf("%s moved to %s, but its obsolete %s couldn't: %w", update.Port, update.After.Version, update.Obsolete.Port, err)
	}
	update.Obsolete.Moved = true
	update.Files = append(update.Files, moved.Files...)
	update.Diff += moved.Diff
	return update, nil
}

// ObsoleteStub is a port the Portfile keeps for an old name, replaced_by
// the one updated, fetching nothing, at its version: terraform's, at
// 1.16.0 beside terraform-1.16.
type ObsoleteStub struct {
	Port, Version string
	Moved         bool
}

// obsoleteStub is the updated port's obsolete stub, in the family the
// edit evaluated; nil where it has none.
func obsoleteStub(update Update) *ObsoleteStub {
	for _, name := range slices.Sorted(maps.Keys(update.family.Ports)) {
		port := update.family.Ports[name]
		nothing, err := port.FetchesNothing()
		if name != update.Port && port.Options["replaced_by"] == update.Port && err == nil && nothing {
			return &ObsoleteStub{Port: name, Version: port.Version}
		}
	}
	return nil
}

func (e *Engine) update(ctx context.Context, request UpdateRequest) (Update, error) {
	switch request.Action {
	case model.EditUpdate, model.EditChecksums:
	case model.EditRevbump:
		if strings.TrimSpace(request.Subject) == "" {
			return Update{}, errors.New("a revision bump needs its reason as the subject, such as --subject \"rebuild for poppler 25.09.0\"")
		}
	default:
		return Update{}, fmt.Errorf("engine: %s is not an update", request.Action)
	}
	if !macports.ValidName(request.Port) {
		return Update{}, fmt.Errorf("%q is not a port name", request.Port)
	}
	if request.HandEdit && (request.Unattended || request.Action != model.EditUpdate) {
		return Update{}, errors.New("--hand-edit writes an attempt for a person to finish: a version update, attended, never bump's or serve's")
	}
	if request.Release != nil && (request.Action != model.EditUpdate || request.Release.Version != request.Version) {
		return Update{}, fmt.Errorf("engine: the release found is %s's, for a version update to %q", request.Release.Version, request.Version)
	}
	branch := request.Branch
	worktree, captured, base, err := e.updateSource(ctx, request)
	if err != nil {
		return Update{}, err
	}
	// start starts the branch a Start request asks for, from the master
	// the update was prepared on; startFor is the version that names it.
	startFor := ""
	start := func() error {
		if request.Start == nil {
			return nil
		}
		started := *request.Start
		started.Base = base
		if !slices.Contains(started.Ports, request.Port) {
			started.Ports = append(started.Ports, request.Port)
		}
		// One not yet named is named for the version the update moves
		// to, where it found one (NameFor).
		if started.Name == "" {
			if started.Name, err = e.NameFor(ctx, request.Port, startFor); err != nil {
				return err
			}
		}
		if branch, err = e.Start(ctx, started); err != nil {
			return err
		}
		worktree, err = e.worktree(ctx, branch)
		return err
	}
	// byHand keeps, for an edit dockhand can't make, the branch the
	// person will make it in.
	byHand := func(err error) (Update, error) {
		if request.HandBranch() && errors.Is(err, ErrUnsupported) {
			if startErr := start(); startErr != nil {
				return Update{}, errors.Join(err, startErr)
			}
			return Update{Branch: branch, Base: base, Port: request.Port, Started: true}, err
		}
		return Update{}, err
	}
	preparer, err := e.preparer()
	if err != nil {
		return Update{}, err
	}
	input := editprep.Request{
		EditIntent: request.EditIntent,
		Action:     request.Action,
		Source:     model.Source{Tree: model.ObjectID(captured), Base: model.ObjectID(base)},
		Selection:  macports.Selection{Selector: request.Port},
		Version:    request.Version,
		Subject:    request.Subject,
		HandEdit:   request.HandEdit,
		Attended:   !request.Unattended,
	}
	if request.Action == model.EditChecksums {
		// Which files the branch changed since its base is the branch's to
		// say, and where its Portfile is among them, the port as the base
		// evaluates it: a version edited by hand since makes the refresh no
		// stealth update, while a comment or a homepage edited leaves it
		// one. The editor decides, and makes the stealth update.
		changed, err := changedSinceBase(ctx, worktree, captured, base)
		if err != nil {
			return Update{}, err
		}
		input.Stealth = &editprep.StealthRequest{Changed: changed, KeepRevision: request.KeepRevision,
			Base: e.basePort(ctx, model.Source{Tree: model.ObjectID(captured), Base: base}, base, request.Port, changed)}
	}
	// own is the branch's own pull request, which is no other.
	own := 0
	if branch.PullRequest != nil {
		own = branch.PullRequest.Number
	}
	if request.Action == model.EditUpdate {
		input.Release = request.Release
		release, err := preparer.ResolveRelease(ctx, input)
		if err != nil {
			return byHand(err)
		}
		input.Release = &release
		// A release outdated found already was chosen automatically, and
		// asks for no version of its own: the editor holds the version it
		// edits to the release's request, which update --outdated gave as
		// its version, and refused every port (field testing,
		// 2026-10-02: "a matching resolved release is required").
		input.Version = release.Requested
		if request.Unattended && !release.NoUpdate {
			if major := e.majorHold(request.Port, release.CurrentVersion, release.Version); major != nil {
				return Update{}, &HeldBeforeEdit{Port: request.Port, Held: []model.Concern{*major}}
			}
			if err := e.heldBeforeEdit(ctx, request.Port, own); err != nil {
				return Update{}, err
			}
		}
	}
	compare := request.CompareUpstream && request.Action == model.EditUpdate
	if compare {
		directory, err := scratch.Dir("upstream-")
		if err != nil {
			return Update{}, err
		}
		defer os.RemoveAll(directory)
		input.KeepArchives = directory
	}
	result, err := preparer.Prepare(ctx, input)
	if err != nil {
		return byHand(err)
	}
	failpoint.Hit("update.prepared")
	update := describe(branch, request.Port, result)
	update.Base = base
	followOnly, err := e.followMoves(ctx, request, worktree, captured, &update, &result)
	if err != nil {
		return Update{}, err
	}
	modelined := false
	if !e.KeepModeline {
		modelined = withModelines(&result)
	}
	fixed, reread, err := withLintFixes(ctx, worktree, captured, &result)
	if err != nil {
		return Update{}, err
	}
	update.Fixed = fixed
	if result.TarballMoved {
		update.Fixed = append(update.Fixed, e.tarballMoved(ctx, update.Release, result))
	}
	// A version update or a checksum refresh is where the port's URLs are
	// looked at, so its plain-HTTP ones are said there, beside the edit. A
	// port with nothing to change is said to be current without waiting on
	// its hosts. They're read before finalize, which may drop the
	// evaluation they're read from; what it changes is no URL's scheme.
	if (request.Action == model.EditUpdate || request.Action == model.EditChecksums) && len(result.Files) > 0 {
		if info, ok := result.PortAfter(update.Port); ok {
			update.PlainHTTP = e.plainHTTP(ctx, info, request.answered)
		}
	}
	if err := finalize(ctx, worktree, captured, &result, update.Followed != "" && !followOnly, modelined || len(fixed) > 0, reread); err != nil {
		return Update{}, err
	}
	update.Stealth, update.DistSubdirRemoved = result.Stealth, result.DistSubdirRemoved
	if result.Stealth != nil {
		update.Subject = update.Port + ": update checksums after a stealth update"
	}
	if len(result.Files) > 0 && !followOnly && (compare || result.GoToolchain != nil) {
		trees := [2]model.Source{{Tree: model.ObjectID(captured), Base: model.ObjectID(base)}, {Tree: result.PreparedTree, Base: model.ObjectID(base)}}
		update.Upstream = e.assessUpstream(ctx, result, sourcecompare.Versions{Old: update.Before.Version, New: update.After.Version}, trees, compare)
		// A repository GitHub answers by another name is a finding of the
		// comparison, which holds: bump went on to submit skopeo, and
		// neither its preview nor the pull request said it had moved
		// (field testing's batch 10, finding 1).
		if update.Renamed != "" && update.Upstream != nil {
			moved := movedChange(update.Release.Repository, update.Renamed, update.Followed != "", e.oldOwnerNow(ctx, update.Release.Repository, update.Renamed, update.Followed != ""),
				update.Port, portRename(update.Port, update.Release.Repository, update.Renamed, update.Upstream.Changes))
			// A port's moves = "follow" follows no port rename, which a
			// renamed project shows only once its archives are read: the
			// follow made holds for a look (the person's rule, 2026-10-29).
			if request.standingFollow && update.Followed != "" && moved.Rule == upstreamRenamedPortRule {
				moved.Hold = true
				moved.Message += `; holds.ports."` + update.Port + `".moves = "follow" followed it, which a port rename isn't`
			}
			update.Upstream.Changes = append(update.Upstream.Changes, moved)
		}
		// An archive MacPorts' mirror already holds another file of, under
		// a dist_subdir other ports share, fails the check at its checksum
		// where MacPorts fetches the mirror's first: gopls's, under go/
		// (field testing at f86cec28). It holds, with the fix.
		if update.Upstream != nil {
			for _, conflict := range result.MirrorConflicts {
				update.Upstream.Changes = append(update.Upstream.Changes, model.UpstreamChange{Kind: "mirror", Rule: mirrorConflictRule, Subject: conflict.Name, Hold: true, Class: model.Introduced,
					Message: mirrorConflictWords(conflict)})
			}
		}
	}
	// A block regenerated under a hook that edits its manifest, whose
	// effect on it couldn't be checked, holds for a look with what the
	// comparison found, where it refused (R3-20).
	if result.ManifestHook != "" && !followOnly {
		if update.Upstream == nil {
			update.Upstream = &model.UpstreamComparison{}
		}
		update.Upstream.Changes = append(update.Upstream.Changes, model.UpstreamChange{Kind: "manifest", Rule: manifestHookRule, Hold: true, Class: model.Introduced, Message: result.ManifestHook})
	}
	if request.Action == model.EditChecksums {
		update.ChecksumsLeft, update.Differing = result.Left, result.Differing
		for _, conflict := range result.MirrorConflicts {
			update.ChecksumsLeft = append(update.ChecksumsLeft, editprep.ChecksumLeft{Kind: "mirror", What: conflict.Name, Reason: mirrorConflictWords(conflict)})
		}
		names := map[string]bool{}
		for _, download := range result.Downloads {
			names[download.Name] = true
		}
		update.Compared = len(names)
	}
	if len(result.Files) == 0 {
		update.Current = true
		return update, nil
	}
	// An unattended update looked before its edit, and found none.
	if request.LookForOthers && request.Action == model.EditUpdate && !request.Unattended {
		update.Others, update.OthersProblem = e.openPullRequests(ctx, []string{update.Port}, own)
	}
	diff, err := worktree.DiffTrees(ctx, captured, string(result.PreparedTree))
	if err != nil {
		return Update{}, err
	}
	update.Diff = string(diff)
	if request.Plan {
		return update, nil
	}
	if request.Start != nil {
		if request.Action == model.EditUpdate {
			startFor = update.After.Version
		}
		if err := start(); err != nil {
			return update, err
		}
		update.Branch, update.Started = branch, true
	}

	if err := expandFor(ctx, worktree, update.Files); err != nil {
		return Update{}, err
	}
	if err := worktree.ApplyToWorkingFiles(ctx, result.Files); err != nil {
		if errors.Is(err, git.ErrWorkingFile) {
			return Update{}, fmt.Errorf("%w; nothing was written, so run it again", err)
		}
		return Update{}, err
	}
	update.Applied = true
	if request.Start == nil && request.Action == model.EditUpdate {
		renamed, err := e.renameForVersion(ctx, &branch, request.Port, update.Before.Version, update.After.Version)
		switch {
		case err != nil:
			progress.VerboseReport(ctx, "%s keeps its name: %v", branch.ShortName(), err)
		case renamed != "":
			update.RenamedFrom, update.Branch = renamed, branch
		}
	}
	// A hand edit's attempt is the person's from here: no edit of
	// dockhand's is recorded, and the refusal it answers is, for status,
	// the hold, and the pull request to say (Then's item 3).
	if result.Refused != nil {
		return e.recordHandEdit(ctx, branch, update, result)
	}
	edit, err := e.editRecord(ctx, worktree, branch, request, update, result)
	if err != nil {
		return update, err
	}
	change := "refreshed checksums"
	switch request.Action {
	case model.EditUpdate:
		change = fmt.Sprintf("%s → %s", update.Before, update.After)
	case model.EditRevbump:
		change = fmt.Sprintf("revision %d → %d", update.Before.Revision, update.After.Revision)
	}
	applied, primed, record, err := e.editFindings(ctx, worktree, branch, base, captured, edit, update, result, compare)
	if err != nil {
		return update, err
	}
	update.Instructions = instructionLines(worktree.Root, edit.Directory, update.Port)
	// The files are written; a record that landed though its commit's
	// outcome is unknown stands, where saying the update failed would
	// leave the person to edit again what's there (store.Recorded).
	err = store.Recorded(ctx, e.Store, e.Repository, func(tx store.Tx) error {
		if err := tx.AddEdit(edit); err != nil {
			return err
		}
		// The tickets it closes join the branch's, each once.
		if len(request.Closes) > 0 {
			current, err := tx.Branch(branch.ID)
			if err != nil {
				return err
			}
			for _, ticket := range request.Closes {
				if !slices.Contains(current.Closes, ticket) {
					current.Closes = append(current.Closes, ticket)
				}
			}
			if err := tx.UpdateBranch(current); err != nil {
				return err
			}
		}
		if primed != nil {
			if err := tx.RecordAssessment(*primed); err != nil {
				return err
			}
		}
		if record != nil {
			if err := tx.RecordChange(*record); err != nil {
				return err
			}
		}
		_, err := tx.AppendEvent(model.Event{At: edit.At, Branch: branch.ID, Kind: model.EventBranchEdit, Level: model.LevelInfo,
			Message: fmt.Sprintf("%s: %s (%s)", update.Port, change, listPaths(update.Files))})
		return err
	}, editRecorded(branch.ID, edit.ID))
	if err != nil || record != nil {
		return update, err
	}
	// The record is what the files say, made again whenever it's read
	// for; one this edit couldn't make is no failure of the edit.
	if _, err := e.revisionChanges(ctx, branch.ID, base, model.ObjectID(applied), true); err != nil {
		progress.VerboseReport(ctx, "%s: what this revision changes wasn't recorded: %v", edit.Directory, err)
	}
	return update, nil
}

// followMoves notes a GitHub repository that answers by another name, and
// where --follow-moves asks, follows it in the Portfile the update
// edits. A port already at its release, on a branch an update moved
// before --follow-moves was asked, still follows the move: duckdb's
// branch at 2.0.0 kept github.setup cwida duckdb, and the command changed
// nothing (field testing, 2026-10-21). That edit is the move alone, and
// it says so: it compares no archives, since the version's are those
// compared already.
func (e *Engine) followMoves(ctx context.Context, request UpdateRequest, worktree *git.Repository, captured string, update *Update, result *editprep.Result) (followOnly bool, err error) {
	if request.Action != model.EditUpdate || update.Release == nil {
		return false, nil
	}
	if len(result.Files) > 0 {
		update.Renamed = e.renamed(ctx, *update.Release)
		if update.Renamed == "" || !request.FollowMoves || request.standingFollow && portRename(update.Port, update.Release.Repository, update.Renamed, nil) != "" {
			return false, nil
		}
		for i, file := range result.Files {
			if path.Base(file.Path) != "Portfile" || file.Delete {
				continue
			}
			moved, n, err := portfile.MoveGitHub(file.After, update.Release.Repository, update.Renamed)
			if err != nil {
				return false, fmt.Errorf("%s: --follow-moves: %w", request.Port, err)
			}
			if n > 0 {
				// The tree is made again once every file is final
				// (finalize).
				result.Files[i].After, update.Followed = moved, update.Renamed
			}
		}
		return false, nil
	}
	if !request.FollowMoves || result.Target.Portfile == "" {
		return false, nil
	}
	renamed := e.renamed(ctx, *update.Release)
	if renamed == "" || request.standingFollow && portRename(update.Port, update.Release.Repository, renamed, nil) != "" {
		return false, nil
	}
	file, text, err := worktree.File(ctx, captured, result.Target.Portfile)
	if err != nil {
		return false, err
	}
	moved, n, err := portfile.MoveGitHub(text, update.Release.Repository, renamed)
	if err != nil {
		return false, fmt.Errorf("%s: --follow-moves: %w", request.Port, err)
	}
	if !file.Exists || n == 0 {
		return false, nil
	}
	result.Files = []git.FileEdit{{Path: result.Target.Portfile, Before: file, After: moved, Mode: file.Mode}}
	prepared, err := worktree.EditTree(ctx, captured, result.Files)
	if err != nil {
		return false, err
	}
	result.PreparedTree = model.ObjectID(prepared)
	update.Renamed, update.Followed = renamed, renamed
	update.Files = []string{result.Target.Portfile}
	update.Subject = update.Port + ": follow the move to " + renamed
	return true, nil
}

// editFindings are what the edit's own work says of the files it leaves,
// where the branch was as its base before the edit, as a new one is: its
// comparison their assessment, and its evaluations their change record.
// Elsewhere both are made from the base when they're read for. applied is
// the tree of the files the edit leaves.
func (e *Engine) editFindings(ctx context.Context, worktree *git.Repository, branch model.Branch, base model.ObjectID, captured string, edit model.Edit, update Update, result editprep.Result, compare bool) (applied string, primed *model.Assessment, record *model.ChangeRecord, err error) {
	// Where the branch was as its base before the edit, as a new one is,
	// the update's own comparison is the assessment of the files it
	// leaves: its before is the base and its after the revision, and it
	// compared them in every context that fetches them (the assessment
	// design, D), or couldn't, which it says. A Git-fetched port's update
	// compared no archives, and records none, so its assessment is made
	// when it's collected.
	trees, err := worktree.CommitTrees(ctx, []string{string(base)})
	if err != nil {
		return "", nil, nil, err
	}
	atBase := trees[string(base)] == captured
	_, applied, err = worktree.WorkingTree(ctx)
	if err != nil {
		return "", nil, nil, err
	}
	if after, _ := result.PortAfter(update.Port); compare && update.Upstream != nil && !after.GitFetched() && atBase {
		primed = &model.Assessment{Branch: branch.ID, Tree: model.ObjectID(applied), Base: base, Port: update.Port, Directory: edit.Directory,
			Comparison: *update.Upstream, Policy: assess.Policy, At: edit.At}
	}
	// Where the branch was as its base, the edit's own evaluations, before
	// and after, are the change record of the files it leaves, as its
	// comparison is their assessment; elsewhere update makes the record
	// from the base, once the edit is recorded.
	if len(result.Fidelity) > 0 && atBase && (branch.PullRequest == nil || !branch.PullRequest.Adopted) {
		before, after := result.Fidelity[0].Before, result.Fidelity[len(result.Fidelity)-1].After
		// The same record the family evaluation makes, through the same
		// producer, the Portfile's texts with it, so what the edit changed
		// in a block this Mac doesn't evaluate is said as a record made
		// from the base says it (batch 76; the architecture re-synthesis,
		// L2c).
		name := edit.Directory + "/Portfile"
		baseFile, baseText, baseErr := worktree.File(ctx, trees[string(base)], name)
		appliedFile, appliedText, appliedErr := worktree.File(ctx, applied, name)
		if before.Platform == after.Platform && baseErr == nil && appliedErr == nil {
			if !baseFile.Exists {
				baseText = nil
			}
			if !appliedFile.Exists {
				appliedText = nil
			}
			made := changeRecord(model.ChangeRecord{Branch: branch.ID, Tree: model.ObjectID(applied), Base: base, Directory: edit.Directory, At: edit.At},
				[2]*macports.Snapshot{&before, &after}, [2][]byte{baseText, appliedText}, "")
			record = &made
		}
	}
	return applied, primed, record, nil
}

// updateSource is what an update is prepared from: the branch's working
// files on its base, or for a plan from master, master's tree as fetched
// now, read through the clone, since a plan only reads.
func (e *Engine) updateSource(ctx context.Context, request UpdateRequest) (*git.Repository, string, model.ObjectID, error) {
	if request.FromMaster || request.Start != nil {
		// A checksum refresh's plan and a rebuild's read master too, as a
		// version update's does (the command UX review's §9: --plan with
		// no branch planned on master for update alone); only a version
		// update starts its branch here.
		planned := request.FromMaster && (request.Action == model.EditChecksums || request.Action == model.EditRevbump)
		if request.FromMaster && !request.Plan || request.Action != model.EditUpdate && !planned {
			return nil, "", "", errors.New("engine: only a version update is prepared from master; start a branch for anything else")
		}
		master, err := e.fetchMaster(ctx)
		if err != nil {
			return nil, "", "", err
		}
		trees, err := e.Repo.CommitTrees(ctx, []string{string(master)})
		if err != nil {
			return nil, "", "", err
		}
		return e.Repo, trees[string(master)], master, nil
	}
	worktree, err := e.worktree(ctx, request.Branch)
	if err != nil {
		return nil, "", "", err
	}
	_, captured, err := worktree.WorkingTree(ctx)
	if err != nil {
		return nil, "", "", fmt.Errorf("reading %s's working files: %w", request.Branch.Worktree, err)
	}
	return worktree, captured, request.Branch.Base, nil
}

// editRecord is what tidy later reads: each file's blob before and after,
// and the subject the edit carries.
func (e *Engine) editRecord(ctx context.Context, worktree *git.Repository, branch model.Branch, request UpdateRequest, update Update, result editprep.Result) (model.Edit, error) {
	subject := update.Subject
	if request.subject != "" {
		subject = request.subject
	}
	edit := model.Edit{ID: model.EditID(store.NewID("ed")), Branch: branch.ID, Kind: request.Action, Port: update.Port, Subject: subject, At: e.now(),
		Upstream: update.Upstream, Release: update.Release, Excepted: request.Excepted}
	edit.Directory = portDirectory(result.Files[0].Path)
	if result.Target.Portfile != "" {
		edit.Directory = path.Dir(result.Target.Portfile)
	}
	if edit.Subject == "" {
		switch edit.Kind {
		case model.EditChecksums:
			edit.Subject = update.Port + ": update checksums"
		case model.EditRevbump:
			edit.Subject = update.Port + ": " + strings.TrimSpace(request.Subject)
		default:
			edit.Subject = update.Port + ": update to " + update.After.Version
		}
	}
	for _, file := range result.Files {
		recorded := model.EditedFile{Path: file.Path, Before: model.ObjectID(file.Before.Blob)}
		if !file.Delete {
			// Written, not only hashed: tidy reads it back to commit
			// this edit alone, one of several updates in its directory
			// (updateSteps).
			blob, err := worktree.WriteBlob(ctx, file.After)
			if err != nil {
				return model.Edit{}, err
			}
			recorded.After = model.ObjectID(blob)
		}
		edit.Files = append(edit.Files, recorded)
	}
	return edit, nil
}

// portDirectory is the <category>/<port> a path is under, or its directory
// when it is not in one.
func portDirectory(file string) string {
	if directory, ok := macports.PortDirectoryOf(file); ok {
		return directory
	}
	return path.Dir(file)
}

// basePort is a port as the branch's base evaluates it, where the branch
// changed its Portfile since; nil where it didn't, where the base has no
// such port, or where it couldn't be evaluated, which a stealth update
// takes as a new version.
func (e *Engine) basePort(ctx context.Context, source model.Source, base model.ObjectID, name string, changed []string) *macports.PortInfo {
	reader, err := e.portReader()
	if err != nil {
		return nil
	}
	directory, err := reader.Directory(ctx, source, name)
	if err != nil || !slices.Contains(changed, directory+"/Portfile") {
		return nil
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(base)})
	if err != nil {
		return nil
	}
	ports, err := reader.Ports(ctx, model.Source{Commit: base, Tree: model.ObjectID(trees[string(base)]), Base: base}, directory, model.Environment{}, nil)
	if err != nil {
		return nil
	}
	for _, port := range ports {
		if port.Name == name {
			return &port
		}
	}
	return nil
}

// changedSinceBase are the files a branch's captured tree has changed since
// its base.
func changedSinceBase(ctx context.Context, worktree *git.Repository, captured string, base model.ObjectID) ([]string, error) {
	trees, err := worktree.CommitTrees(ctx, []string{string(base)})
	if err != nil {
		return nil, err
	}
	return worktree.ChangedPaths(ctx, trees[string(base)], captured)
}

// describe reads what the preparation found.
func describe(branch model.Branch, selector string, result editprep.Result) Update {
	update := Update{Branch: branch, Port: result.Target.Name, Release: result.Release, PatchProblems: result.PatchProblems(), PatchesUnchecked: result.UncheckedPatches(), PatchesApplied: result.PatchesApplied(),
		Distfiles: len(result.Downloads), Regenerated: result.Regenerated, PatchesDropped: result.Dropped}
	for _, refreshed := range result.Refreshed {
		update.PatchesRefreshed = append(update.PatchesRefreshed, refreshed.Name+": "+refreshed.Words)
	}
	if update.Port == "" {
		update.Port = selector
	}
	if before, ok := result.PortBefore(update.Port); ok {
		update.Before = PortVersion{Version: before.Version, Revision: before.Revision}
	}
	if after, ok := result.PortAfter(update.Port); ok {
		update.After = PortVersion{Version: after.Version, Revision: after.Revision}
	}
	if len(result.Fidelity) > 0 {
		update.family = result.Fidelity[0].Before
		update.familyAfter = result.Fidelity[len(result.Fidelity)-1].After
	}
	update.CrossesMajor = update.Before.Version != "" && version.CrossesMajor(update.Before.Version, update.After.Version)
	update.LeavesStable = update.Before.Version != "" && version.LeavesStable(update.Before.Version, update.After.Version)
	for _, file := range result.Files {
		update.Files = append(update.Files, file.Path)
	}
	slices.Sort(update.Files)
	if len(result.Commits) > 0 {
		update.Subject = result.Commits[0].Subject
	}
	return update
}

// expandFor adds the directories of the ports the files belong to to a
// sparse worktree's cone, so the edited files are there to write.
func expandFor(ctx context.Context, worktree *git.Repository, files []string) error {
	cone, err := worktree.SparseCone(ctx)
	if err != nil || len(cone) == 0 {
		return err
	}
	var missing []string
	for _, file := range files {
		directory := portDirectory(file)
		if directory == "." || slices.Contains(missing, directory) {
			continue
		}
		covered := slices.ContainsFunc(cone, func(c string) bool { return directory == c || strings.HasPrefix(directory, c+"/") })
		if !covered {
			missing = append(missing, directory)
		}
	}
	return worktree.ExpandSparse(ctx, missing...)
}

// assessUpstream assesses what upstream's change means for the port an
// update prepared, as the collector compares it (comparison.Collector's
// Update), with the candidate's Portfile as the update left it. Nil where
// nothing was compared or said.
func (e *Engine) assessUpstream(ctx context.Context, result editprep.Result, versions sourcecompare.Versions, trees [2]model.Source, compare bool) *model.UpstreamComparison {
	planner, _ := e.archivePlanner()
	var portfile []byte
	if file, data, err := e.Repo.File(ctx, string(trees[1].Tree), result.Target.Portfile); err == nil && file.Exists {
		portfile = data
	}
	comparison := e.collector(planner).Update(ctx, result, versions, trees, compare, portfile)
	if !compare && len(comparison.Changes) == 0 {
		return nil
	}
	return &comparison
}

// editRecorded witnesses an edit's record (store.Recorded).
func editRecorded(branch model.BranchID, id model.EditID) func(store.Reader) bool {
	return func(r store.Reader) bool {
		edits, err := r.Edits(branch)
		return err == nil && slices.ContainsFunc(edits, func(edit model.Edit) bool { return edit.ID == id })
	}
}

// renamed is the release's GitHub repository's name now, where GitHub
// names it otherwise than the release was found by; empty where it's the
// same, it isn't GitHub's, or GitHub couldn't be asked, which is no
// reason to stop an update.
// movedChange is the comparison's finding that GitHub answers the
// release's repository by another name. A new owner is said as a transfer,
// not a rename, since a new owner's release is a person's to trust, more
// so for what a port installs with privilege (sudosh2's, transferred from
// squash to WLTBAgent); either holds until --follow-moves points
// github.setup at it, the person's choice (field testing at 43949b89:
// gitoxide's, Byron to GitoxideLabs, was followed by hand).
// movedChange is a repository move as a finding; owner says what the old
// owner name names now, where it's another account's or no one's
// (oldOwnerNow). A rename of the project a port is named after is a port
// rename, renamed the port's new name, its line leading with dockhand
// rename, where any other rename's leads with --follow-moves and names
// rename after it (the person's rule, 2026-10-29).
func movedChange(from, to string, followed bool, owner, port, renamed string) model.UpstreamChange {
	change := model.UpstreamChange{Kind: "moved", Rule: upstreamMovedRule, Subject: to, Hold: !followed, Class: model.Introduced}
	fromOwner, _, _ := strings.Cut(from, "/")
	toOwner, toName, _ := strings.Cut(to, "/")
	if renamed != "" {
		change.Rule = upstreamRenamedPortRule
	}
	switch {
	case followed && renamed != "":
		change.Message = fmt.Sprintf("upstream renamed: GitHub redirects %s to %s, and github.setup now names it (--follow-moves); the port keeps its name, %s, which dockhand rename %s %s changes", from, to, port, port, renamed)
	case followed:
		change.Message = fmt.Sprintf("upstream moved: GitHub redirects %s to %s, and github.setup now names it (--follow-moves)", from, to)
	case renamed != "":
		change.Message = fmt.Sprintf("upstream renamed: GitHub redirects %s to %s, and the port is named after it. Use dockhand rename %s %s to rename the port; update --follow-moves keeps its name, %s, and updates github.setup", from, to, port, renamed, port)
	case !strings.EqualFold(fromOwner, toOwner):
		change.Rule = upstreamTransferredRule
		change.Message = fmt.Sprintf("upstream moved from %s to %s: GitHub redirects %s to %s. Review the new owner before trusting its releases. Use update --follow-moves to update github.setup", fromOwner, toOwner, from, to)
		if owner != "" {
			change.Message += ". " + owner
		}
	default:
		change.Message = fmt.Sprintf("upstream renamed: GitHub redirects %s to %s. Use update --follow-moves to update github.setup, or dockhand rename %s %s to rename the port with it", from, to, port, macports.RenamedAfter(port, toName))
	}
	return change
}

// portRename is a port's new name where its project's rename under the
// same owner is the port's: the port named after the repository's old
// name, or the project's own name changed with it, as its archives read
// (assess.ProjectRenamed), which is what makes goplus's, its repository
// gop, one. Empty where it isn't one, an owner's transfer included.
func portRename(port, from, to string, changes []model.UpstreamChange) string {
	fromOwner, fromName, _ := strings.Cut(from, "/")
	toOwner, toName, _ := strings.Cut(to, "/")
	if !strings.EqualFold(fromOwner, toOwner) {
		return ""
	}
	if macports.NamedAfter(port, fromName) || slices.ContainsFunc(changes, func(change model.UpstreamChange) bool { return change.Rule == assess.ProjectRenamed }) {
		return macports.RenamedAfter(port, toName)
	}
	return ""
}

// upstreamRenamedPortRule is movedChange's finding of a port rename.
const upstreamRenamedPortRule = "upstream-renamed-port"

// oldOwnerNow says what a moved repository's old owner name names now,
// where the owner changed and the move isn't followed: a redirect lasts
// only while the old name has no repository of that name, so github.setup
// naming it is a supply-chain risk once someone else holds the name, as
// yabai's koekeishiya now names a new, unrelated account (field testing's
// outdated-ports session). One request reads the account (GET
// /users/{old}), and the repository's own creation date tells a newer
// account apart. Empty where the name is an older account's, or nothing
// could be read.
func (e *Engine) oldOwnerNow(ctx context.Context, from, to string, followed bool) string {
	fromOwner, _, _ := strings.Cut(from, "/")
	toOwner, _, _ := strings.Cut(to, "/")
	if followed || strings.EqualFold(fromOwner, toOwner) {
		return ""
	}
	account, err := e.forge().Account(ctx, "https://github.com", fromOwner)
	if err != nil {
		return ""
	}
	if !account.Exists {
		return fmt.Sprintf("The old name, %s, is free on GitHub for anyone to take, who could publish a repository named %s in this one's place, so --follow-moves matters", fromOwner, from)
	}
	repository, err := e.forge().Describe(ctx, "https://github.com", to)
	if err != nil || repository.CreatedAt.IsZero() || !account.CreatedAt.After(repository.CreatedAt) {
		return ""
	}
	return fmt.Sprintf("The old name, %s, now belongs to another account, created %s, which could publish a repository named %s in this one's place, so --follow-moves matters", fromOwner, account.CreatedAt.Format("2006-01-02"), from)
}

// upstreamTransferredRule is movedChange's finding of a new owner.
const upstreamTransferredRule = "upstream-transferred"

// upstreamMovedRule is the comparison's finding that GitHub answers the
// release's repository by another name.
const upstreamMovedRule = "upstream-moved"

// mirrorConflictRule is the finding that MacPorts' mirror already holds
// another file of an archive's name under a shared dist_subdir.
const mirrorConflictRule = "mirror-conflict"

// manifestHookRule is the finding that a dependency block was regenerated
// under a hook that edits the manifest it's generated from, where whether
// the hook shapes it couldn't be checked (R3-20).
const manifestHookRule = "manifest-hook"

// mirrorConflictWords says a mirror conflict and its fix.
func mirrorConflictWords(conflict editprep.MirrorConflict) string {
	return fmt.Sprintf("The MacPorts mirror already has a different %s under %s/. MacPorts may fetch that file first, causing a checksum failure. Set dist_subdir to ${name} to keep this port's files separate", conflict.Name, conflict.Subdir)
}

func (e *Engine) renamed(ctx context.Context, release model.Release) string {
	if release.Forge != forge.GitHub || release.Repository == "" {
		return ""
	}
	description, err := e.forge().Describe(ctx, "https://github.com", release.Repository)
	if err != nil || description.Name == "" || strings.EqualFold(description.Name, release.Repository) {
		return ""
	}
	return description.Name
}

// withModelines gives each Portfile an edit writes MacPorts' modeline as
// its first line, where it lacks one or spells its settings otherwise
// (portfile.WithModeline, the person's ask, 2026-10-24), saying whether
// it changed one. Line 1 is a comment, which nothing evaluates; the tree
// is made once every file is final (finalize).
func withModelines(result *editprep.Result) bool {
	changed := false
	for i, file := range result.Files {
		if file.Delete || path.Base(file.Path) != "Portfile" {
			continue
		}
		if after, ok := portfile.WithModeline(file.After); ok {
			result.Files[i].After, changed = after, true
		}
	}
	return changed
}

// withLintFixes fixes, on the lines each Portfile an edit writes adds or
// alters, the lint findings dockhand fixes itself (portfile.FixTouched,
// the person's "we should have Dockhand do those", 2026-10-27): the
// fixes, in words, each with its file, and whether one changes what the
// Portfile evaluates to, as each fix says of itself (LintFix.Evaluates).
// The tree is made once every file is final (finalize).
func withLintFixes(ctx context.Context, worktree *git.Repository, captured string, result *editprep.Result) (fixed []string, evaluates bool, err error) {
	for i, file := range result.Files {
		if file.Delete || path.Base(file.Path) != "Portfile" {
			continue
		}
		_, before, err := worktree.File(ctx, captured, file.Path)
		if err != nil {
			return nil, false, err
		}
		after, fixes := portfile.FixTouched(before, file.After)
		for _, fix := range fixes {
			fixed = append(fixed, file.Path+": "+fix.Words())
			evaluates = evaluates || fix.Evaluates
		}
		result.Files[i].After = after
	}
	return fixed, evaluates, nil
}

// finalize makes the prepared tree once from the edit's files as every
// transformation after preparation left them: a moved repository
// followed, the modelines, and the lint fixes. Each made the tree again
// only where it changed something, so --follow-moves in a clean Portfile
// previewed and compared the old repository and wrote the new (Codex's
// review of 2ad896d2, W5). changed says a file changed since the tree was
// made; reread that what the files evaluate to may have, so the prepared
// evaluation and the fidelity reports, which describe the files before,
// are dropped, and the change record is made from the files as written.
// A modeline, a comment, and whitespace change nothing evaluated.
func finalize(ctx context.Context, worktree *git.Repository, captured string, result *editprep.Result, followed, changed, reread bool) error {
	if !followed && !changed {
		return nil
	}
	prepared, err := worktree.EditTree(ctx, captured, result.Files)
	if err != nil {
		return err
	}
	result.PreparedTree = model.ObjectID(prepared)
	if followed || reread {
		result.Fidelity, result.Prepared = nil, macports.Snapshot{}
	}
	return nil
}

// tarballMoved says a version update's move from GitHub's tarball to its
// archive, and where the release carries the archive the github
// PortGroup's releases fetch names, as create reads it
// (portcreate.HasReleaseArchive), that github.tarball_from releases
// fetches it: MacPorts prefers a release's own (the review survey's §4).
func (e *Engine) tarballMoved(ctx context.Context, release *model.Release, result editprep.Result) string {
	words := "github.tarball_from tarball is now archive, GitHub's archive of the tag, with the checksums the update made"
	info, ok := result.PortAfter(result.Target.Name)
	if !ok || release == nil || release.Forge != forge.GitHub || release.Tag == "" {
		return words
	}
	repository, err := e.forge().Repository("https://github.com", release.Repository)
	if err != nil {
		return words
	}
	assets, ok := repository.(forge.AssetRepository)
	if !ok {
		return words
	}
	names, err := assets.Assets(ctx, release.Tag)
	project, version := info.Options["github.project"], info.Options["github.version"]
	if err != nil || !portcreate.HasReleaseArchive(names, project, version) {
		return words
	}
	return fmt.Sprintf("%s; release %s carries its own, %s-%s.tar.gz, which github.tarball_from releases fetches", words, release.Tag, project, version)
}

// recordHandEdit records a hand edit's attempt as the person's answer to
// the refusal it was written for (model.FindingHandEdit): the port's
// directory, the attempt's digest, and the refusal's words.
func (e *Engine) recordHandEdit(ctx context.Context, branch model.Branch, update Update, result editprep.Result) (Update, error) {
	// The attempt proves no evaluation, so its versions are the release's.
	if release := update.Release; release != nil {
		update.Before.Version = cmp.Or(update.Before.Version, release.CurrentVersion)
		update.After.Version = cmp.Or(update.After.Version, release.Version)
	}
	// An attempt that fetched no archive compared none, which isn't said
	// as a port that fetches nothing.
	if update.Upstream != nil && len(result.Downloads) == 0 && NothingCompared(*update.Upstream) {
		update.Upstream = nil
	}
	update.HandEdit = fmt.Sprintf("%s to %s: %s", update.Port, update.After.Version, refusal.WordsOf(result.Refused))
	update.Left = result.Left
	directory := path.Dir(result.Target.Portfile)
	decision := model.Decision{Branch: branch.ID, Finding: model.FindingHandEdit, Subject: directory, Evidence: attemptDigest(result.Files), Answer: model.ByHand, Words: update.HandEdit, At: e.now()}
	err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		if err := tx.AddDecision(decision); err != nil {
			return err
		}
		_, err := tx.AppendEvent(model.Event{At: decision.At, Branch: branch.ID, Kind: model.EventBranchEdit, Level: model.LevelInfo,
			Message: fmt.Sprintf("%s: dockhand's attempt written for a hand edit, %s (%s)", update.Port, update.HandEdit, listPaths(update.Files))})
		return err
	})
	return update, err
}

// attemptDigest is what a hand edit's attempt is known by: its files'
// paths and contents, as written.
func attemptDigest(files []git.FileEdit) string {
	sum := sha256.New()
	for _, file := range files {
		fmt.Fprintf(sum, "%s\x00%d\x00", file.Path, len(file.After))
		sum.Write(file.After)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// instructionLines are a port's Portfile's comments that read as a
// maintainer's instruction, as update, status, and serve quote them:
// "textproc/jq/Portfile:4: Do not update past 3.8.3".
func instructionLines(root, directory, port string) []string {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(directory), "Portfile"))
	if err != nil {
		return nil
	}
	var lines []string
	for _, found := range portfile.Instructions(data, port) {
		lines = append(lines, fmt.Sprintf("%s/Portfile:%d: %s", directory, found.Line, found.Text))
	}
	return lines
}
