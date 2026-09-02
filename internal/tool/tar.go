package tool

// Tar is the host's tar, pinned to macOS's libarchive bsdtar rather
// than searched for on PATH. It is the one tar decision, and it is
// made for distfile: a distfile arrives as gzip, bzip2, xz or zip, and
// reading a single member to stdout (-xOf) across all of those is what
// bsdtar does and what a GNU tar on PATH — MacPorts' gnutar, a
// coreutils shadow — cannot, zip first among them. The other host
// sites, tart's staging stream and git's materialized archive, consume
// plain ustar and would work with any tar; they use this one so that
// there is one answer to "which tar" rather than three.
//
// A Finder resolves a Tool whose value is a path by checking that
// path, so Tar goes through the same Find as every other tool and a
// missing binary reads "/usr/bin/tar not found".
//
// Guest-side tars are outside this decision. The /usr/bin/tar that
// tart exec runs inside a VM is the guest's own, and is named where
// the guest command is assembled.
const Tar Tool = "/usr/bin/tar"
