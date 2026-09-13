package upstream

import (
	"context"
	"errors"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/macports"
)

var ErrNotImplemented = errors.New("upstream: discovery is not implemented")

type Assessment string

const (
	Unknown         Assessment = "unknown"
	Current         Assessment = "current"
	UpdateAvailable Assessment = "update-available"
)

type Release struct {
	Version     string
	Tag         string
	URL         string
	Prerelease  bool
	PublishedAt time.Time
}

type ReleaseReader interface {
	Releases(context.Context, string) ([]Release, error)
}

type Observation struct {
	Source     string
	Version    string
	URL        string
	ObservedAt time.Time
	Error      string
}

type Result struct {
	CurrentVersion   string
	CandidateVersion string
	Assessment       Assessment
	Evidence         []Observation
	Detail           string
	ObservedAt       time.Time
}

type Service struct {
	Ports    macports.Reader
	Releases ReleaseReader
	Tags     TagReader
}

func (s *Service) Discover(ctx context.Context, source macports.Context) (Result, error) {
	return Result{Assessment: Unknown}, ErrNotImplemented
}
