# Image rebuilds: what field testing's rebuilds found

Field testing rebuilt the six Tart images at 7ed949df with `setup tart <rel>
--rebuild --macports-version 2.12.7 --xcode ~/Downloads/xcode_archives`.

- **The APFS resize is tried three times.** sequoia's container resize
  failed once, -69606, leaving 17 GB where 60 were needed, and a plain
  retry passed. `macos.EnsureAPFSSpace` tries `diskutil apfs
  resizeContainer` three times, five seconds apart, before it checks.
- **A failed Xcode install names no unreachable path.** It said
  "workspace retained at /private/tmp/dockhand-xcode.…", a path in the
  -next VM setup deletes when it fails. The workspace is still kept, nothing
  removed recursively, as before; the line now says the guest's macOS
  version and the free space in /private/tmp, which a failed expansion
  turns on.
- **An Xcode rebuild says its base image's MacPorts.** `--xcode` makes the
  release's Xcode image, an add-on beside its base image, which keeps its
  own MacPorts: the base images, and so their golden copies, stayed on
  2.12.6, unsaid. setup now says "Its base image keeps MacPorts 2.12.6,
  older than this Mac's 2.12.7: dockhand setup tart <rel> --rebuild makes
  it again".
- **Not yet fixed: monterey's Xcode 14.0.1 expansion.** xip fails in the
  guest, "Block-compressed payload operation failed", on both runs, where
  the archive expands on the host. It isn't the archive's size or the
  upload: ventura expanded Xcode 14.3.1, the same 7.0 GiB, through the
  same path; and the 60 GB space check passed. Xcode 14.0.1 needs macOS
  12.5 or later, so the vanilla Monterey guest's version is the first
  thing to read; the failure line now says it. No fix is guessed before
  that's read.

## The Xcode archive's copy is checked before it's expanded

Field testing then ruled out the guest's version: a clone of the old
dockhand-xcode-monterey, from the same vanilla digest, runs macOS 12.7.6
with Xcode 14.0.1 installed, so the OS had expanded this xip before, and
space was ample. That leaves the copy.

- setup reads the host archive's size and sha256 while the copy goes
  (`fileDigest`), and the guest copy's after it with stat and shasum
  (`macos.GuestDigest`). Where they differ, it stops before expanding:
  "the Xcode archive's copy in the guest is …, and <path> is …: the copy
  was cut short or changed". Where they match and xip still fails, the
  failure says the copy matched, with its size and sha256.
- The setup protocol stays 3: an image made now holds what it did; the provisioning pin moves (protocol_test's setupPin).
- An Xcode rebuild's note on its base image says the rebuild that makes it is the one without --xcode, where it read as the command just run (field testing, after monterey's rerun passed on b717dbad).
