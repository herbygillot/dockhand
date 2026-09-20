# A pseudo-version says its commit once

`dockhand --version` printed `v0.0.0-20260918123022-d3e24659d56a (d3e24659d56a)` for a build made from an untagged commit. The parenthetical exists to name the commit a version was built from, and a Go pseudo-version already ends in it, so the line said it twice. This was on the maintenance list from before tags existed, and a tagged build never showed it, which is why it stayed there.

A version that already ends in its revision no longer repeats it. The check reads the last hyphenated component, after dropping any `+dirty` the toolchain appends for a modified tree, so it recognises a pseudo-version and does not mistake a date-shaped tag such as `v0.0.0-20260919.2` for one; that still names its commit, because the tag is not derived from it. A modified tree keeps its note either way, standing alone as `(modified)` when the revision is already in the version.
