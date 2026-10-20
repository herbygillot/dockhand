# Binaries for macOS 10.13 to 11

Someone on an older macOS asked to run dockhand. Go 1.27 doesn't run
there, and neither did a binary it built. A normal build loads on macOS
12 and later, which MacPorts' go_toolchain PortGroup shows for Go 1.25 to
1.27. Go upstream says 13. On 10.13 to 11, one import stops it before
main: crypto/x509's `SecTrustCopyCertificateChain`, which macOS 12 added
(MacPorts #73086).

`tools/legacymacos` builds dockhand with the same Go, with x509.patch
taking back Go 1.24's two calls through `go build -overlay`. Both
binaries' imports were checked against the export lists of the macOS 10.13
SDK and the 11.3 SDK: all are there except `SecTrustEvaluateWithError`,
which 10.13's SDK doesn't list, though Go 1.20 to 1.24 import it and ran
on 10.13. `imports.txt` freezes that set. `legacymacos` fails the build
on anything new, so a Go release or a change that imports something
newer is found in CI, not on someone's Mac. Its test builds a certificate
verifier with stock Go and finds the Monterey import.

`make build-legacy` puts the binaries in `dist/`. CI's new `legacy` job
builds them on Linux on every push, which also shows a build with cgo off
doesn't depend on its host. `release.yml` attaches them to each published
release, in archives named `legacy-macos10.13`, with their checksums, and
runs by hand for a named tag, as v0.4.0, whose tag predates it: the tag's
source, main's patch and check. Only that job may write the release.

A legacy build keeps the release's version and commit, so its Generated-By
names the release. `buildinfo.Variant`, linked as "legacy", puts `legacy`
after the version in `--version` and the help, and `Tag` leaves it out.
A tag older than the variable ignores it. If the patch stops applying or
the check fails on a new Go series, the assets stop at the last release
that passed. Experimental until the person who asked has run them.

Weighed and left: a build with Go 1.24, which needs go-github v91 (it uses
`new(expr)`) and the x/ modules downgraded, and dockhand's own uses of
Go 1.25 to 1.27 rewritten (`WaitGroup.Go`, `os.Root.RemoveAll`,
`new(expr)`, promoted fields in struct literals). Older than 10.13 is out
of reach: dockhand imports `utimensat`, which 10.13 added, and MacPorts
has no Go newer than 1.17 there.

Not checked: the Go 1.27 runtime on 10.13 to 11, which upstream doesn't
test. The first people to use these builds will be the test.
