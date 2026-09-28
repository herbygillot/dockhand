package portedit

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/model"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
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
	Modeled  bool
	Affected bool
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
	Downloads []archives.Download
	// Crates are the archives of the Git-pinned crates a Cargo update
	// fetched for their checksums: dependencies, not the port's source, so
	// nothing among Previous is theirs to compare with.
	Crates []archives.Download `json:",omitempty"`
	// Previous are the current version's archives, fetched only when
	// KeepArchives asked, and PreviousProblem why they could not be.
	Previous        []archives.Download `json:"-"`
	PreviousProblem string              `json:",omitempty"`
	// Patches reports whether each declared patch file still applies to the
	// candidate source; a rejected patch is a finding, not a refusal.
	Patches []patchcheck.Result `json:",omitempty"`
	// GoToolchain is what go.mod requires where the Portfile's
	// go.toolchain_min was left below it; nil where it was raised, or
	// needn't be.
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

// GoToolchain is a Go release a module-mode port's go.mod requires that its
// go.toolchain_min doesn't: undeclared, since declaring one gates the port
// on systems whose Go is older, which is the maintainer's call, or declared
// in a way dockhand can't rewrite. A passing build can't catch either: the
// builder's Go is new enough.
type GoToolchain struct {
	Required string
	// Declared is the Portfile's go.toolchain_min; empty when it declares
	// none.
	Declared string
}

// report records a fidelity report and makes its evaluated snapshot the
// prepared result.
func (r *Result) report(report Fidelity) {
	r.Fidelity = append(r.Fidelity, report)
	r.Prepared = report.After
}

type Service struct {
	DependencyTools dependency.Tools
	Ports           macports.Evaluator
	// Archives fetches source archives; its zero value downloads with the
	// default client and limits.
	Archives archives.Client
	// Manifests reads a manifest from the resolved release's repository, for
	// a git-fetched port that downloads no archive to read it from; nil
	// leaves such a port's toolchain minimum as declared.
	Manifests ManifestSource
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
