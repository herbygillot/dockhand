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
