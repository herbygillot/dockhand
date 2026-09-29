# 2026-09-29: the Go minimum as go.mod writes it, and said wherever the update is

The hugo exercise's ov run ([review](../reviews/2026-09-28-hugo-bump-exercise.md#ov-through-adopt-edit-retry-and-submit---passing), findings 1 and 2) and tbls run (finding 1) found two things about `go.toolchain_min`. dockhand wrote the series, `1.26`, where go.mod said `go 1.26.8`. And a minimum it raised was said only as the update ran, never in `submit --plan` or `--passing`.

**What the Go PortGroup compares.** The roadmap had called the trimmed value a defect, one that admits a Go the module refuses. It isn't. The Go PortGroup's header says the value "may be written however go.mod writes it", and `go_toolchain.satisfies` compares only the series, since MacPorts ships the newest patch release of each series it packages. So `1.26` gated tbls exactly as `1.26.8` does. The patch floor is the build's to enforce, under `GOTOOLCHAIN=local`. The roadmap and the review's reconciliation are corrected.

What did change:
- **The `toolchain` line is no requirement.** `dependency.GoRequirement` took the larger of go.mod's `go` and `toolchain` lines. Go documents the second as a suggestion, and the PortGroup builds with `GOTOOLCHAIN=local`, which ignores it, so a module with `go 1.25.0` and `toolchain go1.26.2` was gated at 1.26 on systems where it builds. That was the defect.
- **A raise writes the directive as go.mod does,** `1.26.8`, as the PortGroup's guidance says: "Copy it." It still raises only a minimum of an earlier series, compared as the PortGroup compares, now with Go's own `go/version` rather than semver, which can't read a release candidate such as `1.21rc1`. A patch release moving within the series leaves the Portfile alone.
- **Each outcome is a fact of the result.** `portedit.GoToolchain` records what go.mod requires, what the Portfile declared, and what the update did: covered, raised, undeclared, or left for a person. The engine words each as an upstream line, which the update's record keeps, so the submit preview and `--passing` show it. Only a minimum left unmet holds, as before; a raised or covered one is a `·` line. The pull request's description carries no upstream finding, this one included; its diff shows the raise.

Tests:
- `TestGoRequirementIsTheGoDirectiveAsWritten`: `1.26.8` as written, and the `toolchain` line ignored either side of the directive;
- `TestModuleModeGoPortRaisesToolchainMinFromTheManifest`: `go 1.24.3` with `toolchain go1.25.1` raises 1.22 to `1.24.3`, recorded as raised;
- `TestToolchainMinIsLeftAloneWhenNotRaisable`: a patch release of the declared series and a later declared series are covered and left alone, beside the earlier cases, each with its outcome;
- `TestServeSaysAGoToolchainMinimumItNeedNotHold`: raised and covered reach the submission's Upstream section without holding.
