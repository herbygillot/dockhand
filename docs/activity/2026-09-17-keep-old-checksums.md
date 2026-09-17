# `--keep-old-checksums`

Asked for on 2026-09-17, right after legacy checksum blocks started being rewritten: a way to keep the old algorithms.

## The flag

`--keep-old-checksums` on `bump` and `refresh-checksums` keeps a legacy checksum group exactly as written, the same algorithms in the same layout, and refreshes every value it names from the download. The downloader now computes md5 and sha1 beside sha256, rmd160, and size, so an `md5 sha1 rmd160` block gets three fresh values in its three slots. Without the flag the group is rewritten as `rmd160`, `sha256`, and `size`; with it, nothing but the digits changes. Groups of current algorithms behave the same either way.

The choice travels with the preparation: `record.PreparationSpec.KeepOldChecksums` is frozen at binding, the driver hands it to the edit service on every preparation of that job, and a bump that continues an open contribution inherits it the way it inherits the shared-release choice. `--diff` honors it too. Both edit paths read it: `portfile.ReplaceChecksumsKeeping` for the plain path and the observed path in `portedit`, where a kept legacy group is edited value by value like a current one.

## Exercise

`refresh-checksums argh --diff --keep-old-checksums` on the ports tree leaves `argh`'s block unchanged, since the archive still matches its md5, sha1, and rmd160, and reports nothing to commit; without the flag it produces the modernized block recorded in [the modernization note](2026-09-17-checksum-modernization.md).

## Tests

`portfile`: a kept legacy group refreshes md5, sha1, and rmd160 in place. `portedit`: a real `md5 sha1 rmd160` block refreshed through the observed path keeps its layout with the download's md5 and sha1 written in. `workflow`: the choice is frozen in the preparation spec and survives the store.
