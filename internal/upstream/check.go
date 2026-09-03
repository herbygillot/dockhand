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
//
// m is the politeness every witness here is consulted under, and its
// zero value is the single port asking one question: unpaced,
// uncached, git's own user agent. That is what a bump of one port has
// always done and it stays exactly that. A bump over a selector hands
// in a real one, because a thousand ports resolving "latest" ask a
// forge the same thousand questions a report does — and a report that
// paced itself while the write verb beside it did not would be
// politeness in name only.
func Check(ctx context.Context, tools *tool.Finder, h port.Handle, f *portfetch.Fetcher, style portstyle.Type, declared info.Livecheck, gh GhRunner, m Manners) (Report, error) {
	// Only the forge coordinates need a read of their own: which option
	// names to ask for depends on the carrier style, so they cannot live
	// in a struct. The livecheck configuration came with the values the
	// caller already evaluated.
	opts, err := h.Options(ctx, coordOptions(style)...)
	if err != nil {
		return Report{}, err
	}

	lc, _, err := m.livecheck(ctx, f, h.Target.Portdir, h.Target.Subport, declared.Version)
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
	var refs []Ref
	if onAForge {
		var versions []string
		var authoritative bool
		versions, authoritative, refs, err = observeForge(ctx, tools, gh, repo, m)
		if err != nil {
			return Report{}, err
		}
		obs.ForgeVersions, obs.Authoritative = versions, authoritative
	}

	slog.Debug("upstream observation", "livecheck", obs.Livecheck, "disabled", obs.LivecheckDisabled, "forgeVersions", len(obs.ForgeVersions))
	report := Judge(obs)
	if onAForge && report.Verdict == LivecheckAhead && obs.Authoritative && len(refs) > 0 {
		// Livecheck outran the releases feed, which only speaks for
		// tags upstream blessed as releases — a version tagged but
		// never released (the gopass satellite repos) is invisible to
		// it. The tags themselves are the second witness, and they are
		// already in hand: observeForge reads them first, so the
		// corroboration that used to cost a second ls-remote now costs
		// nothing at all.
		report = corroborate(report, Versions(refs))
	}
	return report, nil
}

// observeForge asks the forge what versions exist, reports whether the
// answer is authoritative, and hands back the tags it read.
//
// Two witnesses, and which of them the verdict is reached over is
// unchanged: releases REPLACE tags where a repository publishes them,
// because a repository that publishes them has said which of its tags
// it means and that is a statement no heuristic can improve on. Never a
// supplement, because a project that publishes releases and also
// carries build tags would otherwise have its own judgment diluted by
// ours.
//
// What changed is which is ASKED first, and only that. The cheap
// witness leads: ls-remote is unauthenticated and unmetered, it yields
// the digest the releases observation is keyed on — so a forge that has
// not moved costs a conditional request rather than a body — and its
// tags are what the LivecheckAhead corroboration needs, which used to
// be a second round trip. The metered, authenticated call is the one
// worth spending a cheap request to make cheaper.
//
// Authoritative travels with the releases and not with the tags, and it
// means exactly one thing: upstream said so. It is not a claim that the
// list is right. The name heuristic still judges every entry, because
// upstream's flags are a filter and not a verdict — mergestat marks
// betas as releases — so an authoritative list of the wrong versions is
// still the wrong versions.
//
// A tags failure is returned only when the releases feed did not answer
// either. There is no third witness under the two, so continuing with
// neither would mean judging the port on the livecheck alone while
// reporting a verdict that reads as if both had spoken — but a
// repository whose git protocol is blocked and whose API is not is
// still fully witnessed, and it was answered before this order changed.
func observeForge(ctx context.Context, tools *tool.Finder, gh GhRunner, repo Repo, m Manners) ([]string, bool, []Ref, error) {
	refs, digest, _, tagErr := m.refs(ctx, tools, repo)
	versions, _, relErr := m.releases(ctx, gh, repo, digest)
	if relErr != nil {
		// Not fatal: the tags stand. Said out loud because the fallback
		// is the heuristic the feed exists to correct, and a run that
		// lost its authoritative witness must not be indistinguishable
		// from one whose repository publishes no releases.
		slog.Debug("releases witness unavailable", "forge", repo.Forge.Name, "url", repo.URL, "err", relErr)
	}
	if len(versions) > 0 {
		slog.Debug("forge releases", "forge", repo.Forge.Name, "url", repo.URL, "versions", len(versions))
		return versions, true, refs, nil
	}
	if tagErr != nil {
		return nil, false, nil, tagErr
	}
	slog.Debug("forge tags", "forge", repo.Forge.Name, "url", repo.URL, "versions", len(refs))
	return Versions(refs), false, refs, nil
}
