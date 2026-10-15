# Batch a: a different copy on MacPorts' mirror under a shared dist_subdir

Field testing on f86cec28: gopls 0.23.0 to 0.24.0 took its checksums from the
GitHub tag archive, tools-0.24.0.tar.gz, and check-360 failed at the checksum
step. The golang PortGroup's shared dist_subdir, go, already held x/tools'
own tools-0.24.0.tar.gz on distfiles.macports.org, and MacPorts fetched the
mirror's first. The port was fixed by hand with `dist_subdir ${name}`
(#35546).

- `distfetch.Store.SharedMirrorCopy`: where the port's dist_subdir isn't its
  own (its name, or under it), MacPorts' mirror is asked for the archive's
  name there, and a copy that differs from the one just fetched is said.
  None there, or the same file, is nothing.
- `portedit.Result.MirrorConflicts`, from each archive an update or a refresh
  fetches. Update holds on it as an upstream change (rule `mirror-conflict`),
  "MacPorts' mirror already has a different tools-0.24.0.tar.gz under go/,
  which MacPorts may fetch first and fail its checksum; dist_subdir ${name}
  keeps this port's apart", before a VM boots; checksums says it as left.
- TestASharedMirrorSubdirsOtherFileIsSaid.
