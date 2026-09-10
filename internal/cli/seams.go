package cli

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/dependents"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/portnote"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/tempdir"
)

// This file holds the CONSUMER-OWNED SEAMS the domain declares and the
// composition root implements: run.Stager, run.Local, change.Evaluator
// and publish.Evaluator. Every one of them is an interface in the
// package that NEEDS it and an implementation here, because each is a
// question about a MacPorts installation and a git repository — two
// things the lifecycles deliberately do not hold.

type local struct {
	tr   *tree.Tree
	repo *gitRepo
}

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

// Requires is the forward dependency lookup, for ordering a cohort's
// builds. A checkout with no tree acquired answers nothing, and the
// caller treats that as "no ordering known" rather than as a failure:
// the graph makes a build order better and its absence is what shipped
// until now.
func (l local) Requires(_ context.Context, ports []string) (map[string][]string, error) {
	if l.tr == nil {
		return nil, errNotAcquired{"a ports tree"}
	}
	return l.tr.Requires(ports)
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
func (l local) Instructions(ctx context.Context, sha, portdir string) ([]dependents.Instruction, error) {
	if l.repo == nil {
		return nil, errNotAcquired{"a repository"}
	}
	if sha == "" || portdir == "" {
		return nil, fmt.Errorf("reading maintainer cues: no commit or portdir was named")
	}
	src, err := l.repo.BlobAt(ctx, sha, portdir+"/"+macports.PortfileName)
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

// Baseline materializes the subjects as they stood at another commit —
// the merge base — so the provider can measure what the change is
// leaving behind.
//
// IT IS A SEPARATE STAGING DIRECTORY from Stage's, deliberately: the two
// trees hold the same paths with different contents, and one directory
// would have the second Materialize overwrite the first. Both are
// registered for cleanup on the same run.
//
// A SUBJECT THE BASE DOES NOT HOLD IS SKIPPED AND NOT A FAILURE. A
// change that ADDS a port has no before for it, which is a fact about
// the change rather than a staging error, and a provider handed a
// partial baseline measures what it can.
