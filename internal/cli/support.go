package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/planning"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
)

// gitRepo is the repository handle under a name this file can use in a
// signature without importing git into every helper's line. It is an
// alias and not a wrapper: the value IS git's, and a wrapper would be a
// second place a repository's contract could drift.
type gitRepo = git.Repo

// sink is progress.Sink over a stream: the narration a person watching
// wants, and nothing a caller learns anything from. A JSON caller wires
// progress.Discard{} instead.
//
// Its methods return nothing because Sink's do, and that is the property
// the type exists for: a caller cannot learn anything by narrating, and
// an operation cannot fail because nobody was listening.
type sink struct{ w io.Writer }

// Stage names the operation and the stage it has reached.
func (s sink) Stage(op, stage string) { fmt.Fprintf(s.w, "%s: %s\n", op, stage) }

// Say is one sentence at one level. Detail is the per-target chatter a
// sweep of four hundred produces and it is printed, because the sweep's
// rows ARE its progress; a quieter threshold is a flag nobody has ruled.
func (s sink) Say(l progress.Level, text string) {
	switch l {
	case progress.Warn:
		fmt.Fprintln(s.w, "warning: "+text)
	case progress.Info, progress.Detail:
		fmt.Fprintln(s.w, text)
	}
}

// Stream passes a raw build log through, unparsed.
func (s sink) Stream(r io.Reader) { _, _ = io.Copy(s.w, r) }

var _ progress.Sink = sink{}

// session starts a fresh evaluator against this run's installation, for
// the readings the run's own single evaluator cannot serve: one framed
// on a TARGET platform rather than the host, and one that must be closed
// before the directories it evaluated are removed. The CALLER closes it
// — that is the difference from Services.Eval, which Services closes.
func (s *Services) session(ctx context.Context, opts ...eval.Option) (*eval.Evaluator, error) {
	pfx, err := s.Prefix()
	if err != nil {
		return nil, err
	}
	return eval.Start(ctx, pfx, opts...)
}

// mustEval is the run's evaluator as planning's oracle seam, or nil.
//
// A nil oracle is planning.ErrNoEvaluator at Plan time and never a
// panic, which is the honest shape: a plan made without asking the port
// anything is not a smaller plan, it is a different act, and the refusal
// belongs where the plan is attempted rather than where it is wired.
func mustEval(s *Services) port.Oracle {
	if s.ev == nil {
		return nil
	}
	return s.ev
}

// portHandle addresses one evaluation context on the run's own
// evaluator, with the run's temporary root so a shadow evaluation's
// copies are attributable to this invocation.
func portHandle(target tree.Target, ev *eval.Evaluator, s *Services) port.Handle {
	return port.New(target, ev).WithTempDir(s.Temp())
}

// definitions is the write-intent catalogue as DATA, which is what
// planning.Planner takes: it chooses from the list by name and never
// branches on which intent is running.
func definitions() []intent.Definition {
	verbs := intentCatalogue()
	out := make([]intent.Definition, 0, len(verbs))
	for _, v := range verbs {
		out = append(out, v.Definition)
	}
	return out
}

// planningFor is the planner an OPERATION carries — for provenance on
// the survey road and for the machine publish slot's Reconstruct on the
// pass road. It is the same value the verb planned with, so a
// reconstruction is made by the thing that made the plan.
func planningFor(s *Services) planning.Planner {
	return planning.Planner{Eval: mustEval(s), Fetch: s.Fetch(), Temp: s.Temp(), Catalog: definitions()}
}

// forgeLogin answers the gh: half of `maintainer:me` through the run's
// own forge seam.
//
// It is wired here rather than reached for inside the grammar because
// this is the composition root: who you are on the forge is a fact about
// the machine, and a selector package that ran gh for itself could not
// be tested without one.
func forgeLogin(s *Services) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		login, err := s.Forge(ctx, "api", "user", "-q", ".login")
		if err != nil {
			return "", fmt.Errorf("finding your forge handle needs gh: %w (or spell it out)", err)
		}
		return strings.TrimSpace(login), nil
	}
}

// gitIdentity answers the mail: half of `maintainer:me`.
//
// Both halves are asked because neither is complete: on a real tree the
// forge handle names 1070 of the maintainer's ports and the mail key
// names 1072, and the two stragglers are ports whose maintainers list
// spells the handle a third way.
func gitIdentity(s *Services) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		bin, err := s.Tools.Find(tool.Git)
		if err != nil {
			return "", err
		}
		out, err := tool.Run(ctx, bin, tool.Opts{Args: []string{"config", "--get", "user.email"}})
		if err != nil {
			return "", fmt.Errorf("git config user.email: %w", err)
		}
		return strings.TrimSpace(out), nil
	}
}

