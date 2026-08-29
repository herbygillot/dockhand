package upstream

import (
	"context"
	"log/slog"

	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
)

// Check runs both resolvers for one evaluation context and judges
// their testimony. The livecheck resolver is the port's own livecheck
// phase, driven whole; style is the port's located version carrier,
// which decides whether a git forge exists to ask.
func Check(ctx context.Context, ev *eval.Evaluator, f *portfetch.Fetcher, portdir, subport string, style portstyle.Type) (Report, error) {
	names := append([]string{"livecheck.version"}, CoordOptions(style)...)
	opts, err := ev.Options(ctx, portdir, subport, "", names...)
	if err != nil {
		return Report{}, err
	}

	lc, err := f.Livecheck(ctx, portdir, subport)
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
		obs.Livecheck = lc.Version
	case lc.UpToDate:
		// Up to date means livecheck found exactly the version it was
		// checking against.
		obs.Livecheck = opts["livecheck.version"]
	case lc.NoMatch:
		// Ran and matched nothing: the rot signal. Livecheck stays
		// empty.
	}

	if repo, ok := Coords(style, opts); ok {
		versions, err := Tags(ctx, repo)
		if err != nil {
			return Report{}, err
		}
		slog.Debug("forge tags", "forge", repo.Forge.Name, "url", repo.URL, "versions", len(versions))
		obs.ForgeVersions = versions
	}

	slog.Debug("upstream observation", "livecheck", obs.Livecheck, "disabled", obs.LivecheckDisabled, "forgeVersions", len(obs.ForgeVersions))
	return Judge(obs, func(a, b string) (int, error) {
		return f.Vercmp(ctx, a, b)
	})
}
