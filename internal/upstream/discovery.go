package upstream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portsource"
	"github.com/herbygillot/dockhand/internal/model"
)

type Assessment string

const (
	Unknown         Assessment = "unknown"
	Current         Assessment = "current"
	UpdateAvailable Assessment = "update-available"
	// Uncertain is a port discovery can't call current or outdated: a
	// version that compares newer than the port's own was set aside
	// (Result.SetAside), and nothing newer is found beyond it. Nothing is
	// selected; a person looks, and names the version to update to.
	Uncertain Assessment = "uncertain"
	// Moved is a port that tracks a branch, whose branch names a newer
	// commit than the one it pins: it's behind, and the version to name
	// for that commit is a person's (Head).
	Moved Assessment = "moved"
	// OwnVersion is a port with no release to look for: it fetches
	// nothing, and its livecheck reads no version, as a _select port's or
	// a metaport's. Its version is MacPorts' own, so it's covered, and
	// neither current nor outdated.
	OwnVersion Assessment = "own-version"
)

// ErrOwnVersion is Resolve's answer for a port whose discovery is
// OwnVersion: there's no release to choose.
var ErrOwnVersion = errors.New("upstream: no release to look for")

// Head is what a port tracking a branch is checked against: the branch,
// HEAD where it names none, and the commit it names now.
type Head struct {
	Branch, Commit string
}

// SetAside is a version that compares newer than the port's own, set aside
// because its tag's commit was made before the commit of the tag the port
// follows now. An old tag oddly spelled is one, as dolt's v040.15 is; so is
// a release made on a branch, a tag made late, or a commit dated wrong, and
// none of that tells them apart.
type SetAside struct {
	Tag string
	// Version is the port's version at the tag, as the Portfile evaluates
	// it, and Source the version as the tag spells it, which an update
	// names to take it: dockhand update <port> <Source>.
	Version, Source string
	// Predates is the tag the port follows now, whose commit this tag's
	// predates.
	Predates string
}

// UncertainError is Resolve's answer for a port whose discovery is
// Uncertain: an update chooses no release there, and needs one named.
type UncertainError struct {
	Port     string
	SetAside []SetAside
}

func (e *UncertainError) Error() string {
	var tags []string
	for _, aside := range e.SetAside {
		tags = append(tags, aside.Tag)
	}
	return fmt.Sprintf("upstream: can't tell whether %s is current: %s compares newer, but predates %s; name the version to update to", e.Port, strings.Join(tags, ", "), e.SetAside[0].Predates)
}

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
	// Release is the release selected, current or newer; nil where the
	// assessment is Unknown or Uncertain.
	Release          *model.Release
	CurrentVersion   string
	CandidateVersion string
	Assessment       Assessment
	// SetAside are the versions that compare newer than the one selected
	// but predate the port's own release, newest first. With none selected
	// beyond them, the assessment is Uncertain, and CandidateVersion is the
	// newest of them.
	SetAside []SetAside
	// Head is the branch a port tracks and the commit it names now, for a
	// port that tracks a branch; nil for any other.
	Head       *Head
	Evidence   []Observation
	Detail     string
	ObservedAt time.Time
}

type Service struct {
	HTTP            *http.Client
	Ports           macports.Reader
	Catalogs        map[portsource.Forge]Catalog
	Versions        versionSelector
	EvaluateVersion func(context.Context, string) (string, error)
	// EvaluateVersions evaluates several source versions in one pass when the
	// bound probe supports it; discovery falls back to EvaluateVersion otherwise.
	EvaluateVersions func(context.Context, []string) ([]string, error)
	// Git is the git executable a port tracking a branch is read with; git
	// on PATH when empty.
	Git string
}

func (s *Service) repository(port macports.PortInfo, automatic bool) (portsource.Spec, forge.Repository, error) {
	var spec portsource.Spec
	var err error
	purpose := portsource.Edit
	if automatic {
		purpose = portsource.Discovery
	}
	spec, err = portsource.Interpret(port, purpose)
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
