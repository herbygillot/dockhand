// Package staging turns the identity an attempt carries into a tree on
// disk that a build can be served from.
//
// IT LIVED IN THE COMPOSITION ROOT, and that is why this package exists.
// run declares the Stager interface because run needs it; the body needs
// a git repository, a temporary root and a MacPorts evaluator at once,
// which is why it was implemented beside the wiring that holds those
// three. But nothing here is wiring — materializing a commit, reading a
// Portfile's per-platform options, staging a baseline — and logic in a
// composition root has no natural test home: the tests instantiate the
// application, not the root.
//
// IT IS NOT UNDER internal/run, and the linter is what settled that.
// run's depguard list forbids git and tempdir, and a subpackage inherits
// the list. Holding a repository and a temporary root is precisely this
// package's job, so it belongs beside run rather than inside it.
//
// The reason is not ref-moving, though the rule said so until this move
// went looking: R23's one-ref-mover property is enforced tree-wide by
// statestore's AST check, and an import line never secured it. What the
// ban buys is that run's DURABLE QUEUE carries an identity and a
// question and never a filesystem path — a path written in October does
// not exist in November, and a dispatcher draining another process's
// attempt cannot reach that process's temporary root. Turning an
// identity into a staged tree at the moment of starting is what makes a
// person's bump, a pass's drain and a later verify one road.
//
// Two defects lived here undisturbed while it did. Baseline never
// materialized the tree's _resources, so no ABI baseline was ever
// captured, for any port, on any run; and preflight evaluated every
// Portfile at os.major 0 whenever no release had been resolved, which
// recorded whole cohorts "unsupported" without a guest ever building
// one. Both survived a green suite.
package staging

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/build"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/run"
	"github.com/herbygillot/dockhand/internal/tempdir"
)

// ErrNoEvaluator is a stager built without a session.
//
// It reaches a caller as Preflight.Err and never as a decline: a
// preflight that could not be taken has learned nothing about the port,
// and run.Plan schedules an unread member as an ordinary build. A
// wiring fault must not read as "this port declares known_fail".
var ErrNoEvaluator = errors.New("staging: no evaluator was wired for the preflight")

// Session opens a short-lived evaluator. It is a function rather than an
// evaluator because the preflight asks what a portdir declares under the
// platform it will be BUILT on, and the run's own evaluator carries the
// host's frame.
type Session func(ctx context.Context, opts ...eval.Option) (*eval.Evaluator, error)

// New builds a stager over the three things the work needs and the
// composition root holds.
func New(repo *git.Repo, temp tempdir.Root, session Session) *Stager {
	return &Stager{repo: repo, temp: temp, session: session}
}

// Stager materializes the portdirs an attempt will build from the
// commit the attempt names, and reads their preflight. IT IS THE
// RE-PLAN THE DRAIN RULING CHOSE, and it has had no implementation
// since step 7 by design: the interface was declared where it is
// consumed and the body waited for the composition root that could
// hold a repository, a temporary root and an evaluator at once.
//
// THE QUEUE CARRIES AN IDENTITY AND A QUESTION AND NEVER A FILESYSTEM
// PATH. record.Attempt names a Sha and record.Change names its
// Subjects; nothing durable names a directory, because no two processes
// would ever agree on one and a path written in October is a path that
// does not exist in November. So Start asks this value to turn the
// identity into a staged tree at the moment of starting — on a person's
// bump, on a pass's drain and on a later verify alike — which is what
// makes those three roads one road.
//
// THE MATERIALIZATION IS FROM THE OBJECT DATABASE, never from the
// working tree: git.Repo.Materialize is `git archive <rev> -- <path>`,
// so a dirty checkout, a branch somebody switched away from and a
// commit no branch names all stage identically. That is what lets a
// branchless snapshot — a working-tree adoption pinned to a commit —
// be staged by exactly this call rather than by a special case that
// could not queue.
//
// THE OVERLAY IS A TREE AND NOT A BAG OF PORTDIRS. MacPorts reads
// _resources out of the tree a port came from, and for archive sites it
// does so with NO FALLBACK: a staging directory holding only the
// changed portdirs would serve the guest a tree with no archive sites
// configured, and the baseline half of an ABI comparison would be lost
// rather than reported missing. So build.ResourcesDir is materialized
// beside the members, from the same commit.
type Stager struct {
	repo *git.Repo
	temp tempdir.Root
	// session opens a SHORT-LIVED evaluator framed on the TARGET
	// release, which is why it is a function and not the run's own
	// evaluator. The preflight asks what a portdir declares under the
	// platform it will be built on — known_fail and use_xcode are
	// per-platform options — and the run's evaluator carries the host's
	// frame. A draft that reused it would have answered the host's
	// question about a guest's Portfile.
	session Session
	// keep collects the per-stage cleanups, so a pass that stages forty
	// attempts can drop forty temporary trees at the end of the pass
	// rather than at the end of the process. See Cleanup.
	keep []func()
}

