# Legacy macOS builds

`make build-legacy` builds dockhand for macOS 10.13 High Sierra to 11 Big Sur,
amd64 and arm64, into `dist/`. They are experimental until someone on one
of those releases has run them. `.github/workflows/release.yml` attaches
them to each published release, as `dockhand-<tag>-legacy-macos10.13-<arch>.tar.gz`
with a checksums file. For a tag from before the workflow, as v0.4.0, run
it by hand with the tag (Actions, Release, Run workflow), which builds that
tag's source with main's patch and check.

A legacy build is the release's own version and commit, so a commit's
`Generated-By` names the release as any build of it does. `dockhand
--version` says `legacy` after it, from `buildinfo.Variant`, from the
first release after v0.4.0; v0.4.0's says it only in its archives' names.

Where the patch no longer applies, or the check fails, on a new Go series,
the job fails and that release has no legacy assets: they stop at the
last release that passed until the patch is looked at again. The patch
stays the certificate-chain loop in crypto/x509, nothing more.

A normal build loads on macOS 12 and later. Go 1.25 and later
import `SecTrustCopyCertificateChain` in crypto/x509, through
`//go:cgo_import_dynamic` even with cgo off, and macOS 12 added it. dyld
binds every import when it loads a binary, so on 10.13 to 11 dockhand
stopped before main ([MacPorts #73086](https://trac.macports.org/ticket/73086)).
`x509.patch` takes back Go 1.24's calls, `SecTrustGetCertificateCount` and
`SecTrustGetCertificateAtIndex`, which 10.13 has. `build.sh` applies it to
copies of the three files, builds with `go build -overlay`, and leaves the
toolchain alone. A build with cgo off is the same on any host, Linux
included, so the Mac building it doesn't matter.

Older than 10.13 is out of reach. dockhand imports `utimensat`, which 10.13
added, and MacPorts has no Go newer than 1.17 there.

## When the build fails

**The patch doesn't apply.** A Go release changed the lines it replaces.
Diff `src/crypto/x509/root_darwin.go` and
`src/crypto/x509/internal/macos/security.{go,s}` from the release before,
then write the patch again so it still makes the same swap. It is applied
with no fuzz on purpose.

**legacymacos reports an import imports.txt doesn't list.** A Go release,
or a change to dockhand, imports something new. Look for it in the macOS
10.13 SDK's `.tbd` files, for example
[MacOSX10.13.sdk](https://github.com/phracker/MacOSX-SDKs/tree/master/MacOSX10.13.sdk)'s
`usr/lib/system/*.tbd` and the frameworks' `.tbd`. If it's there, add it to
`imports.txt`. If it isn't, the code that imports it needs another way for
these releases, as `x509.patch` is for crypto/x509.

## What's checked, and what isn't

Every import loads on 10.13 and 11, which the SDKs' export lists prove for
each symbol. The Go 1.27 runtime itself, though, is supported upstream from
macOS 13. Nobody runs Go's tests on 10.13 to 11, so a fault that only shows
at run time there is possible, and it would be found by people using these
builds.
