package upstream

import (
	"context"
	"log/slog"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
)

// Check runs both resolvers for the handle's context and judges their
// testimony. The livecheck resolver is the port's own livecheck phase,
// driven whole; style is the port's located version carrier, which
// decides whether a git forge exists to ask.
func Check(ctx context.Context, h port.Handle, f *portfetch.Fetcher, style portstyle.Type, declared info.Livecheck, gh GhRunner) (Report, error) {
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
		// never a rot verdict.
		return Report{}, err
	}
	var obs Observation
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
		// releases has said authoritatively which tags count, and the
		// name heuristic exists only for repos that never say.
		if versions, ok := Releases(ctx, gh, repo); ok {
			slog.Debug("forge releases", "forge", repo.Forge.Name, "url", repo.URL, "versions", len(versions))
			obs.ForgeVersions, obs.Authoritative = versions, true
		} else {
			versions, err := Tags(ctx, repo)
			if err != nil {
				return Report{}, err
			}
			slog.Debug("forge tags", "forge", repo.Forge.Name, "url", repo.URL, "versions", len(versions))
			obs.ForgeVersions = versions
		}
	}

	slog.Debug("upstream observation", "livecheck", obs.Livecheck, "disabled", obs.LivecheckDisabled, "forgeVersions", len(obs.ForgeVersions))
	return Judge(obs), nil
}
