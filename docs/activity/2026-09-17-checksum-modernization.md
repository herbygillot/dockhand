# Legacy checksum blocks are modernized when touched

Asked for on 2026-09-17, after the tree survey: 1,554 assessed entries (1,288 Portfiles) were refused with "missing SHA256 or ambiguous checksum group", the third-largest reason in the tree. Python led with 331, then devel 181, net 133, x11 89, lang 77.

## What those blocks look like

A census of the 1,288 Portfiles' `checksums` commands: 557 carry `md5 sha1 rmd160`, 323 `md5` alone, 69 `rmd160 sha1`, 48 `md5 sha1`, 41 `sha1` alone, 6 `rmd160` alone; the remaining 240 or so already have `sha256` beside an `md5` or `sha1`. None of them is exotic. dockhand simply did not recognize `md5` and `sha1` as algorithms, so a block starting with one parsed as a run of unnamed groups without a `sha256`.

## The rule

A checksum group is *legacy* when it names `md5` or `sha1`, or lacks `sha256`. When dockhand refreshes a legacy group's archive, by `refresh-checksums` or by a version bump, it rewrites the group whole, first algorithm through last value, as `rmd160`, `sha256`, and `size`, which is what MacPorts' own `port checksum` and `port bump` write. The layout is the Portfile's own: values aligned in a column stay aligned in that column, the line continuation between pairs is copied, and a single-pair group continues onto new lines under its first algorithm. A group made only of current algorithms (any subset of `rmd160`, `sha256`, `size`) keeps its order and layout and is still edited value by value, so no supported port sees a different diff than before.

`portfile.LegacyChecksums` decides, `portfile.RewriteChecksumGroup` renders, and both edit paths use them: the plain `ReplaceChecksums` and the observed path in `portedit`, where `distfiles.Group` now records its algorithms in written order with their word spans and is identified by its first algorithm's owner rather than by `sha256`, which a legacy group may not have. A legacy group split across a `checksums` and a `checksums-append` declaration is refused rather than rewritten across unrelated text. The rewrite reports itself at info level: "Modernizing argh-0.2.3.tgz checksums: md5 sha1 rmd160 -> rmd160 sha256 size".

Refreshing a legacy block whose archive is unchanged now produces a commit, the modernized declarations, where before the same command would have been refused. That is the intended outcome: a maintainer touching the port gets the current layout for free.

## Exercise

On the ports tree, `refresh-checksums argh --diff` produced:

    -checksums           md5     ebc4e31c1e76cd433bbee734b95c967b \
    -                    sha1    ed4dec1a36d39c44084e3292e6d08226f31e2c3e \
    -                    rmd160  35cbc417dbe90054b6dc5cc83031bdd273a30ad7
    +checksums           rmd160  35cbc417dbe90054b6dc5cc83031bdd273a30ad7 \
    +                    sha256  50874370c149a23ff48bd4312395b9ba6eeec0864c44ecbd715758ddae9262c4 \
    +                    size    21346

The existing rmd160 matched the download, which is the check that the archive is the one the port meant. `assess` on ten previously refused ports reported eight ready; the two others fail on the next reason in the chain, an ftp-only master site. The whole refused population was re-assessed afterwards; see the follow-up note below.

## Also found

A versionless `bump` of a python port that was already current, `py-altgraph` and `py-packaging`, failed with "upstream: archive release does not match the Portfile". The archive-release coherence check rejected `NoUpdate` outright, whereas a forge source reports "already current". The check now accepts an already-current archive release, and the preview says `py314-altgraph: already current at 0.17.5; latest eligible version is 0.17.5`. Committed separately.

## Tests

`portfile`: legacy groups in aligned, single-space, single-line, tab-indented, single-pair, and named forms, and a current group keeping its order. `distfiles`: a legacy group binds, is owned by its first algorithm, and spans its written pairs. `portedit`: a real `md5 sha1 rmd160` block is modernized by a refresh and by a bump through the observed path, and a `sha256 size` block keeps its layout. `upstream`: an already-current archive release passes the coherence check.
