package portedit

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"net/http"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/record"
)

var (
	ErrProbeInconclusive = errors.New("portedit: version probe is inconclusive")
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
	SharedRelease bool
	Action        record.Action
	Source        record.Source
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
	Scope     *record.ReleaseScope `json:",omitempty"`
	Coverage  []ContextCoverage    `json:",omitempty"`
	Base      record.Source
	Target    record.Target
	Files     []portfile.Edit
	Commits   []CommitIntent
	Fidelity  []Fidelity
	Release   *record.Release
	Downloads []Download
}

type Service struct {
	DependencyTools  dependency.Tools
	Ports            macports.Reader
	HTTP             *http.Client
	MaxDownloadBytes int64
}

func (s *Service) Prepare(ctx context.Context, request Request) (_ Result, err error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	input, err := s.load(ctx, request)
	if err != nil {
		return Result{}, err
	}
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
	err = result.commitEdit(input, request, evaluated.edit, fidelity.Revision(input.before, evaluated.after, input.target.Name, input.files.root), "revbump")
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
