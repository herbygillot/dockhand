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
