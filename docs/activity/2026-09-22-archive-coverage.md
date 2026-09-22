# 2026-09-22: the archive passes share one coverage object

The fifth part of Next item 1. Three passes walked the observed contexts
with the same bookkeeping written three times: `assessArchives` for an
assessment, `planObservedChecksums` for a checksum refresh, and
`planObservedArchives` for a version edit. Each kept its own maps of the
checksum groups the contexts declared, the groups an archive covered, the
groups a patch file present beside the Portfile made inert, and the
archives to download once by group, name, and locations, and each ended
with the same check that every declared group is covered or inert.

`archiveCoverage` holds that once: `declare` for a context's groups and
the inert ones among them, `cover` for an artifact's group, `download` for
an artifact's one planned download, and `uncovered` for the declared
groups nothing covers, in order. What each pass accepts stays with the
pass, as the review asked: the assessment only reads, the refresh downloads
every archive, and the version edit downloads the changed ones and refuses
a protected one that moved. The three error messages are as they were;
the uncovered group named is now the first in order rather than the first
the map happened to yield.

It is an object inside `portedit`, not a package, which is where the
review said to start: extracting it would mean exporting what it reads
from `sourceInput`, and its callers are the three passes and nothing else.
