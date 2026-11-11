package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/checkplan"
	"github.com/herbygillot/dockhand/internal/macports/binaryarchive"
	"github.com/herbygillot/dockhand/internal/model"
)

// An install estimate's lookups: eight ports' listings at once, each within
// five seconds (the install estimate, 2026-10-29); the catalog keeps each
// a day.
const (
	listingAsks = 8
	listingWait = 5 * time.Second
)

// InstallEstimate is what a check will install in an environment before it
// builds, by the port index: the dependencies its targets reach that the
// environment hasn't installed, and of those, the ones with no archive of
// their version on packages.macports.org for its release. A hint only: the
// clone decides (Decided: saying which build from source).
type InstallEstimate struct {
	Environment model.Environment
	// Dependencies are how many it installs.
	Dependencies int
	// Unarchived are those with no archive for the release, and Unknown
	// those whose listing couldn't be had in time, which leaves the
	// archives unsaid.
	Unarchived, Unknown int
	// Unestimated says why the archives aren't estimated, where a local
	// prefix's variants.conf sets global variants: the builders' archives
	// aren't what it installs.
	Unestimated string
}

// Words are the estimate as a person reads it: "about 14 dependencies to
// install, 3 with no archive on packages.macports.org for this release, by
// the port index; the clone decides".
func (est InstallEstimate) Words() string {
	if est.Dependencies == 0 {
		return "no dependencies to install, by the port index; the clone decides"
	}
	words := "about 1 dependency to install"
	if est.Dependencies != 1 {
		words = fmt.Sprintf("about %d dependencies to install", est.Dependencies)
	}
	switch {
	case est.Unestimated != "":
		words += " (" + est.Unestimated + ")"
	case est.Unknown == 0:
		words += fmt.Sprintf(", %d with no archive on packages.macports.org for this release", est.Unarchived)
	}
	return words + ", by the port index; the clone decides"
}

// ArchiveVersionReader reads every indexed port's version and revision as
// an archive's name writes them (treequery.Reader.ArchiveVersions).
type ArchiveVersionReader interface {
	ArchiveVersions(ctx context.Context, source model.Source) (map[string]string, error)
}

// InstallEstimates are what a plan's check will install in each of its
// environments, by the port index of its source: the closure of its
// targets' dependencies (checkplan.Closure), less what the environment has
// installed, none in Tart's or GitHub's, which start from MacPorts with no
// ports by their setup, and the person's active ports in a local prefix;
// a command provider's environments get none. wait has the listings looked
// up, as check --plan does; otherwise only what's kept is read, and the
// archives are said where all of them are. Nothing where the index can't
// be read.
func (e *Engine) InstallEstimates(ctx context.Context, plan model.Plan, source model.Source, wait bool) []InstallEstimate {
	capabilities, err := e.capabilities()
	if err != nil || capabilities.Graphs == nil {
		return nil
	}
	versionReader, ok := capabilities.Graphs.(ArchiveVersionReader)
	if !ok {
		return nil
	}
	graph, err := capabilities.Graphs.DependencyGraph(ctx, source)
	if err != nil {
		return nil
	}
	versions, err := versionReader.ArchiveVersions(ctx, source)
	if err != nil {
		return nil
	}
	var roots []string
	built := map[string]bool{}
	for _, target := range plan.Targets {
		name := strings.ToLower(target.Target.Name)
		roots = append(roots, name)
		built[name] = true
	}
	var estimates []InstallEstimate
	for _, environment := range plan.Environments {
		except := map[string]bool{}
		for name := range built {
			except[name] = true
		}
		var variants []string
		switch provider := e.Providers[environment.Provider].(type) {
		case buildenv.InstalledProvider:
			active, set, err := provider.Installed(ctx, environment)
			if err != nil {
				continue
			}
			for _, name := range active {
				except[strings.ToLower(name)] = true
			}
			variants = set
		default:
			if environment.Provider != buildenv.Tart && environment.Provider != buildenv.GitHub {
				continue
			}
		}
		closure := checkplan.Closure(graph, roots, except)
		estimate := InstallEstimate{Environment: environment, Dependencies: len(closure)}
		darwin, err := strconv.Atoi(environment.Platform.Version)
		switch {
		case len(variants) > 0:
			estimate.Unestimated = "archives not estimated: variants.conf sets " + strings.Join(variants, " ")
		case err != nil || environment.Platform.Architecture == "":
			estimate.Unknown = len(closure)
		default:
			estimate.Unarchived, estimate.Unknown = e.unarchived(ctx, closure, versions, darwin, environment.Platform.Architecture, wait)
		}
		estimates = append(estimates, estimate)
	}
	return estimates
}

// unarchived counts the ports with no archive of their version for a
// release, by their listings in the archive catalog, and those whose
// listing it couldn't have: eight asked of the site at once where wait,
// each within listingWait; only those kept otherwise.
func (e *Engine) unarchived(ctx context.Context, ports []string, versions map[string]string, darwin int, arch string, wait bool) (none, unknown int) {
	var mu sync.Mutex
	var group sync.WaitGroup
	slots := make(chan struct{}, listingAsks)
	for _, port := range ports {
		// A version may have an underscore of its own; the revision is
		// after the last.
		versionRevision := versions[port]
		i := strings.LastIndex(versionRevision, "_")
		if i < 0 {
			unknown++
			continue
		}
		version, revision := versionRevision[:i], versionRevision[i+1:]
		group.Add(1)
		go func() {
			defer group.Done()
			listing, availability := e.Catalog.Listing(ctx, port, false)
			if availability == binaryarchive.Unavailable && wait {
				slots <- struct{}{}
				asking, cancel := context.WithTimeout(ctx, listingWait)
				listing, availability = e.Catalog.Listing(asking, port, true)
				cancel()
				<-slots
			}
			mu.Lock()
			defer mu.Unlock()
			switch {
			case availability == binaryarchive.Unavailable:
				unknown++
			case !binaryarchive.Listed(listing, port, version, revision, darwin, arch):
				none++
			}
		}()
	}
	group.Wait()
	return none, unknown
}
