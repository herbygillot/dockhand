package portedit

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"net/http"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/record"
)

var (
	errProbeInconclusive = errors.New("portedit: version probe is inconclusive")
	ErrNotImplemented    = errors.New("portedit: requested transformation is not implemented")
	ErrUnsupported       = portfile.ErrUnsupported
	ErrFidelity          = fidelity.ErrMismatch
)

type CommitIntent struct {
	Subject string
	Body    string
	Paths   []string
}

type Request struct {
	// EditIntent is the person's choices for the edit, as bound or as
	// recorded on the job: a Stub already resolved there is honored rather
	// than resolved again.
	record.EditIntent
	Action record.Action
	Source record.Source
	// Root is an exclusively owned disposable source snapshot, never a user checkout.
	Root      string
	Selection macports.Selection
	Platform  record.Platform
	Version   string
	Reason    string
	Release   *record.Release
}

// Fidelity is the comparison report for one evaluated edit.
type Fidelity = fidelity.Report

// ContextCoverage distinguishes metadata models from the native host. Neither
// kind records a build; verification providers establish build results.
type ContextCoverage struct {
	Fetch    *macports.FetchSemantics `json:",omitempty"`
	Platform record.Platform
	Modeled  bool
	Affected bool
}

type Result struct {
	Scope    *record.ReleaseScope `json:",omitempty"`
	Coverage []ContextCoverage    `json:",omitempty"`
	Base     record.Source
	Target   record.Target
	Files    []portfile.Edit
	Commits  []CommitIntent
	Fidelity []Fidelity
	// Prepared is the evaluated snapshot of the prepared files, the state
	// every later step reads: the go.mod check, dependency regeneration,
	// the patch check, and the adapter's final evaluation. It is set with
	// each fidelity report, so it is the last report's snapshot without
	// any reader having to know that.
	Prepared  macports.Snapshot `json:"-"`
	Release   *record.Release
	Downloads []Download
	// Patches reports whether each declared patch file still applies to the
	// candidate source; a rejected patch is a finding, not a refusal.
	Patches []patchcheck.Result `json:",omitempty"`
}

// report records a fidelity report and makes its evaluated snapshot the
// prepared result.
func (r *Result) report(report Fidelity) {
	r.Fidelity = append(r.Fidelity, report)
	r.Prepared = report.After
}

type Service struct {
	DependencyTools  dependency.Tools
	Ports            macports.Evaluator
	HTTP             *http.Client
	MaxDownloadBytes int64
	// Manifests reads a manifest from the resolved release's repository, for
	// a git-fetched port that downloads no archive to read it from; nil
	// leaves such a port's toolchain minimum as declared.
	Manifests ManifestSource
}

// ManifestSource reads one file of a port's source repository at the
// resolved release. An absent file reports dependency.ErrManifestMissing.
type ManifestSource interface {
	Manifest(ctx context.Context, port macports.PortInfo, release record.Release, path string) ([]byte, error)
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
	if request.Action == record.Bump {
		return s.prepareVersion(ctx, request, input)
	}
	if request.Action == record.RefreshChecksums {
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
	err = result.commitEdit(input, request, evaluated.edit, fidelity.Revision(family, evaluated.after, input.target.Name, input.files.root), "revbump")
	return result, err
}

func (request Request) Validate() error {
	if request.Action != record.BumpRevision && request.Action != record.Bump && request.Action != record.RefreshChecksums {
		return fmt.Errorf("%w: %s", ErrNotImplemented, request.Action)
	}
	if request.Action == record.Bump && request.Release == nil {
		return fmt.Errorf("portedit: a resolved release is required")
	}
	if request.Action != record.Bump && request.Version != "" {
		return fmt.Errorf("portedit: an explicit version applies only to bump")
	}
	return nil
}
