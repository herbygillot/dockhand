# The survey's tail

The remaining survey item after the setup-argument work: four bounded pieces and one deliberate refusal.

## Revisions carried in tables

The qt family reads each module's revision from the same table as its checksums, `revision [regexp -inline {[0-9]+} [lindex ${module_info} 7]]`, so the reset that follows a version bump found a calculated declaration and stopped. The literal tracer that gave checksum values in tables an owner, now `portfile.UniqueLiteral`, does the same for a calculated revision: the one place the Portfile writes the words `revision N` for the port's current revision is reset to `revision 0`; two such places, or none, still refuse. A fixture with a table of one checksum and one `"revision 1"` bumps and resets both.

## Host reads inside PortGroups

The qt4 PortGroup asks `file exists ${qt_frameworks_dir}/QtCore/QtCore` to choose which dependency path to append, and every port loading it was unknown for reading installed state. A host access records its Tcl frames, and the frame inside a PortGroup file names the line: when that line is a control command whose every body is a benign sink, or a benign sink itself, the read shapes the build and can reach no source declaration, so it is explained the way compiler-selection probes are. `tolerateExplainedProbes` judges each recorded access on its own: a compiler probe, with the Portfile's toolchain reads in build positions; a PortGroup-local benign read; anything else keeps the context inconclusive, and the frames are reported at debug level. qfsm, qjson, aqbanking6, rocs, trojita, and fontmatrix are input-found; mythtv.27 is unsupported for its git-commit version instead.

## FTP master sites

About 110 ports fetch only from FTP. `fetch.OpenFTP` retrieves one file by anonymous FTP through `github.com/jlaffaye/ftp`, bounded like an HTTP body, credentials in the URL refused; the archive downloader takes `ftp://` locations beside HTTP ones and hashes and sniffs the body the same way. A minimal FTP server in the test exercises login, extended passive mode, retrieval, a missing file, and the bound. `bump ndiff 1.00 --diff` fetched ndiff-1.00.tar.gz from ftp.math.utah.edu and rewrote its md5-only checksum as rmd160, sha256, and size; traceroute in the corpus is input-found.

## Perl's v spellings

32 perl modules spell their version `v2.403.9`. The stability classifier called that unknown, so automatic discovery refused them; a leading v is now part of a stable spelling, since MacPorts' vercmp orders it and the perl PortGroup strips it.

## Left as refused

A few perl ports point their livecheck at a CPAN author directory without a trailing slash, and the server answers with a redirect to the same directory over plain http. The fetch guard refuses a redirect that leaves HTTPS, and it keeps refusing: following it would let a tampered listing choose the version, and MacPorts' own livecheck following it is not a reason for dockhand to. Those ports take an explicit version.

## Corpus

136 input-found, 8 unsupported, 3 unknown, from 129/8/10 the evening before; no regressions. The three unknowns are openjdk11's inconclusive candidate evaluation, gr-ieee802-15-4's tag convention, and rep-gtk's uncovered checksum context, none of them this item's.