// emitDiff prints the patch a change would carry, as a git diff, and
// writes nothing.
//
// It is a DIFF OF TREES and not of files: the prepared content is
// grafted onto the base commit's tree as an unreferenced object and
// compared with it, so what is printed is exactly the patch the mint
// commit would carry — including a file the plan adds and one it
// deletes, which a per-file diff of edited bytes could not show.
func emitDiff(ctx context.Context, s *Services, prepared change.Prepared) error {
	repo, err := s.Repo()
	if err != nil {
		return err
	}
	after, err := prepared.Identify(ctx, repo, prepared.Base.Sha)
	if err != nil {
		return err
	}
	before, err := repo.RevParse(ctx, prepared.Base.Sha+"^{tree}")
	if err != nil {
		return err
	}
	patch, err := repo.DiffTrees(ctx, before, string(after))
	if err != nil {
		return err
	}
	_, err = s.Out.Write(patch)
	return err
}

// workTree is change.Tree over a person's actual checkout. IT IS THE
// BOUNDARY change's own rule names — "the conversion happens once, in
// the caller that holds a repository" — so the join from a tree-relative
// path to a host one happens here and nowhere inside change.
type workTree struct{ root string }

func (w workTree) host(p string) string { return filepath.Join(w.root, filepath.FromSlash(p)) }

