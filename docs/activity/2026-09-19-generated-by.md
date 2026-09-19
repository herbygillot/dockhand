# The trailer says Generated-By

The trailer dockhand puts on the commits it mints read `Assisted-By: Dockhand <build> (<url>)`. Dockhand generates those commits whole, so the trailer now says `Generated-By: Dockhand <build> (<url>)`. Both earlier spellings, `Assisted-By: Dockhand` and the original `Generated-by:` line, are still recognized as attribution, so a rewritten message carries one trailer and a pull request body strips all three from the description.
