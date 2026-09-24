# 2026-09-24: each implementation states its optional capabilities

Step 1 of the roadmap's Next; the contracts review's "Optional
capabilities" and item 3 of the direction record's agenda. Ten
capabilities are found by a type assertion where they are used, so the
compiler checks none of them, and an implementation that lost a method
would lose the feature quietly: `--trace` would say the log is
unavailable, retention would skip a provider.

Each implementation now has a test that asserts what it satisfies and
says what goes without it:

- `verify/tart`: `verify.LogReader`, `verify.ArtifactPruner`,
  `choice.LocalImages`, and its machine's guest log reader.
- `verify/github`: `verify.LogReader`, `verify.LogCachePruner`.
- `forge/github`: the client's `forge.PullRequestInspector` and
  `forge.Documents`; the repository's `forge.ReleaseRepository` and
  `forge.FileRepository`.
- `forge/gitlab`: `forge.FileRepository`, and not
  `forge.ReleaseRepository`, since GitLab offers discovery tags.
- `macports/portedit`: `VersionProbe` is an `upstream.VersionProbe` and an
  `upstream.BatchVersionProbe`.

The three asserted as anonymous or unexported interfaces got names the
tests can hold to the call site: `upstream.BatchVersionProbe` (was an
inline interface in `Bind`), `forge.Documents` (was `upstream`'s
unexported `documents`, now beside the forge's other capabilities), and
`verify/tart`'s `guestLogReader`. No behavior changed.

With them went the CLI's `errNotImplemented`, which only a test's
`NotErrorIs` still referred to.
