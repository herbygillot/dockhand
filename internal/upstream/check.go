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

	if repo, ok := coords(style, opts); ok {
		// Releases first, tags as the fallback: a repo that publishes
		// releases has said authoritatively which tags count. The name
		// heuristic still judges what remains — upstream flags are a
		// filter, not a verdict (mergestat marks betas as releases).
		if versions, ok := Releases(ctx, gh, repo); ok {
			slog.Debug("forge releases", "forge", repo.Forge.Name, "url", repo.URL, "versions", len(versions))
			obs.ForgeVersions, obs.Authoritative = versions, true
		} else {
			versions, err := Tags(ctx, tools, repo)
			if err != nil {
				return Report{}, err
			}
			slog.Debug("forge tags", "forge", repo.Forge.Name, "url", repo.URL, "versions", len(versions))
			obs.ForgeVersions = versions
		}
	}

	slog.Debug("upstream observation", "livecheck", obs.Livecheck, "disabled", obs.LivecheckDisabled, "forgeVersions", len(obs.ForgeVersions))
	report := Judge(obs)
	if report.Verdict == LivecheckAhead && obs.Authoritative {
		// Livecheck outran the releases feed, which only speaks for
		// tags upstream blessed as releases — a version tagged but
		// never released (the gopass satellite repos) is invisible to
		// it. The tags themselves are the second witness; best-effort,
		// because the corroboration is a refinement of a verdict
		// already reached, not a resolver of its own.
		if repo, ok := coords(style, opts); ok {
			if tags, terr := Tags(ctx, tools, repo); terr == nil {
				report = corroborate(report, tags)
			} else {
				slog.Debug("tag corroboration unavailable", "err", terr)
			}
		}
	}
	return report, nil
}
