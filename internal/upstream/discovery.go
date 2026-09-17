package upstream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
	HTTP            *http.Client
	Ports           macports.Reader
	Catalogs        map[portsource.Forge]Catalog
	Versions        VersionSelector
	EvaluateVersion func(context.Context, string) (string, error)
	// EvaluateVersions evaluates several source versions in one pass when the
	// bound probe supports it; discovery falls back to EvaluateVersion otherwise.
	EvaluateVersions func(context.Context, []string) ([]string, error)
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
			return spec, nil, fmt.Errorf("%w: %v", errAutomaticUnsupported, err)
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
