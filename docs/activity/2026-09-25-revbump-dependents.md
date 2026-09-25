# 2026-09-25: update --revbump-dependents and --except

Design v3 §6.7 and decision 27.

## What changed

- **`update <port> --revbump-dependents`** updates the port, then bumps the revision of every port that links it directly, so users rebuild them. Those are its library dependents in the port index at the branch's base, found with `impact`'s `DependentReader`.
  - Build-only and runtime-only dependents are not bumped.
  - Each dependent's commit subject is recorded for tidy as `<port>: rebuild for <updated> <version>`.
  - `--plan` lists the dependents and changes nothing.
  - If one bump fails, the error names that port; the ones before it stay bumped.
- **`Engine.LinkedPorts`** chooses what to bump:
  - It bumps one port per directory, since subports may share a revision line and bumping each would bump the shared one twice.
  - It skips dependents the branch already changes and says so, so a second `update` doesn't bump them again.
  - It removes excepted ports.
- **`--except <port>`** leaves a dependent out, with its whole directory, since its subports may share its revision. It refuses a port that isn't a library dependent, and requires `--revbump-dependents`.

## Tests

- **Engine:**
  - libharbor's library dependents give harbor-cli and one of harbor-viewer's two subports.
  - A changed harbor-cli is left alone.
  - Excepting harbor-viewer drops its subport too, which caught a bug on the way.
  - A build-only dependent is refused by `--except`.
- **Command:**
  - `--except` alone is refused.
  - `--except` of a build-only dependent is refused.
  - `--plan` lists yq and bumps nothing.
  - The real run bumps yq's revision and leaves jo, which only builds with jq.
  - A second update leaves the already-bumped yq alone.

**Not yet:** the design's note that a dependent has another maintainer, whom the PR will mention. That needs each port's maintainers from the index.
