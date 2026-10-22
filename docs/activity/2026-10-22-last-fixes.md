# 2026-10-22: v0.5.0's last fixes

Five of v0.4.0's known issues, closed before v0.5.0's acceptance, at
Herby's "Is there anything else you think that could be jammed into
v0.5.0?" (2026-10-22 21:33Z), as prioritization placed them. Each README
row says it's fixed on main.

## A Go module that moved hosts is said

- The upstream assessment reads each side's go.mod module path
  (`assess.moduleMoved`, beside the toolchain rule's go.mod reading); where
  the new version's differs from the old one's, it's a finding held for a
  look, `go-module-moved`, naming both, and the Portfile's `go.package`
  where it's still the old path: "upstream: go.mod's module moved from
  github.com/kevinschoon/pomo to codeberg.org/kevinschoon/pomo; the
  Portfile's go.package names the old path". `holds.allow` can list it,
  as `assess.Rules` gives it.

## checksums says a hand-edited version left its vendored lists behind

- A checksum refresh of a port whose version was edited by hand since its
  base, and whose `cargo.crates` or `go.vendors` are still the base's,
  changes nothing and says the way on (portedit `staleVendored`, before
  the refresh): "…'s version was edited from 1.2.3 to 1.2.4, and its
  cargo.crates are still 1.2.3's, which checksums doesn't refresh; nothing
  was changed. Undo the version edit, then dockhand update … 1.2.4
  regenerates them". The rule's own test reads it; the base's evaluation
  the refresh is given (`StealthRequest.Base`) says the old version and
  list.

## A refused fetch is tried again a day later

- A revision's assessment recorded as unable to compare because a server
  refused an archive, as a 404 does (`refusal.UpstreamMissing`), is made
  again at the first look a day after it was recorded
  (`engine.lookAgain`, `refusedAgainAfter`), and holds until then, where it
  held until a file was edited; a transient problem is made again at once,
  as before.

## A patch changed under its own name is said

- The submit plan names each patch the branch changed under its own name,
  one the base had, in a port's files directory (`SubmitPlan.ChangedPatches`,
  `changedPatches`): "Patches  textproc/jq/files/patch-docs.diff changed;
  dockhand diff shows it", a notice holding nothing, since the upstream
  assessment reads the base's source for it and a check applies it.
  `submit --json` gives `changed_patches`.

## create exits for a look where a draft has no checksums

- A new port whose checksums couldn't be made says the whole problem, the
  rows a Go module before go 1.17 needs by hand with their locks and `port
  -v checksum` as the way to their checksums, where it said its first line,
  and exits 3 with "Next: resolve its checksums as said above, then
  dockhand checksums <port>", where it exited 0.

The README marks each of the five rows fixed on `main`; the others stay,
as prioritization's note says.

## v0.5.0-rc1's Rust regression

- **Every Rust update that lists its crates refused**, "[dust.dist_subdir
  changed]" (Prime-time's quick stage on rc1, rows B3, C2, and C3: dust,
  pgdog, and doge, which v0.4.0 planned). The Cargo PortGroup sets
  `dist_subdir` to cargo-crates where a port lists crates, and to its name
  otherwise; the version step evaluates the Portfile with its crates set
  aside, so its prediction read the port's name, and strict fidelity's
  crate step (part 3, item 3) took that prediction whole, finding the
  evaluated cargo-crates unexpected. The crate step now takes `dist_subdir`
  from the evaluation where the port lists crates, with the blocks and the
  fields the PortGroups fetch for them (portedit/dependencies.go).
- `TestCargoDependencyPreparation`'s new `crates-dist-subdir` scenario has
  cargo_fetch's own default; it failed with the regression and passes with
  the fix. dust, pgdog, and doge plan again from main in a scratch clone at
  the quick stage's pin, 026b3878.
- **The harness gap**: rows C2 and C3 graded the refusal as refused well.
  C2 now fails a refusal that names no crate or pin, and C3 fails where
  doge, whose archive ships its Cargo.lock, is refused, since v0.4.0
  planned both.
