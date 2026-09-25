# 2026-09-24: Golden Gate is Darwin 27

Step 1 of the roadmap's Next. The [Golden Gate report](2026-09-20-golden-gate.md)
added macOS 27 to the release table as Darwin 26, the next number after
Tahoe's 25. A Golden Gate guest reports kernel `Darwin 27.0.0` (build
26A428; [direction record](../reviews/2026-09-23-contracts-direction.md),
"Found along the way"): Apple skipped 26. With the old entry, `--os
golden-gate` asked for a platform no guest reports, a Golden Gate host
found no release for itself, and a modeled context modeled a Darwin that
never shipped.

- `internal/macos/release.go` maps Golden Gate to 27, and says the keys
  are not consecutive. Darwin 26 has no release, no product version, and
  describes itself raw ("darwin 26 arm64").
- One place assumed consecutive numbers. `observe` models both sides of
  a literal Darwin condition in a Portfile by adding the boundary and its
  neighbors, n-1 and n+1, as profiles; on a Golden Gate host, a boundary
  at 26 or 27 would have asked for Darwin 26, whose platform variables do
  not exist. `profilesForBoundaries` now skips a Darwin with no release,
  and the neighbors that exist carry the comparison.
- Tests follow: the Golden Gate release test is on 27 and asserts 26 is
  unknown; the Tart image-name refusal uses 26; the observation profiles
  test covers a Golden Gate host with boundaries at 26 and 27.
