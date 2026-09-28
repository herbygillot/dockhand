# 2026-09-27: an update is compared with the archive MacPorts shipped

The person asked whether archives are fetched in a way that matches their `dist_subdir`, in case of name collisions.

**Names can't collide.**
- Each download goes to its own `source-*` file in the store; the distfile's name is only a label on it.
- An update's old and new archives are paired by position.
- `archives.Sources` refuses a Portfile that names one distfile twice.

`dist_subdir` is MacPorts' own namespace, for its distfiles directory and its mirror, and no upstream URL includes it. An update fetches from the Portfile's `master_sites`. Only `diff --archive` reached MacPorts' mirror, and it used the port's `dist_subdir`.

**The gap the question found.** The update fetched the current version's archive, the one it compares the new version with, from upstream, without checking it against the Portfile's checksums. So an upstream archive changed since, the stealth update `dist_subdir` exists for, was compared as though MacPorts had shipped it. For a Go or Cargo port it was also the source of the dependency list checked against the declared one. An archive gone from upstream failed the comparison, which under D4 holds, though MacPorts' mirror has it.

**What changed.**
- **One verified fetch.** `diff --archive`'s fetch moved into the archives package as `Store.Shipped`, with `Declared` and `Differs` from the engine's stealth code. It takes an archive as the Portfile's checksums declare it:
  - from upstream;
  - or, where upstream now serves something else or nothing, from the mirror under the port's `dist_subdir`, its name unless the Portfile sets one.
  - An archive the Portfile declares no checksums for is refused.
  - Legacy md5 or sha1 alone can't tell, so upstream's stands.
- **The mirror is the client's `Mirror`.** The engine sets it, as `archives.MacPortsMirror`, for `diff --archive` and for updates, through `preparation.Service.Mirror`. It is empty in tests, which never reach the real mirror.
- **An update fetches its old archives with it.**
  - Both the archive path and the Go/Cargo path use it.
  - Where neither upstream nor the mirror has an archive as shipped, the update goes on, and its comparison says why, which holds `bump`'s and serve's submission.
  - A Go/Cargo port's dependency check then reads what upstream serves, as it did before.

**Tests.**
- `TestAnUpdateComparesWithTheArchiveMacPortsShipped`: from upstream; from the mirror, at `/fixture/1.0_1/fixture-1.0.tar.gz` for a port whose `dist_subdir` is `fixture/1.0_1`; and from neither.
- `TestGoDependencyPreparation`'s `kept-stealth` and `kept-unshipped` cases.
- `TestShippedTakesOnlyWhatThePortfileDeclares`.
- The first fails without the change, in its mirror and neither cases.
- The preparation fixture declares placeholder checksums, so these tests declare the served archive's with `shippedChecksums`.
