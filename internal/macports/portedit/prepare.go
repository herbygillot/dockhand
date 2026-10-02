package portedit

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/herbygillot/dockhand/internal/macports/depblock"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/pypi"
)

var (
	errProbeInconclusive = observe.ErrInconclusive
	ErrNotImplemented    = errors.New("portedit: requested transformation is not implemented")
	ErrUnsupported       = portfile.ErrUnsupported
	ErrFidelity          = fidelity.ErrMismatch
)

type CommitIntent struct {
	Subject string
}

type Request struct {
	// EditIntent is the person's choices for the edit, as bound or as
	// recorded on the job: a Stub already resolved there is honored rather
	// than resolved again.
	model.EditIntent
	Action model.EditKind
	Source model.Source
	// Workspace is the projection of the source the edit reads and never
	// writes; every candidate is an overlay of it. It is the caller's to
	// open and close, and it outlives every probe made from it.
	Workspace *workspace.Workspace
	Selection macports.Selection
	Platform  model.Platform
	Version   string
	// Subject is the commit subject after the port name; empty takes the
	// editor's default for the action, and a revision bump has none.
	Subject string
	Release *model.Release
	// KeepArchives is a directory, the caller's, where a version update
	// keeps the new archives and also fetches the current version's, so
	// the two can be compared; empty keeps neither.
	KeepArchives string
	// Stealth asks a checksum refresh to make a stealth update of an
	// archive it finds changed under the same name; nil refreshes the
	// checksums alone.
	Stealth *StealthRequest
}

// Fidelity is the comparison report for one evaluated edit.
type Fidelity = fidelity.Report

// ContextCoverage distinguishes metadata models from the native host. Neither
// kind records a build; verification providers establish build results.
type ContextCoverage struct {
	Fetch    *macports.FetchSemantics `json:",omitempty"`
	Platform model.Platform
	// Variant is the variant asked for in a variant's context, one whose
	// body declares archives of its own.
	Variant  string `json:",omitempty"`
	Modeled  bool
	Affected bool
	// FetchesNothing is a context where the port has no archive to
	// download, as a metaport or a _select port has.
	FetchesNothing bool `json:",omitempty"`
}

type Result struct {
	Scope    *macports.ReleaseScope `json:",omitempty"`
	Coverage []ContextCoverage      `json:",omitempty"`
	Base     model.Source
	Target   model.Target
	Files    []portfile.Edit
	Commits  []CommitIntent
	Fidelity []Fidelity
	// Prepared is the evaluated snapshot of the prepared files, the state
	// every later step reads: the go.mod check, dependency regeneration,
	// the patch check, and the adapter's final evaluation. It is set with
	// each fidelity report, so it is the last report's snapshot without
	// any reader having to know that.
	Prepared  macports.Snapshot `json:"-"`
	Release   *model.Release
	Downloads []distfetch.Download
	// Crates are the archives of the Git-pinned crates a Cargo update
	// fetched for their checksums: dependencies, not the port's source, so
	// no pair is theirs to compare.
	Crates []distfetch.Download `json:",omitempty"`
	// Regenerated are the dependency blocks the update wrote again, each
	// with how many entries it has, and how many aren't as they were.
	Regenerated []Regenerated `json:",omitempty"`
	// Pairs are each archive the update replaced beside the one that
	// replaces it, in every context that fetches them, fetched only when
	// KeepArchives asked; PreviousProblem is why the replaced ones could
	// not be.
	Pairs           []ArchivePair `json:"-"`
	PreviousProblem string        `json:",omitempty"`
	// Patches reports whether each declared patch file still applies to the
	// candidate source; a rejected patch is a finding, not a refusal.
	Patches []patchcheck.Result `json:",omitempty"`
	// Dropped are the patches the update took out, with their files,
	// since the new source already holds them.
	Dropped []string `json:",omitempty"`
	// GoToolchain is what a module-mode port's go.mod requires, and what
	// the update did about the Portfile's go.toolchain_min; nil where no
	// go.mod was read.
	GoToolchain *GoToolchain `json:",omitempty"`
	// Stealth is the stealth update a checksum refresh found and made; nil
	// when it found none, or wasn't asked (Request.Stealth).
	Stealth *Stealth `json:",omitempty"`
	// DistSubdirRemoved is true when a version update removed the
	// dist_subdir an earlier stealth update set.
	DistSubdirRemoved bool `json:",omitempty"`
	// Unchanged is the port as it stands where nothing was edited, as for
	// an update to the version it already has: what it is at, which no
	// fidelity report says then. Nil when there was an edit.
	Unchanged *macports.PortInfo `json:"-"`
}

