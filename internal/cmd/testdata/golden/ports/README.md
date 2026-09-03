# Golden fixture ports

`devel/bumpee` is the port the bump goldens (`internal/cmd/golden_test.go`)
run against: a synthetic Portfile whose "upstream" releases sit beside it
under `files/upstream`, reached through `master_sites file://${portpath}/files/upstream`.
A bump to 2.0 therefore fetches `bumpee-2.0.tar.gz` from disk, so the plan,
its diff, and the in-place rewrite are the same on any machine with a
MacPorts evaluator and involve no network.

The distfiles are real gzip'd tarballs (one directory, one README) written
with Go's `archive/tar` and `compress/gzip` with every timestamp zeroed, USTAR
headers, and best compression, so regenerating them reproduces the bytes
exactly. 1.0 and 2.0 differ in content and in size, so every value the
Portfile records — rmd160, sha256, size — moves under a bump. The recorded
checksums for 1.0 are the ones dockhand itself computes: a
`bump --to 1.0 --recheck --plan` over the fixture plans zero edits.

The goldens pin the Portfile's own sha256 and the byte offsets of its edits,
so any change to `Portfile` re-records all three bump goldens
(`go test ./internal/cmd/ -run Golden -update`).