// Stage materializes one attempt's members and hands back the roster
// with their staged locations and what the preflight read.
//
// A MEMBER WHOSE PREFLIGHT COULD NOT BE READ IS NOT A DECLINE, and that
// is why Preflight.Read exists: run.Plan schedules an unread member as
// an ordinary build, because a machine that could not ask has learned
// nothing about the port, where a KnownFail == false read as an answer
// would spend a VM and come back FAILED.

func (s *Stager) Stage(ctx context.Context, sha string, subjects []record.Subject, on platform.Release) ([]run.Member, map[string]run.Preflight, error) {
	dir, drop, err := s.temp.MakeDir("stage")
	if err != nil {
		return nil, nil, err
	}
	s.keep = append(s.keep, drop)
	members := make([]run.Member, 0, len(subjects))
	pre := make(map[string]run.Preflight, len(subjects))

	// THE TREE IS BUILT BEFORE ANY MEMBER IS ASKED ANYTHING. The overlay
	// has to be a tree (see the type's doc), and the preflight is an
	// EVALUATION: it reads known_fail and use_xcode out of the staged
	// Portfile, which is a Portfile that may open a port group. Read
	// before _resources exists, that evaluation resolves port groups
	// through getportresourcepath's fallback — against the
	// INSTALLATION'S DEFAULT TREE, not the commit under test — or fails
	// outright with "PortGroup not found" and leaves the member unread.
	//
	// Both outcomes decide scheduling. An unread preflight is scheduled
	// as an ordinary build, so a known_fail the commit declares is
	// spent on a VM and a use_xcode it declares is not asked for; a
	// default tree that answers differently answers wrongly and in
	// silence.
	//
	// A missing _resources is still not a staging failure — some trees
	// do not carry one — so the error is carried into every preflight
	// rather than refusing an attempt whose members would all
	// materialize. It is recorded BEFORE the members are read, which is
	// the difference: a preflight that ran without the tree used to
	// stand as an answer unless materializing happened to fail.
	resErr := s.repo.Materialize(ctx, sha, build.ResourcesDir, dir)

	for _, sub := range subjects {
		if err := s.repo.Materialize(ctx, sha, sub.Portdir, dir); err != nil {
			return nil, nil, err
		}
		staged := filepath.Join(dir, filepath.FromSlash(sub.Portdir))
		members = append(members, run.Member{
			Port:    sub.Port,
			Portdir: staged,
			Names:   append([]string(nil), sub.Names...),
		})
		pf := s.preflight(ctx, staged, sub, on)
		if resErr != nil && pf.Err == nil {
			pf.Err = fmt.Errorf("staging %s: %w", build.ResourcesDir, resErr)
		}
		pre[sub.Port] = pf
	}
	return members, pre, nil
}

