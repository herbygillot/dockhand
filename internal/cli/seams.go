package cli

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/dependents"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/build"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/portnote"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/run"
	"github.com/herbygillot/dockhand/internal/tempdir"
)

// This file holds the CONSUMER-OWNED SEAMS the domain declares and the
// composition root implements: run.Stager, run.Local, change.Evaluator
// and publish.Evaluator. Every one of them is an interface in the
// package that NEEDS it and an implementation here, because each is a
// question about a MacPorts installation and a git repository — two
// things the lifecycles deliberately do not hold.

// stager materializes the portdirs an attempt will build from the
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
type stager struct {
	repo *git.Repo
	temp tempdir.Root
	// session opens a SHORT-LIVED evaluator framed on the TARGET
	// release, which is why it is a function and not the run's own
	// evaluator. The preflight asks what a portdir declares under the
	// platform it will be built on — known_fail and use_xcode are
	// per-platform options — and the run's evaluator carries the host's
	// frame. A draft that reused it would have answered the host's
	// question about a guest's Portfile.
	session func(ctx context.Context, opts ...eval.Option) (*eval.Evaluator, error)
	// release is the platform the preflight is framed on. It is a value
	// on the stager and not a parameter of Stage because run.Stager's
	// signature is the queue's — a sha and the subjects — and the
	// platform is the ATTEMPT's, which the road that built this stager
	// already knows. A stager built for one attempt answers for that
	// attempt's platform.
	release platform.Release
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
func (s *stager) Stage(ctx context.Context, sha string, subjects []record.Subject) ([]run.Member, map[string]run.Preflight, error) {
	dir, drop, err := s.temp.MakeDir("stage")
	if err != nil {
		return nil, nil, err
	}
	s.keep = append(s.keep, drop)
	members := make([]run.Member, 0, len(subjects))
	pre := make(map[string]run.Preflight, len(subjects))
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
		pre[sub.Port] = s.preflight(ctx, staged, sub)
	}
	// The overlay has to be a tree; see the type's doc. A missing
	// _resources is not a staging failure — some trees do not carry one
	// — so the error is reported through the preflight rather than
	// refusing an attempt whose members all materialized.
	if err := s.repo.Materialize(ctx, sha, build.ResourcesDir, dir); err != nil {
		for port, pf := range pre {
			if pf.Err == nil {
				pf.Err = fmt.Errorf("staging %s: %w", build.ResourcesDir, err)
				pre[port] = pf
			}
		}
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
func (s *stager) Cleanup() {
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
func (s *stager) preflight(ctx context.Context, staged string, sub record.Subject) run.Preflight {
	if s.session == nil {
		return run.Preflight{Err: errNotAcquired{"an evaluator"}}
	}
	frame := info.Platform{OS: "macosx", Major: s.release.Darwin, Arch: "arm"}
	ev, err := s.session(ctx, eval.WithPlatform(frame))
	if err != nil {
		return run.Preflight{Err: err}
	}
	defer ev.Close()
	h := port.New(subportTarget(staged, sub), ev)
	opts, err := h.Options(ctx, "known_fail", "use_xcode")
	if err != nil {
		return run.Preflight{Err: err}
	}
	return run.Preflight{
		Read:       true,
		KnownFail:  tclTrue(opts["known_fail"]),
		NeedsXcode: tclTrue(opts["use_xcode"]),
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
type local struct{ tr *tree.Tree }

// Dependents is the reverse index's answer for one port.
func (l local) Dependents(_ context.Context, portName string) ([]portindex.Dependent, []portindex.Unread, error) {
	if l.tr == nil {
		return nil, nil, errNotAcquired{"a ports tree"}
	}
	rev, err := l.tr.Dependents()
	if err != nil {
		return nil, nil, err
	}
	return rev.ByPort[strings.ToLower(portName)], rev.Unread, nil
}

// Instructions is the maintainer's own cues: a revision-bump comment in
// the Portfile, read from the tree as it stands and TRANSCRIBED rather
// than acted on.
//
// THE CHEAP HALF RUNS FIRST, and that is a cost gate rather than a
// correctness one. The roster the reader matches names against is the
// tree's reverse index, and building it is one sequential pass over a
// 25 MB PortIndex; a few dozen Portfiles in a 41630-port tree carry one
// of these comments, so filling the roster unconditionally would spend
// that pass on every port in order to narrow a roster nothing will
// consult. portnote.MentionsRevbump answers the same two patterns over
// the same blocks, so a Portfile it says no about is one the reader
// would have skipped.
//
// AN UNBUILT INDEX IS NOT A FAILURE HERE. The reader's fallback is its
// own word list, and its own refusal — stop at the first token it
// cannot justify — is what a person finishes by hand; a roster that
// could not be built therefore narrows nothing rather than losing the
// instruction, which is why the index error is dropped and the quote
// still travels.
func (l local) Instructions(_ context.Context, portdir string) ([]dependents.Instruction, error) {
	src, err := os.ReadFile(filepath.Join(portdir, macports.PortfileName)) //nolint:gosec // the portdir is the record's own
	if err != nil {
		return nil, err
	}
	if !portnote.MentionsRevbump(src) {
		return nil, nil
	}
	var known []string
	if l.tr != nil {
		if rev, err := l.tr.Dependents(); err == nil {
			for _, d := range rev.ByPort[strings.ToLower(path.Base(portdir))] {
				known = append(known, d.Name)
			}
		}
	}
	out := make([]dependents.Instruction, 0, 2)
	for _, in := range portnote.Instructions(src, known) {
		out = append(out, dependents.Instruction{
			Source: macports.PortfileName, Quote: in.Quote, Ports: in.Ports,
		})
	}
	return out, nil
}

// blobEvaluator is change.Evaluator: what a portdir EVALUATES TO, asked
// of the working tree's copy. It is the seam change.Prepare holds a
// prediction against, and it is the run's own evaluator because the
// question is about the port as this machine has it.
type blobEvaluator struct{ ev *eval.Evaluator }

// Values evaluates one portdir. A nil evaluator is a caller with no
// MacPorts installation to ask, and change.Prepare treats that as an
// absence rather than a failure — which is why the nil check lives at
// the composition root that decides whether to build one of these at
// all, and not here.
func (b blobEvaluator) Values(ctx context.Context, portdir string) (info.Values, error) {
	return b.ev.Values(ctx, portdir, "", "")
}

// identityAt is publish.Evaluator: the epoch/version/revision a port
// evaluates to AT A COMMIT, which is what publish.Move needs to say
// whether base would upgrade an install at one to a tree at the other.
//
// It materializes and evaluates rather than parsing, and that is the
// whole reason it is here: a version literal in a Portfile is not the
// version the port evaluates to — `version 0.7.6-20${snapshot}` is the
// ordinary counter-example — and a downgrade judged from source text
// would be judged from a string the port itself disagrees with.
type identityAt struct {
	ev   *eval.Evaluator
	repo *git.Repo
	temp tempdir.Root
}

// IdentityAt stages the portdir at rev and asks the evaluator what it
// is. The staged copy is dropped before returning: this answers one
// question and holds nothing after it.
func (e identityAt) IdentityAt(ctx context.Context, rev, portdir string) (macports.Identity, error) {
	dir, drop, err := e.temp.MakeDir("identity")
	if err != nil {
		return macports.Identity{}, err
	}
	defer drop()
	if err := e.repo.Materialize(ctx, rev, portdir, dir); err != nil {
		return macports.Identity{}, err
	}
	v, err := e.ev.Values(ctx, filepath.Join(dir, filepath.FromSlash(portdir)), "", "")
	if err != nil {
		return macports.Identity{}, err
	}
	return macports.Identity{Epoch: v.Epoch, Version: v.Version, Revision: v.Revision}, nil
}
