# Named source checksums and multiple archives

Replaced unnamed-pair assumptions with explicit checksum groups. Named filenames, including interpolated Tcl expressions, remain byte-for-byte intact while only checksum values change. Each downloaded source must match exactly one group with sha256; duplicate, missing, calculated, and overridden checksum values are refused. Multiple archives may share one direct master site or select distinct tagged sites. Ambiguous mirrors, source-count changes, and unsupported fetch customizations still stop preparation. Every archive is downloaded and hashed before a candidate is accepted; final evaluation/fidelity and immutable release checks remain in place.

The `zix` Portfile uses the standard GitLab archive setup and a local `fdatasync.patch`; the old blanket patchfiles rejection was the relevant boundary. Preparation now proves patch files are regular files within the frozen port directory and preserves them. Remote patches and arbitrary hooks remain unsupported. No change to forge adapters or blanket relaxation of fetch hooks was needed.

Authored these changes in prepare. Regressions cover interpolated names, multiple archives, tagged source selection, malformed associations, preserved local GitLab patches, and existing fidelity/custom-hook refusals. No v1 comments or tests were copied. The optional download output sink provides the same hashed bytes for dependency lockfile preparation in the next slice.

Validation passed: preparation regressions, full `go test ./...`, `go vet ./...`, and `make build`. GitLab/local-patch behavior was exercised through real MacPorts evaluation with a controlled archive server; no live contribution was published.
