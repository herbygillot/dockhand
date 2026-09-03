package upstream

import (
	"context"
	"log/slog"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/tool"
)

// Check runs both resolvers for the handle's context and judges their
// testimony. The livecheck resolver is the port's own livecheck phase,
// driven whole; style is the port's located version carrier, which
// decides whether a git forge exists to ask. tools resolves the git
// the tag resolver runs.
func Check(ctx context.Context, tools *tool.Finder, h port.Handle, f *portfetch.Fetcher, style portstyle.Type, declared info.Livecheck, gh GhRunner) (Report, error) {
	// Only the forge coordinates need a read of their own: which option
	// names to ask for depends on the carrier style, so they cannot live
	// in a struct. The livecheck configuration came with the values the
	// caller already evaluated.
	opts, err := h.Options(ctx, coordOptions(style)...)
	if err != nil {
		return Report{}, err
	}

	lc, err := f.Livecheck(ctx, h.Target.Portdir, h.Target.Subport)
	if err != nil {
		// The check could not run (site down, unknown type): an error,
		// never a rot verdict — and an error about upstream rather than
		// about this machine, unless the evaluator underneath is what
		// failed, which Unreachable leaves alone.
		return Report{}, Unreachable("livecheck", err)
	}
	obs := Observation{Current: declared.Version}
	switch {
	case !lc.Ran:
		obs.LivecheckDisabled = true
	case lc.Version != "":
		// The newer version livecheck found.
		obs.Livecheck = lc.Version
	case lc.UpToDate:
		// Up to date means livecheck found exactly the version it was
		// checking against.
		obs.Livecheck = declared.Version
	case lc.NoMatch:
		// Ran and matched nothing: the rot signal. Livecheck stays
		// empty.
	}

	// The coordinates are derived once and carried: the corroboration
	// below needs the same repository, and deriving it twice was a
	// second chance for the two reads to disagree about what port this
	// even is.
	repo, onAForge := coords(style, opts)
	if onAForge {
		versions, authoritative, err := observeForge(ctx, tools, gh, repo)
		if err != nil {
			return Report{}, err
		}
		obs.ForgeVersions, obs.Authoritative = versions, authoritative
	}

	slog.Debug("upstream observation", "livecheck", obs.Livecheck, "disabled", obs.LivecheckDisabled, "forgeVersions", len(obs.ForgeVersions))
	report := Judge(obs)
	if onAForge && report.Verdict == LivecheckAhead && obs.Authoritative {
		// Livecheck outran the releases feed, which only speaks for
		// tags upstream blessed as releases — a version tagged but
		// never released (the gopass satellite repos) is invisible to
		// it. The tags themselves are the second witness; best-effort,
		// because the corroboration is a refinement of a verdict
		// already reached, not a resolver of its own.
		//
		// Authoritative is only ever set from a releases feed, so the
		// tags have not been read yet and this is the first ask, not a
		// second one. The forge test is repeated rather than inferred
		// from that: the repository is what Tags is asked about, and a
		// guard on the thing being used holds whatever a later edit does
		// to where Authoritative is assigned.
		if tags, terr := Tags(ctx, tools, repo); terr == nil {
			report = corroborate(report, tags)
		} else {
			slog.Debug("tag corroboration unavailable", "err", terr)
		}
	}
	return report, nil
}

// observeForge asks the forge what versions exist, and reports whether
// the answer is authoritative.
//
// Two witnesses, consulted in order, and the order is the whole rule.
// Releases first: a repository that publishes them has said which of
// its tags it means, and that is a statement no heuristic can improve
// on. Tags only when there are no releases to read — never as a
// supplement, because a project that publishes releases and also carries
// build tags would otherwise have its own judgment diluted by ours.
//
// Authoritative travels with the releases and not with the tags, and it
// means exactly one thing: upstream said so. It is not a claim that the
// list is right. The name heuristic still judges every entry, because
// upstream's flags are a filter and not a verdict — mergestat marks
// betas as releases — so an authoritative list of the wrong versions is
// still the wrong versions.
//
// A tags failure is returned. There is no third witness under it, so
// continuing would mean judging the port on the livecheck alone while
// reporting a verdict that reads as if both had spoken.
func observeForge(ctx context.Context, tools *tool.Finder, gh GhRunner, repo Repo) ([]string, bool, error) {
	if versions, ok := Releases(ctx, gh, repo); ok {
		slog.Debug("forge releases", "forge", repo.Forge.Name, "url", repo.URL, "versions", len(versions))
		return versions, true, nil
	}
	versions, err := Tags(ctx, tools, repo)
	if err != nil {
		return nil, false, err
	}
	slog.Debug("forge tags", "forge", repo.Forge.Name, "url", repo.URL, "versions", len(versions))
	return versions, false, nil
}
