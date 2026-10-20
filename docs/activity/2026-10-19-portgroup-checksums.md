# A checksum a PortGroup's table sets is said as the PortGroup's

The last line of the roadmap's v0.5.0 part 3, step 2 (604f358b). 73 ports
in cross take their checksums from their PortGroup's table:
crossbinutils 1.0's `crossbinutils.versions_info` and crossgcc 1.0's
`crossgcc.versions_info`. `checksums aarch64-elf-binutils` said "the
checksums for binutils-2.47.tar.xz can't be found in the Portfile to
edit: no command written in the Portfile makes it, as an eval'd one isn't".
It then printed "Use these checksums in the Portfile:" with a block, and
the block wasn't the Portfile's to take.

- `portfile.LocateDeclaration` returns `*portfile.InPortGroup` where the
  command's frame, or the nearest frame outside it with a file, is a
  PortGroup's (`_resources/port1.0/group/<name>-<version>.tcl`). It names
  the PortGroup, its file and line, and the tables its command reads.
  crossgcc's checksums sit in an `if` of its procedure, which has no file
  of its own, so its line is the `if`'s.
- `distfiles.Unlocated` says it as "the checksums for X are set by the
  crossbinutils 1.0 PortGroup, at
  _resources/port1.0/group/crossbinutils-1.0.tcl:166, from
  crossbinutils.versions_info, which no Portfile edit changes; a change
  there moves every port of the group". It never says "in the Portfile".
- `checksums` says whether the archives as they are now agree with the
  checksums there (`ChecksumsToWrite.Agree`). Where they differ, it gives
  the archives' checksums, and it never says to put them in the Portfile.
- An update refuses with the same words before it moves the version
  (`portGroupChecksums`, from the release's check and the version's
  preparation). The new version is a row the table has no entry for, and
  no Portfile edit adds it. `update aarch64-elf-binutils 2.48` had said
  "aarch64-elf-binutils.dependencies changed".
- dockhand doesn't edit the PortGroup.
- A survey of the cross category finds all 73 saying it. The first try
  found 56; crossgcc's 17 had no file on their own frame.
- Test: `TestADeclarationAPortGroupMakesIsThePortGroups`, crossgcc's case
  included.
