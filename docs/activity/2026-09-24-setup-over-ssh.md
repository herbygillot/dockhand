# 2026-09-24: setup reaches its guests over the channel

Step 2 of the roadmap's Next; decisions 42 and 43 of the [contracts
direction](../reviews/2026-09-23-contracts-direction.md).

**Transport.** Setup's own SSH client (`golang.org/x/crypto/ssh`, a dial
macOS's Local Network privacy refused from this session) and its `tart
exec` commands are gone; every guest step goes through
`internal/tart/channel`:

- `Connect` resolves the guest's address (`tart ip`) and waits, four
  minutes at most, for SSH. A new candidate is reached by bootstrap: the
  image's `admin` password, once, recording the host keys it presents
  and installing dockhand's key. Everything after uses the key.
- The guest agent is still installed, over SSH: it grows the disk, and
  verification marks each clone through it. `ReadyAgent` waits for
  `tart exec /usr/bin/true` and relies on no output; the old check of
  exec's data transport is gone with the reads that needed it.
- Tools, Xcode, MacPorts, validation, and the manifest run over SSH; the
  Xcode archive is copied with `Upload`, checked by size and sha256.
- `setup --check` reaches its clone as verification will, by the image's
  recorded host keys and dockhand's key, never the password.

**Host keys.** A candidate's host keys are recorded under its temporary
name while it is built, then under the image and its golden copy once it
is adopted, and the temporary record is removed. A golden restore copies
the golden copy's record.

**Images made before the key.** An image without recorded host keys was
made before dockhand reached guests over SSH. Its next `setup` clones it,
gives the candidate the key, validates it, and adopts it like a rebuild,
installing nothing again; `setup --check` says to run `setup` instead.

**Images in the person's home.** When dockhand's home lacks an image and
the person's Tart home has one of that name, from an earlier dockhand,
setup copies it with Tart's `export` and `import`, through a file under
`~/.dockhand/imports` removed afterwards, and upgrades it as above. The
person's image is only read. A copy that does not validate against its
profile (`errUnsuitable`), as a Tahoe image with the macOS 27 tools does
not, is cleaned up and provisioned afresh, saying why. `--rebuild`
skips the copy.

The vendored `golang.org/x/crypto/ssh` and what only it used are pruned;
`x/crypto` stays for the archive checksums.

Tests: the provisioning double gains the new steps, and tests cover the
bootstrap before anything is installed and the host keys recorded under
the image and golden copy, `--check` reaching the clone by the image's
keys, an image without the key upgraded in place, and an import that
validates and one that falls back to provisioning.
