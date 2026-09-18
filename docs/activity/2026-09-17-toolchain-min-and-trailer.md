# go.toolchain_min follows the manifest; the trailer is plain text

Two requests on 2026-09-17, after the GOPATH manifest fix.

## go.toolchain_min

The question was whether dockhand checks `go.toolchain_min` when it scans a port. It did not. The Go PortGroup's own guidance decides what the right behavior is: with `go.offline_build no` the build runs in module mode, Go enforces `go.mod`'s `go` directive, and that directive is exactly the minimum, "copy it"; in GOPATH mode, the default, the directive is only an upper bound on what the source needs, and a minimum should be declared only when known to be needed.

So, for a module-mode port, the archive's bytes are now kept through the edit, the new release's `go.mod` is read from it (through the GOPATH `worksrcdir`, which the manifest fix made possible), and `dependency.GoRequirement` reports the series it requires, the larger of its `go` and `toolchain` directives. A literal `go.toolchain_min` below that is raised, the change is evaluated and checked against the previous snapshot like any other edit, and the bump says "Raising go.toolchain_min from 1.22 to 1.25, which fixture's go.mod requires". It never lowers a minimum, never adds one to a port that declares none, since that would gate the port on older systems and is the maintainer's call, and never refuses a bump over it; a minimum carried by an expression is reported for hand editing. In GOPATH mode the declared minimum is left alone and the reason is given at verbose level. In the tree, 373 Go ports build in module mode from an archive, 277 of them declaring a minimum.

`bump git-lfs --diff` showed the other branch of the rule on a real port: git-lfs 3.8.0 requires Go 1.25 per its manifest and the Portfile declares no minimum, so the preview reported the requirement and left the Portfile alone. uni, which prompted the question, builds in GOPATH mode, so its 1.22 stays; that is the PortGroup's advice, not an omission.

## The trailer

Generated contribution commits ended with `Generated-by: [dockhand](https://github.com/herbygillot/dockhand)`, Markdown in a place that is not Markdown. They now end with

    Assisted-By: Dockhand devel+1a2b3c4d5e6f (https://github.com/herbygillot/dockhand)

The version token comes from a new `internal/version` package that both `--version` and the trailer read from the embedded build information: the module version when built from a tag; otherwise the pseudo-version Go stamps on an untagged build, `v0.0.0-20260917234735-1843138f26d4+dirty` for a modified tree at that commit, which names the exact code that made the commit; and `devel+<revision>` when only the revision is known, `devel` when nothing is. No tag exists yet, so commits carry the pseudo-version for now. The PR body's filter and the message renderer recognize the legacy trailer too, so a message rewritten from an older record carries one trailer, not two.

Commits prepared before the change keep the old trailer: the vault and uni pull requests were published from branches prepared earlier, uni's reusing its verified build, so both still say `Generated-by`. The next prepared commit says `Assisted-By`.

## Exercise

`bump uni` continued the contribution, reused the passing build, and published https://github.com/macports/macports-ports/pull/34742. `bump git-lfs --diff` exercised the toolchain rule's report branch on a real port.

## Tests

`dependency`: the requirement is the larger of the two directives, series only, empty when there is no `go` directive. `portedit`: a module-mode fixture raises 1.22 to 1.25 from a manifest with `go 1.24` and `toolchain go1.25.1`, read through a GOPATH worksrcdir, with clean fidelity; an already-covering minimum, a GOPATH-mode build, and an undeclared minimum are left alone with the right message. `version`: the tag and string forms. The message and body tests follow the new trailer.
