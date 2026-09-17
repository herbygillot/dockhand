# The Go manifest is found under the archive's top directory

Asked for on 2026-09-17 after `bump uni` was refused: "no extracted archive contains the dependency manifest at worksrcdir".

## The bug

The Go PortGroup's default `worksrcdir` is `gopath/src/${go.package}`, for uni `gopath/src/zgo.at/uni/v2`. That directory exists only after MacPorts extracts the archive and `post-extract` moves the source into a GOPATH layout. The archive itself holds `uni-2.10.0/go.mod`. To regenerate `go.vendors`, dockhand searched the archive for the manifest at the evaluated `worksrcdir` verbatim, and its one fallback stripped a single leading component, which could not match either. Every Go PortGroup port gets that `worksrcdir`, and 173 Portfiles in the tree declare `go.vendors`; the live regeneration test had passed only because it hands the generator the archive's top directory directly. No real vendored Go port had been through dockhand's regeneration.

## The rule

`dependency.GOPATHLayout` recognizes a `worksrcdir` under `gopath/src`. For such a directory, `Manifest` looks for the manifest directly under the archive's single top-level directory, which the PortGroup's extraction flattens into GOPATH, and `ConfirmSource` accepts that member. The generator already derives the helper's subdirectory from the member found, so `go2port get --dir /` follows. Two top-level directories each carrying a manifest are ambiguous and refused. A port that sets `worksrcdir` outright is read at that path, as before.

## Exercise

`bump uni --diff` regenerated the block: version 2.9.0 to 2.10.0, the archive's checksums, and every `go.vendors` entry with its new lock, digests, and size, `zgo.at/zstd` moving from `ec259dea6715` to `7567984d0ee9` among them. The real `bump uni --no-publish` then built uni 2.10.0 offline from that block in Tart on macOS 26 (Tahoe) arm64 and passed, which is the proof that the regenerated vendors are complete; the branch `dockhand/bump/uni-fxpeh3hc33yxmmnbzbckljjrhk` is verified and waits on `publish uni`.

One thing the diff shows that dockhand does not act on: uni 2.10.0's `go.mod` says `go 1.24` while the Portfile keeps `go.toolchain_min 1.22`. Reading the new manifest's `go` and `toolchain` directives and moving `go.toolchain_min` when they exceed it belongs on this path; it is not done yet.

## Tests

`dependency`: a GOPATH-style `worksrcdir` finds `go.mod` under the archive's top directory, a nested `go.mod` deeper in the tree is not mistaken for it, `go.work` is still reported missing, and two top-level manifests are refused. The dependency, portedit, preparation, and assess suites pass.
