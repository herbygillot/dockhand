# v0.5.0 part 1, item 2: the source a port fetches, compared whole (CX-2)

Codex's review of 832f059e, high 2. `ReleaseStateOf` left out `fetch.type`
and `git.url`, so a port whose repository changed at the same tag, or that
moved from an archive to Git at the same version, read as fetching the
same source: `SameSource` said so, `assessPort` compared nothing upstream,
and `RebindReleaseScope`'s protected-sibling check couldn't see the move.

- `ReleaseState` gains `FetchType` and `GitURL`, read through
  `PortInfo.option`, and `Unknown` where the evaluation couldn't settle
  either; `ReleaseState.Same` is equality where both are known. An option
  the port doesn't set reads as empty on both sides, as before, so ports
  that never set them compare as they did.
- `SameSource` and the protected-sibling check use `Same`.
- assess.Policy is 20, so assessments made under the old comparison are
  made again. The state isn't stored, so nothing migrates.
- `--follow-moves` rewrites github.setup after preparation, outside the
  scope's rebinding, so its legitimate move isn't refused; its tests pass
  unchanged.

TestASourceIsHowAndWhereItFetches: a git.url-only change and a fetch-type
change each read as another source, an unsettled option is never the
same, and a protected sibling whose git.url moved is refused.