// Regenerated is one dependency block an update wrote again: go.vendors,
// cargo.crates, or cargo.crates_github, its entries, a Go module or a
// crate each, and those that aren't as they were.
type Regenerated struct {
	Option         string
	Count, Changed int
	// Dropped are the crates the Portfile pinned over its lock that the
	// new lock moved past, so the pins went with the rest.
	Dropped []Override `json:",omitempty"`
	// Unchecked says why the block as it was couldn't be checked against
	// the source it was made from, where it couldn't: it's regenerated
	// whole, with no override found to keep.
	Unchecked string `json:",omitempty"`
	// Notices are what the update says of the block that holds nothing:
	// Git crates the Portfile declares under a branch where the lock pins
	// them otherwise, which look unused, and cargo.update being on.
	Notices []string `json:",omitempty"`
}

// Override is a registry crate a Portfile pinned at another version than
// its lock: Pinned over the lock's Was, which the new lock's Locked
// moved past.
type Override struct {
	Name, Pinned, Was, Locked string
}

// GoToolchain is the Go release a module-mode port's go.mod requires, and
// what the update did about the Portfile's go.toolchain_min.
type GoToolchain struct {
	// Required is go.mod's go directive, as it writes it.
	Required string
	// Declared is the Portfile's go.toolchain_min before the update; empty
	// when it declares none.
	Declared string
	Outcome  GoToolchainOutcome
}

// GoToolchainOutcome is what an update did about go.mod's requirement.
type GoToolchainOutcome string

const (
	// GoToolchainCovered is a declared minimum already of the required
	// series, which is all the Go PortGroup compares.
	GoToolchainCovered GoToolchainOutcome = "covered"
	// GoToolchainRaised is a declared minimum dockhand raised to the
	// requirement.
	GoToolchainRaised GoToolchainOutcome = "raised"
	// GoToolchainUndeclared is a port that declares no minimum. Declaring
	// one gates the port on systems whose Go is older, which is the
	// maintainer's call.
	GoToolchainUndeclared GoToolchainOutcome = "undeclared"
	// GoToolchainByHand is a minimum below the requirement declared in a
	// way dockhand can't rewrite.
	GoToolchainByHand GoToolchainOutcome = "by hand"
)

// Unmet reports a requirement the Portfile's minimum doesn't meet after the
// update. A passing build can't catch it, since the builder's Go is new
// enough, so it's a person's to look at.
func (t GoToolchain) Unmet() bool {
	return t.Outcome == GoToolchainUndeclared || t.Outcome == GoToolchainByHand
}

// report records a fidelity report and makes its evaluated snapshot the
// prepared result.
// PortBefore is a port as the edit found it: the first evaluation's, or,
// where nothing was edited, as it stands; false where the result says
// neither.
func (r Result) PortBefore(name string) (macports.PortInfo, bool) {
	if len(r.Fidelity) > 0 {
		port, ok := r.Fidelity[0].Before.Ports[name]
		return port, ok
	}
	if r.Unchanged != nil && r.Unchanged.Name == name {
		return *r.Unchanged, true
	}
	return macports.PortInfo{}, false
}

// PortAfter is a port as the edit leaves it: the prepared files'
// evaluation, which the last fidelity report's is, or, where nothing was
// edited, as it stands; false where the result says neither. Its readers
// needn't know which of those the result holds (the helper-ownership
// review's table).
func (r Result) PortAfter(name string) (macports.PortInfo, bool) {
	if port, ok := r.Prepared.Ports[name]; ok {
		return port, true
	}
	if len(r.Fidelity) > 0 {
		port, ok := r.Fidelity[len(r.Fidelity)-1].After.Ports[name]
		return port, ok
	}
	if r.Unchanged != nil && r.Unchanged.Name == name {
		return *r.Unchanged, true
	}
	return macports.PortInfo{}, false
}

