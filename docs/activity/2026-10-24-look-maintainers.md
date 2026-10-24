# 2026-10-24: serve's look is shown only for the maintainer it looked for

The M1's rc5 run: with no `maintainer` in dhtest's config, `dockhand
status` still said "Your ports: 123 ports have newer releases, as serve
found 3d ago", a look from 10-21 under an earlier maintainer.
Prioritization placed the fix in 4b9fbfef.

- **The look records whom it looked for:** `OutdatedLook.Maintainers`,
  serve's maintainers when it looked.
- **One rule, in the reader:** `LastOutdatedLook(maintainers)` returns a
  look only where the recorded maintainers are the config's, compared as
  sets. A look that records none, as v0.4.0 and v0.5.0 wrote, is no one's.
  status reads it with `engine.Engine.Maintainers`, which compose sets from
  the config's `maintainer`. serve's notice reads it with its own.
- **A day with no maintainer isn't stamped:** a serve restarted that day
  with a maintainer set looks that day, not the next.
- **Test:** TestALookShowsOnlyForTheMaintainersItLookedFor. A look for
  `@ada example.org:ada` shows under the same two in another order, and not
  under none or @bob. A look recording none shows nowhere. serve, after its
  no-maintainer hint, looks the same day once restarted with a maintainer.
