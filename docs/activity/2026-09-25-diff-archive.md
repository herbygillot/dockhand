# 2026-09-25: diff --archive

Design v3 §6.5 points to it from the stealth-update report ("dockhand diff --archive for all of it"), and it is on the step 10 list.

## What changed

- **`dockhand diff --archive [<port>...]`** compares what the changed ports' source archives hold. It takes the archives the base's Portfile declares and the ones the branch's files declare now, and diffs them file by file, less each archive's versioned top directory. That way `jq-1.7.1/NEWS` and `jq-1.8.1/NEWS` compare as `NEWS`.
  - Each archive gets a summary line: "2 files differ: 1 changed, 1 added". Then comes the patch. `--stat` shows the summary alone, and `--json` carries both.
  - An identical pair says so. A new port says there is nothing at the base to compare with.
  - Named ports narrow it, by name or directory.
- **Fetching, checked.**
  - Each archive is fetched as its Portfile declares it and checked against that Portfile's checksums.
  - When upstream no longer serves an archive as declared, the old one comes from MacPorts' distfiles mirror, under the port's `dist_subdir`. After a stealth update, upstream serves the new contents under the old name, and the mirror is the one place the old ones survive.
  - The output says when the old archive is the mirror's copy.
  - When neither has it as declared, dockhand says so rather than comparing the wrong file.
  - The engine's `ArchiveFetcher` seam does the fetching; MacPorts' evaluator is the default. It reuses the preparation's download policy, so a port with credentials, custom fetching, or vendored sources is refused the same way.
- **The comparison** extracts both archives to a scratch directory and runs `git diff --no-index` over them (`git.DiffDirectories`), which gives an ordinary `a/`, `b/` patch and names binary files rather than encoding them.
- **`archive.Extract`** writes an archive's regular files, less a shared top directory. It leaves out links and special members, and refuses a member whose path would leave the directory.
- **xz, zstd, and lzip.** `archive.Walk` now reads these tarballs through the system's own `xz`, `zstd`, or `lzip`, piped into the same member-by-member reader. Before, it read gzip, bzip2, zip, and plain tar only. Many upstreams ship `.tar.xz`, so the upstream comparison on `update` (§6.12) now works for them too.
- **The stealth report** ends by pointing to `dockhand diff --archive`.

## Decisions

- **Not in the stealth report itself.** The design's report shows the inside-the-archive summary inline. That would mean a second download from the mirror, and extracting both archives, during every `checksums` run. The report points to `diff --archive` instead, which does it on request.
- **Archives pair in the order the Portfile declares them.** That is the order of `distfiles`, which MacPorts keeps. An archive only one side declares is shown as new, or as no longer fetched.
