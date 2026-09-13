package upstream

import (
	"context"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

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
	Draft       bool
	Prerelease  bool
	PublishedAt time.Time
}

type ReleaseReader interface {
	Releases(context.Context, string) ([]Release, error)
	ListTags(context.Context, string) ([]Tag, error)
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
	Ports    macports.Reader
	Releases ReleaseReader
	Tags     TagReader
	Versions VersionSelector
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
