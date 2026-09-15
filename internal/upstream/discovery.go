// Package upstream selects and checks releases from interpreted port sources.
// Forge adapters supply repository facts; this package owns eligibility and the
// selected source returned to preparation.
package upstream

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
	"github.com/herbygillot/dockhand/internal/record"
)

type Assessment string

const (
	Unknown         Assessment = "unknown"
	Current         Assessment = "current"
	UpdateAvailable Assessment = "update-available"
)

// Catalog binds an interpreted Portfile source to its remote repository.
type Catalog interface {
	Repository(instance, name string) (forge.Repository, error)
}

type Observation struct {
	Source     string
	Version    string
	URL        string
	ObservedAt time.Time
	Error      string
}

type Result struct {
	Release          *record.Release
	CurrentVersion   string
	CandidateVersion string
	Assessment       Assessment
	Evidence         []Observation
	Detail           string
	ObservedAt       time.Time
}

type Service struct {
	Ports           macports.Reader
	Catalogs        map[portsource.Forge]Catalog
	Versions        VersionSelector
	EvaluateVersion func(context.Context, string) (string, error)
}

func (s *Service) Discover(ctx context.Context, source macports.Context) (Result, error) {
	if s == nil || s.Ports == nil {
		return Result{Assessment: Unknown}, fmt.Errorf("upstream: port reader is required")
	}
	snapshot, err := s.Ports.Evaluate(ctx, source)
	if err != nil {
		return Result{Assessment: Unknown}, err
	}
	info, ok := snapshot.Ports[source.Target().Name]
	if !ok {
		return Result{Assessment: Unknown}, fmt.Errorf("upstream: selected port was not evaluated")
	}
	return s.DiscoverPort(ctx, info)
}

func (s *Service) repository(port macports.PortInfo, automatic bool) (portsource.Spec, forge.Repository, error) {
	var spec portsource.Spec
	var err error
	if automatic {
		spec, err = portsource.Discover(port)
	} else {
		spec, err = portsource.Interpret(port)
	}
	if err != nil {
		if automatic && errors.Is(err, portsource.ErrUnsupported) {
			return spec, nil, fmt.Errorf("%w: %v", ErrAutomaticUnsupported, err)
		}
		return spec, nil, err
	}
	if s == nil || s.Catalogs == nil {
		return spec, nil, fmt.Errorf("upstream: source catalogs are required")
	}
	catalog := s.Catalogs[spec.Forge]
	if catalog == nil {
		return spec, nil, fmt.Errorf("upstream: no catalog supports %s", spec.Forge)
	}
	repository, err := catalog.Repository(spec.Instance, spec.Repository)
	if err != nil {
		return spec, nil, err
	}
	if repository == nil || repository.Name() != spec.Repository {
		return spec, nil, fmt.Errorf("upstream: catalog returned a different source")
	}
	return spec, repository, nil
}
