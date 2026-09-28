# 2026-09-28: kept archives

Roadmap item 6, decisions 28 and 44. A reused target that a built one needs has to be in the guest, and until now it could only be built again there (see [partial reuse](2026-09-28-partial-reuse.md)). So dockhand now keeps the archive each passed build makes, for a later guest to install.

## Keeping them

**What's kept.** When a target passes in a Tart guest, the guest reports its archive's digest, as before, and now also where the archive is. The provider fetches it over the checked channel, only from MacPorts' own `software` directory under the prefix. The engine then:
- checks it against the digest the result reported;
- syncs it and moves it into place;
- only then records it as kept (schema 22).

An archive is ready for dependents once its record exists (decision 44). One kept already isn't fetched again. One that can't be kept leaves a progress line, and the result stands.

**Where.** `~/.dockhand/archives/<repository>/`, beside the database, each file named by its sha256. Each checkout the database serves keeps its own, so cleaning one up never weighs another's references. Moving a downloaded file into place goes through `atomicfile.Place`, which syncs the file and its directory, rather than copying hundreds of megabytes again.

**Cleanup** (decision 36, amended by 44). Automatic cleanup forgets a kept archive, and removes its file, once:
- no result in an open branch's checks names it;
- no result recorded within `cleanup.after` does. A recent reuse of an old build names its archive, so that archive stays while the reuse is recent;
- it was kept before `cleanup.after` itself.

It also removes what no record names, once it's older than that: a fetch that never finished, or a file whose record never followed. Nothing caps the store, as decision 36 said at first, so cleanup says how much stays: "removed 3 kept archives, 820 MB, that no open branch's checks name; 41 archives kept, 6.2 GB".

**The guest program.** It only reports one more thing, so `VerifierProtocol` stays 1, re-pinned.

**Tests.** Each fails with its part undone:
- `TestAnArchiveIsKeptOnlyWholeAndOnce`: kept whole, checked, not fetched twice; a missing file is fetched again; nothing is kept under a name that isn't a file's.
- `TestAnArchiveIsKeptOnlyFromMacPortsSoftware`, and the provider's recording test: a passed target's archive is fetched, by MacPorts' name for it; a failed one's isn't; nothing outside `software` is fetched.
- `TestArchivesGoWhenNoLiveResultNamesThem`: the store's rule, one clause at a time, the recent reuse included.
- `TestCleanupRemovesArchivesNoLiveResultNames`: files, orphans, unfinished fetches, and the report.

## Installing them

A reused target that a built one needs is now installed in the guest from its kept archive, rather than built again (`reuse.Choose`, `reuse.Needs`). With the fixture's library and the tool that links it, a change to the tool builds only the tool, and the guest installs the library. On a retry, a target an earlier attempt finished, which one it builds now needs, is installed from its archive too, rather than built unrecorded (decision 44: "retry consumes only complete applicable results with their archives available"). A needed target whose archive isn't kept builds, as before; on a retry, it's left to MacPorts.

**Not where decision 28 planned.** Decision 28 planned to put a kept archive in the guest's `${prefix}/var/macports/software/<port>/`, which `archivefetch` skipped fetching for. MacPorts 2.12 looks there only for a port it has installed. For one it hasn't, it looks in `incoming/verified`, its own cache of what it fetched and verified. Neither is an interface MacPorts documents.

**What MacPorts documents.** MacPorts documents distributing archives of one's own:
- `archive_sites.conf` names a source of archives;
- `pubkeys.conf` names the keys that verify them, and says how to sign an archive for them: `openssl dgst -ripemd160 -sign`.

So the kept archives are an archive site of MacPorts' own kind in the guest, `file:///var/tmp/dockhand-archives/`, each in its port's directory, signed with dockhand's key, which the guest's `pubkeys.conf` is told to trust. MacPorts sorts its sites by how soon they answer, and a local one first, so it's tried before packages.macports.org. Dependencies nobody changed are still fetched from packages.macports.org.

**Signed both ways.**
- A source `archive_sites.conf` names is verified with RIPEMD-160 signatures: MacPorts gives it `sigtype rmd160` by default, and the file can't name another.
- The ports tree's own site declares signify's.
- MacPorts' prefetch creates an empty signature file for each type before fetching. Afterwards it takes the first type whose file exists, even an empty one, and whose type comes first depends on the order of a Tcl array.
- So each archive is signed both ways, as packages.macports.org serves both:
  - with openssl's RIPEMD-160 and dockhand's RSA key, by the command `pubkeys.conf` shows;
  - with signify's Ed25519, which `internal/signify` makes and MacPorts' own signify accepts, in a test.

The keys are made once, in `~/.dockhand/ssh` beside dockhand's SSH key. A guest trusts them only in the clone its check makes.

**Checks.**
- The provider checks each archive against its digest again before signing it, so a file changed since it was kept stops the attempt.
- A build that shows the dependency active from another archive is said to have been given another by MacPorts: "jq built with oniguruma6 from another archive than the one kept of its build".

**The guest program.** It changes how a needed target gets into the guest, not how anything is built or judged: installing the archive of a build with the same inputs is what reuse already stands on. So `VerifierProtocol` stays 1, re-pinned.

**Checked live.** The setup: clones of the Tahoe Xcode image, and a scratch branch revision-bumping oniguruma6 and jq, which links it.
- **check-1** built both, and kept both archives: 484 KB and 352 KB, each recorded by its digest.
- **check-2** bumped jq alone:
  - oniguruma6 was reused, jq built, and the guest was given oniguruma6's archive;
  - that first try signed it with signify alone. MacPorts asked for RIPEMD-160 first, took the empty placeholder, and built oniguruma6 itself;
  - the check said so: "jq built with oniguruma6 from another archive than the one kept of its build: MacPorts chose sha256:aa2597c2…".

  That's what showed the order of the signature types, above.
- **check-3**, with both signatures:
  - MacPorts fetched oniguruma6's archive from dockhand's site, and verified it with `dockhand.pem` ("successfully verified with public key /var/tmp/dockhand-archives/dockhand.pem");
  - it skipped fetching and building oniguruma6, and installed and activated `@6.9.10_1`;
  - jq built against it, and no other archive was reported.
- **jq's own tests** failed in the guests where it built first, as "unable to open output file 'src/main.o': 'Operation not permitted'". Its `make install` recompiles `main.o`, and its `make check` can't recompile it again. A control check of jq alone, with no archive site, fails the same way, so it's jq's, not the archive's.

**Tests.** Each fails with its part undone:
- `TestTargetsReuseWhatStandsAndBuildWhatTheBuiltOnesNeed`: a needed target with a kept archive is installed, not built, and takes nothing with it.
- `TestTheGuestInstallsWhatABuildNeedsFromItsKeptArchive`: what the provider is given; the warning when MacPorts chose another archive; a retry installing what the attempt before it finished.
- `TestKeptArchivesGoToTheGuestSigned`: both signatures, the RIPEMD-160 one checked by `openssl dgst -ripemd160 -verify` as MacPorts runs it, both keys, the site made readable before the program starts, and a changed archive stopping the attempt.
- `TestKeptArchivesAreAnArchiveSiteOfMacPorts`: the guest program's `pubkeys.conf` and `archive_sites.conf`, and nothing without archives.
- `TestTheArchiveKeysAreMadeOnce`, and `signify`'s tests, MacPorts' own signify verifying what the key signs.