// Cleanup drops every tree this stager has materialized and forgets
// them. It is THE PER-PASS CLEANUP THE DRAIN'S RE-PLAN CREATES: a pass
// stages one tree per attempt it starts, and a process that lives a
// month would otherwise accumulate one per attempt per tick under the
// run's temporary root, which tempdir.Root only removes when the
// process ends. A one-shot verb never notices; a dispatcher fills a
// disk.
//
// It is called between passes and never inside one, because the guest
// is served FROM the staged tree: dropping it while a build is running
// would pull the ports tree out from under the environment. What makes
// that safe at the pass boundary is that verify.Request carries the
// portdirs to the provider, which copies what it needs into the guest
// before Submit returns.

func (s *Stager) Cleanup() {
	for i := len(s.keep) - 1; i >= 0; i-- {
		s.keep[i]()
	}
	s.keep = nil
}

// preflight asks, before any VM boots, whether a portdir declares
// known_fail under the target release's platform frame — mpbb's own
// layering, borrowed: the buildbot excludes known_fail ports at list
// time by reading the evaluated option, and only falls back to
// discovering it mid-build. The same session answers use_xcode, so a
// port that needs a full toolchain is probed for one before the build
// starts rather than forty minutes in.
//
// A FAILURE IS RECORDED AND NEVER RAISED. Preflight.Read is what parts
// "the port does not declare known_fail" from "the staged Portfile
// could not be read", and this function's whole contribution to that
// distinction is refusing to conflate them: an evaluation that could
// not run comes back Read false with its error, and run.Plan schedules
// the member normally.

func (s *Stager) preflight(ctx context.Context, staged string, sub record.Subject, on platform.Release) run.Preflight {
	// RULE 7, AND THE MOST EXPENSIVE PLACE IT WAS MISSING. A zero
	// release means os.major 0, which is older than any macOS that has
	// ever existed — so qt5's min-version callback fires, qt6's fires,
	// every cxx_standard port declines, and run.Plan reads all of it as
	// the PORT refusing the platform and records `unsupported` without
	// booting a guest. Measured: three of cmark's dependents, none of
	// which declares known_fail in its Portfile at all.
	//
	// "I was not told which platform" is not "the port declines". It
	// comes back unread, exactly like a Portfile that could not be
	// evaluated, and run.Plan then schedules the member normally — the
	// safe direction, because a preflight exists to save a VM and never
	// to invent a verdict.
	if on.IsZero() {
		return run.Preflight{Err: errors.New("no platform to evaluate the port against")}
	}
	if s.session == nil {
		return run.Preflight{Err: ErrNoEvaluator}
	}
	// The architecture is THIS MACHINE'S, because a tart guest runs the
	// architecture of the Mac hosting it — the frame is describing a
	// real environment, not a hypothetical one. It was the literal "arm",
	// which cost nothing while a frame did not simulate architecture and
	// costs a wrong known_fail now that it does.
	frame := info.Platform{OS: "macosx", Major: on.Darwin, Arch: platform.HostArch()}
	ev, err := s.session(ctx, eval.WithPlatform(frame))
	if err != nil {
		return run.Preflight{Err: err}
	}
	defer ev.Close()
	h := port.New(subportTarget(staged, sub), ev)
	opts, err := h.Options(ctx, "known_fail", "use_xcode", "test.run")
	if err != nil {
		return run.Preflight{Err: err}
	}
	hasTests := tclTrue(opts["test.run"])
	return run.Preflight{
		Read:       true,
		KnownFail:  tclTrue(opts["known_fail"]),
		NeedsXcode: tclTrue(opts["use_xcode"]),
		HasTests:   &hasTests,
		Reason:     opts["known_fail_reason"],
	}
}

// subportTarget names the evaluation context a member is: the staged
// portdir, with the subport spelled when the record says the member is
// not the portdir's own top-level port. A cohort seats subports beside
// their parents and a preflight asked of the parent would answer the
// wrong port's known_fail.

func subportTarget(staged string, sub record.Subject) tree.Target {
	t := tree.Target{Portdir: staged}
	if sub.Port != "" && sub.Port != path.Base(sub.Portdir) {
		t.Subport = sub.Port
	}
	return t
}

// tclTrue reads a value the way Tcl's [string is true] does, which is
// how mpbb judges known_fail. Transcribed rather than imported: the
// question is what BASE would call true, and a Go-side ParseBool
// answers a different question that agrees on four values out of six.

func tclTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// local is run.Local: the LOCAL half of a cohort proposal's inputs —
// the tree's reverse index and the maintainer's Portfile cues. run
// holds neither an index nor an evaluator, which is why it is an
// interface there and a value here.
//
// A BACKEND THAT CANNOT ANSWER RETURNS AN ERROR, and the propose step
// records nothing: no finding is not a finding of "no dependents"
// (rule 7). An unbuilt PortIndex is exactly that case, and it is the
// ordinary one on a fresh checkout.
// local answers run.Local: the reverse index for a port's dependents,
// and the maintainer's cues out of a Portfile AT A COMMIT.
//
// It carries a repository as well as a tree because the cues are read
// from git. They used to come from a host path, and the path handed in
// on every settlement that replays a frozen roster is empty — so the
// read landed on whatever Portfile was under the process's working
// directory, and its error was discarded.

func (s *Stager) Baseline(ctx context.Context, sha string, subjects []record.Subject) ([]string, error) {
	if sha == "" || len(subjects) == 0 {
		return nil, nil
	}
	dir, drop, err := s.temp.MakeDir("baseline")
	if err != nil {
		return nil, err
	}
	s.keep = append(s.keep, drop)
	out := make([]string, 0, len(subjects))
	for _, sub := range subjects {
		// A SUBJECT THAT WOULD NOT MATERIALIZE IS AN ERROR, not a `continue`.
		//
		// It was a continue, and that is the first of four places one
		// baseline failure was swallowed on its way to nobody. An empty
		// slice with a nil error is indistinguishable from "this change has
		// no base", so run.Start proceeded, the provider declined for want
		// of a before, the ABI comparison declined on the provider, the
		// cohort declined on the comparison, and the record ended up
		// holding baseline_source "none" with nothing to explain it.
		//
		// Measured on a real change whose merge-base portdir was present at
		// the base commit the record names — so the staging failed for a
		// reason that no longer exists anywhere, which is precisely what a
		// swallowed error costs.
		//
		// The caller still treats a missing baseline as degradation rather
		// than a fault (run.Start), which is the design: the comparison
		// says "undescribed" and the build is still worth running. What
		// changes is that it now degrades WITH A REASON.
		if err := s.repo.Materialize(ctx, sha, sub.Portdir, dir); err != nil {
			return nil, fmt.Errorf("staging %s at %s for the baseline: %w",
				sub.Portdir, git.Abbrev(sha), err)
		}
		out = append(out, filepath.Join(dir, filepath.FromSlash(sub.Portdir)))
	}
	// THE OVERLAY IS A PORTS TREE, NOT A BAG OF PORTDIRS, and the baseline
	// overlay was a bag of portdirs. Stage materializes build.ResourcesDir
	// beside its members and this did not, while the tart provider stages
	// both overlays through one function that tars _resources out of
	// whichever root it is handed. So the host tar was asked for a
	// directory that was not there on EVERY baseline, for EVERY port, on
	// every run.
	//
	// That is why the ABI comparison has never produced a measurement.
	// Not a bad guest, not a missing archive: the before was never staged,
	// so the provider declined for want of one, the comparison declined on
	// the provider, and the cohort proposal declined on the comparison —
	// down a path that discarded its own reasoning at four separate
	// points, which is why it read as environmental for as long as it did.
	//
	// The provider's own comment says the consequence in advance: a port
	// served from an overlay without _resources "has no archive site at
	// all", and `port -b install` fails with "no usable archive sites
	// configured" — "That is the baseline's entire second step, which
	// means the ABI comparison cannot be made for any port anywhere until
	// this is staged." It was written about the branch overlay, and it was
	// true of the baseline overlay the whole time.
	if err := s.repo.Materialize(ctx, sha, build.ResourcesDir, dir); err != nil {
		return nil, fmt.Errorf("staging %s at %s for the baseline: %w",
			build.ResourcesDir, git.Abbrev(sha), err)
	}
	return out, nil
}