func (w workTree) Read(p string) ([]byte, bool, error) {
	b, err := os.ReadFile(w.host(p))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

func (w workTree) Write(p string, content []byte, mode fs.FileMode) error {
	return os.WriteFile(w.host(p), content, mode)
}

func (w workTree) Remove(p string) error {
	if err := os.Remove(w.host(p)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// writeInPlace realizes a change into the working tree and commits
// nothing. What it applies, and what happens if part of it fails, is
// change.Prepared.ApplyTo's — this holds the repository and therefore
// the paths, and says the sentence.
func writeInPlace(s *Services, pl *plan.Plan, prepared change.Prepared) error {
	repo, err := s.Repo()
	if err != nil {
		return err
	}
	if err := prepared.ApplyTo(workTree{root: repo.Root}); err != nil {
		return err
	}
	fmt.Fprintf(s.Out, "edited %s in place; nothing was committed\n", pl.Portdir)
	return nil
}

// The release flags. Every verb that names a macOS release spells it the
// same way — a name, a version, and where a set makes sense, "all" — and
// this is that vocabulary in one place.

// parseRelease reads one --on or --macos value into a release. A value
// the table does not name is the invocation's fault, so the error lands
// in the usage band. Nothing else is interpreted here: "" and "all" are
// refused like any other unknown name, because the verbs that treat
// either specially decide so before parsing, and not all of them agree
// on what "all" means.
func parseRelease(s string) (platform.Release, error) {
	r, err := platform.Parse(s)
	if err != nil {
		return platform.Release{}, &UsageError{Err: err}
	}
	return r, nil
}

// releaseFlag parses --on for the verbs that take ONE release: the empty
// flag means the provider default, which is the newest provisioned base.
// A matrix is refused with directions, because an intent verb's --on is
// singular and `verify --on <list|all>` is the road that takes several.
func releaseFlag(on string) (platform.Release, error) {
	if on == "" {
		return platform.Release{}, nil
	}
	if strings.EqualFold(on, "all") || strings.Contains(on, ",") {
		return platform.Release{}, usagef("this verb takes one platform; run the matrix with `dockhand verify <branch> --on <list|all>`")
	}
	return parseRelease(on)
}

// settleRelease fills in the platform a change will be verified on,
// BEFORE the attempt is minted.
//
// It used to be left zero and resolved inside the provider at submit
// time, where tart.baseFor substitutes Bases[0] for a zero release —
// the one place nobody else can see the answer. Three parties then
// disagreed about a single run: the guest built on the newest base, the
// record said the platform was "", and the PREFLIGHT evaluated every
// staged Portfile at os.major 0.
//
// That last one is why this exists. Zero is older than any PortGroup
// supports, so qt5's min-version callback, qt6's, and every
// cxx_standard port declare known_fail against it — and run.Plan
// declines a known_fail member BEFORE booting a VM. Measured on cmark's
// dependents: Aseprite, PrismLauncher and nheko were all recorded
// `unsupported` — a verdict that exits 0, reads as the change working
// as intended, and counts as an outcome a promotion may publish on —
// without a guest ever building one of them. None of the three declares
// known_fail in its Portfile at all.
func settleRelease(ctx context.Context, s *Services, f *intentFlags, verifying bool) error {
	if !verifying || !f.release.IsZero() {
		return nil
	}
	provisioned, err := provisionedReleases(ctx, s)
	switch {
	case errors.Is(err, verify.ErrNoProvider), errors.Is(err, verify.ErrNoEnvironment):
		// NOT THIS FUNCTION'S REFUSAL TO MAKE, and making it here broke
		// the road the composition root documents: "no tart at all is
		// ErrNoProvider, and the roads narrow their contract around it (a
		// bump mints and says unverified)". app.Change already does
		// exactly that — it reads the resolver's error, declines to
		// enqueue, mints the branch and carries the advisory.
		//
		// Resolving the platform is an errand on the way to a build. A
		// machine with no build to reach has no platform to resolve and
		// no question to answer, so this leaves the release as it found
		// it and lets the road say the true thing.
		//
		// Measured: `bump litestream` on a host with no tart exited 33
		// having minted nothing, where the design and the docs both say
		// it mints and reports "unverified".
		return nil
	case err != nil:
		return err
	}
	rel, err := resolveReleaseSet(nil, provisioned, true)
	if err != nil {
		return err
	}
	f.release = rel[0]
	return nil
}

// resolveReleaseSet resolves a list flag against the provisioned bases:
// nothing means the newest, "all" means every base, and otherwise each
// value in the order given, duplicates included.
//
// With requireBase, each named release must actually HAVE a base — a
// verdict cannot be promised on an environment that does not exist. The
// check runs per element, interleaved with parsing, so whichever of an
// unprovisioned and an unknown name the user wrote first is what they
// hear about. Without requireBase the names are taken as given, which is
// exec's contract.
func resolveReleaseSet(on []string, provisioned []platform.Release, requireBase bool) ([]platform.Release, error) {
	if len(provisioned) == 0 && (requireBase || len(on) == 0) {
		// A BACKSTOP, and pointed at the verb that can actually answer.
		// The ordinary road reaches realVerifier's own refusal first,
		// which lists the goldens and names --restore when one stands;
		// this function is pure and holds no finder, so rather than
		// guessing at a remedy it sends the reader to doctor, which
		// reports both populations.
		return nil, fmt.Errorf("%w: no base images; `dockhand doctor` reports what this machine holds",
			verify.ErrNoEnvironment)
	}
	if len(on) == 0 {
		return provisioned[:1], nil
	}
	var out []platform.Release
	for _, v := range on {
		if strings.EqualFold(v, "all") {
			return provisioned, nil
		}
		r, err := parseRelease(v)
		if err != nil {
			return nil, err
		}
		if requireBase && !slices.Contains(provisioned, r) {
			return nil, fmt.Errorf("%w: no base image for %s; `dockhand provision tart --macos %s` builds one",
				verify.ErrNoEnvironment, r.Name, strings.ToLower(r.CompactName()))
		}
		out = append(out, r)
	}
	return out, nil
}

// modernReleases is the span a fresh machine provisions and the Xcode
// guide explains when no base narrows it: Monterey (Darwin 21) onward,
// in table order, oldest first. It is read from the table rather than
// from a provider's capabilities on purpose — a provider reports its
// bases newest first, and the provision sweep's progress lines and the
// Xcode needs table are printed in this order.
func modernReleases() []platform.Release {
	var out []platform.Release
	for _, r := range platform.Releases {
		if r.Darwin >= 21 {
			out = append(out, r)
		}
	}
	return out
}

// provisionedReleases asks the machine which bases exist, for the verbs
// whose --on resolves against them. It goes through the provider
// resolver rather than naming tart, so the one place a backend is named
// stays realVerifier.
func provisionedReleases(ctx context.Context, s *Services) ([]platform.Release, error) {
	prov, err := s.Verifier(ctx)
	if err != nil {
		return nil, err
	}
	return prov.Capabilities().Platforms, nil
}

// needsPass is what a PASS declares, written out because a pass is the
// one road that genuinely reaches almost everything and a reader is
// owed the reason for each entry rather than the word "all".
//
//	Repo       the state ref, the ledger and every branch it retires
//	Evaluator  change.Reconstruct's, for the machine publish slot's
//	           simplicity judgment, and publish's direction comparison
//	Fetcher    behind the planner the same slot re-plans with; acquired
//	           ONCE and held for a resident dispatcher's whole life,
//	           which is what makes the drain's re-plan affordable
//	Verifier   the settle, discharge and drain stages
//	Forge      the close stage's Gather and the publish stage's Apply
//
// Tree is NOT here, and that is the same distinction ProposeTree
// documents: a pass resolves no selector — it runs over what the store
// already carries — and the only tree it would want is the propose
// step's, which records nothing when there is none.
func needsPass() app.Needs {
	// Tree and Index because a pass SETTLES: a passing attempt proposes
	// its cohort, and that survey reads the ports index. A dispatcher on
	// a tree with no index cannot finish the job it exists to do, so it
	// says so at startup rather than after the first build it drains.
	return app.Needs{Repo: true, Evaluator: true, Fetcher: true, Verifier: true, Forge: true, Tree: true, Index: true}
}

// baseFiles reads the base's bytes for every whole file a plan rewrites,
// keyed by the plan's own portdir-relative paths — change.Source.Files,
// which is what turns a whole-file write into a checkable one.
//
// A PATH THE BASE DOES NOT HOLD IS ABSENT AND NEVER AN ERROR, which is
// the whole reason this uses the batch session rather than BlobAt:
// `cat-file --batch` answers an unresolvable request with a missing line
// and git.ErrNoObject, where BlobAt's error cannot tell "no such file"
// from "the repository would not answer". A read that FAILED is returned
// — a preparation that could not establish the precondition must not
// proceed as though the file were new (rule 7).
