# 2026-09-30: the binary archive site out of Tart's SSH channel

Item 6's sixth piece is the private-helper review's finding 6. Making a kept archive installable from an archive site of MacPorts' kind was split across two places that shouldn't know about it:
- the Tart provider knew the site's layout, both of MacPorts' signature formats, and the public keys' file names;
- the SSH transport package, `tart/channel`, generated and stored the signing keys beside SSH's, and so imported RSA, X.509, PEM, and signify for a MacPorts protocol.

## What moved

`internal/macports/binaryarchive` now owns what a site's entry is:
- `Keys` and `LoadKeys(directory)`: the signify and RSA keys, made once and read after. Two processes making them at once keep the first one's, as before.
- `Sign`: checks a kept archive against its digest, and writes its `.sig` and `.rmd160` signatures. The RSA one is made with `openssl dgst -ripemd160 -sign`, as `pubkeys.conf` says to sign one's own archives.
- `Keys.PublicKeys`: the files at the site's root, `dockhand.pub` and `dockhand.pem`.
- `EntryPath`: an entry's path in the site, `<site>/<port>/<archive>`.
- `Installable`: whether a port's archive can be an entry.

The Tart provider keeps what is Tart's: the guest's site directory, uploading the entry's files, making them readable to MacPorts' user, and the guest's configuration. `tart/channel` is SSH transport and trust alone again.

The review named three kinds of archive, and the package's comment keeps them apart: upstream source archives (`archive`, `portedit/archives`), the packages MacPorts builds (`model.Archive`), and a package signed as a site's entry (`binaryarchive`).

## Nothing rotated

The keys stay in `~/.dockhand/ssh/`, as `archives.key` and `archives-rsa.pem`, the files `channel` wrote. Tart passes that directory to `LoadKeys`, so keys made before this change are the ones read after it. The review asked for that.

## Tests

- `TestAnArchiveIsSignedAsASitesEntry` covers an entry's files and its signify signature. It checks that the RSA signature verifies with openssl as MacPorts verifies one, and it covers a digest that doesn't match and names a site can't hold.
- `TestTheArchiveKeysAreMadeOnce` moved from `channel` unchanged, but for its owner: racing makers, owner-only files, and no temporary file left.
- Tart's `TestKeptArchivesGoToTheGuestSigned` still holds, unchanged but for how its keys are made. It covers every upload, the signatures, the site's order of commands, and a mismatched archive stopping the attempt.