func (r *Result) report(report Fidelity) {
	r.Fidelity = append(r.Fidelity, report)
	r.Prepared = report.After
}

type Service struct {
	DependencyTools depblock.Tools
	Ports           macports.Evaluator
	// Archives fetches source archives; its zero value downloads with the
	// default client and limits.
	Archives distfetch.Client
	// Manifests reads a manifest from the resolved release's repository, for
	// a git-fetched port that downloads no archive to read it from; nil
	// leaves such a port's toolchain minimum as declared.
	Manifests ManifestSource
	// PyPI says which files a PyPI release publishes, where its source
	// archive can't be had; its zero value asks PyPI.
	PyPI pypi.Client
}

// ManifestSource reads one file of a port's source repository at the
// resolved release. An absent file reports macports.ErrManifestMissing.
type ManifestSource interface {
	Manifest(ctx context.Context, port macports.PortInfo, release model.Release, path string) ([]byte, error)
}

func (s *Service) Prepare(ctx context.Context, request Request) (_ Result, err error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	input, err := s.load(ctx, &request)
	if err != nil {
		return Result{}, err
	}
	defer func() { err = errors.Join(err, input.Close()) }()
	result, err := s.prepare(ctx, request, input)
	if err == nil && len(result.Files) == 0 {
		port := input.info
		result.Unchanged = &port
	}
	return result, err
}

func (s *Service) prepare(ctx context.Context, request Request, input *sourceInput) (Result, error) {
	if request.Action == model.EditUpdate {
		return s.prepareVersion(ctx, request, input)
	}
	if request.Action == model.EditChecksums {
		return s.prepareChecksums(ctx, request, input)
	}
	revised, err := portfile.BumpRevision(input.data, input.target.Subport, input.info.Revision)
	if err != nil {
		return Result{}, err
	}
	evaluated, err := s.evaluateEdit(ctx, input, revised)
	if err != nil {
		return Result{}, err
	}
	result := Result{Base: request.Source, Target: input.target}
	family, err := input.familySnapshot(ctx, s.Ports)
	if err != nil {
		return Result{}, err
	}
	err = result.commitEdit(input, request, evaluated.edit, fidelity.Revision(family, evaluated.after, input.target.Name), "revbump")
	return result, err
}

func (request Request) Validate() error {
	if request.Action != model.EditRevbump && request.Action != model.EditUpdate && request.Action != model.EditChecksums {
		return fmt.Errorf("%w: %s", ErrNotImplemented, request.Action)
	}
	if request.Action == model.EditUpdate && request.Release == nil {
		return fmt.Errorf("portedit: a resolved release is required")
	}
	if request.Action != model.EditUpdate && request.Version != "" {
		return fmt.Errorf("portedit: an explicit version applies only to bump")
	}
	if request.Action == model.EditRevbump && strings.TrimSpace(request.Subject) == "" {
		return fmt.Errorf("portedit: a revision bump needs a subject saying why")
	}
	return nil
}

// commitEdit records one evaluated edit as the result's single commit unless
// fidelity found unexpected changes. Files and fidelity are recorded either
// way so callers can report what was attempted.
func (r *Result) commitEdit(input *sourceInput, request Request, edit portfile.Edit, report Fidelity, subject string) error {
	r.Files = []portfile.Edit{edit}
	r.report(report)
	if len(report.UnexpectedChanges) > 0 {
		return fmt.Errorf("%w: %v", ErrFidelity, report.UnexpectedChanges)
	}
	name := input.target.Name
	if request.Stub != "" {
		name = request.Stub
	}
	if request.Subject != "" {
		subject = request.Subject
	}
	line, err := commitmsg.Subject(name, subject)
	if err != nil {
		return err
	}
	r.Commits = []CommitIntent{{Subject: line}}
	return nil
}
