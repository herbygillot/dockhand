# 2026-09-29: what the old manifest couldn't be read for holds

The private-helper review's follow-up ([review](../reviews/2026-09-28-private-helper-follow-up.md), [reconciliation](2026-09-29-private-helper-follow-up-reconciled.md)) found the one gap left in the source comparison. A manifest's reading names what it couldn't follow, such as a requirements.txt that reads another file, or a pyproject.toml whose dependencies are dynamic, from another file. Only the new version's were said. A gap in the old version alone compared as nothing: a requirements.txt that read base.txt and now holds only a comment, or dependencies no longer dynamic and not declared either. Nothing held, though what changed was unknown.

Now the old version's gaps hold too, as D4 has it, named for their version: "upstream's requirements.txt in the old version reads base.txt too, which the comparison doesn't follow". A gap both versions share is said once, as before, and different ones are each said. The fix stays in `sourcecompare`'s own `reading`, as the review suggested.

`TestWhatTheComparisonCouldntReadHolds` has the review's two cases, a gap both versions share, and a different one in each. Five mutations each fail it.
