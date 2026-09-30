# 2026-09-29: what holds, and what doesn't

Batch 4 of the roadmap's smaller items. The upstream comparison guards unattended submission, and it was wrong in both directions: gh couldn't be compared, which holds, and a Python pin MacPorts couldn't meet passed.

## Each archive beside its own replacement

gh fetches a source tarball on macOS 10.13 and later, and a prebuilt zip below it ([review](../reviews/2026-09-28-hugo-bump-exercise.md#gh-usql-hk-and-pgdog-chosen-for-untried-paths), finding 2). The update fetched the new version's archives in every context, two, and the current version's in this Mac's alone, one, then paired them by position, so the counts differed and nothing was compared: "the versions have 1 and 2 distfiles, so they can't be paired", which holds as D4 has it.

Pairing only this Mac's context would have compared the tarball and left the zip unread, which holds too. The update's planning already pairs each archive it replaces with its replacement, in every context, by the checksum declaration they share (`oldFiles[id]` in `planObservedArchives`). That pairing is now kept, once for each declaration however many contexts share it, with the port as that context evaluates it (`archivePair`). With archives kept, each replaced archive is fetched as MacPorts shipped it, checked against that context's checksums, and paired with its replacement (`pairArchives`, `Result.Pairs`, which replaces `Result.Previous`). A Go or Cargo update, which fetches the current version's archives for their manifests, reuses those it has. The engine compares each pair, and says a change the pairs share once. A new archive that replaces none dockhand found still holds, now named.

## A Python pin MacPorts can't meet

sqlit-tui 1.6.4 pins `textual-fastdatatable==0.19.0`, while MacPorts had py-textual-fastdatatable 0.17.1, and a noarch build passes regardless ([review](../reviews/2026-09-28-hugo-bump-exercise.md#sshuttle-through-paths-not-yet-taken)). The comparison said the pin moved, with a `·`.

Now a Python requirement the new version adds or moves carries its name and PEP 440 specifier (`sourcecompare.Requirement`). The engine matches it to a port the updated port depends on, by the python PortGroup's naming, `py313-textual-fastdatatable` providing `textual-fastdatatable` (`macports.PythonPackage`), reads that port's version in the tree the update is made in, and holds where the specifier excludes it: "pyproject.toml requires textual-fastdatatable ==0.19.0, which MacPorts' py313-textual-fastdatatable 0.17.1 doesn't meet". The tree is the branch's, so a branch that updated the dependency first, as the libuv run's took sqlit-tui with py-textual-fastdatatable 0.19.0, meets it. A requirement no dependency's name matches is left alone, since ports needn't be named for their packages (py313-yaml provides PyYAML). What can't be told, a version or specifier that isn't PEP 440's, is said with a `·`, and doesn't hold.

The specifier's reading is `sourcecompare.Admits`, PEP 440's comparisons, wildcards, compatible releases, epochs, and pre-, post-, and development releases. Its 37 cases were each answered by Python's own packaging 26.3, `SpecifierSet(spec).contains(version, prereleases=True)`, and the Go agrees with every one. A pre-release is admitted wherever its version is, since the question is whether an installed version meets the requirement, not which to pick.

The same fixture found that Python dependencies were known by their lower-case names, so `textual_fastdatatable` becoming `Textual-FastDataTable` read as one dropped and another added, which holds. They're now known by their names as PEP 503 compares them, in requirements.txt, PEP 621's array, and Poetry's table alike.

Tests:
- `TestAnUpdateOfArchivesChosenByReleasePairsEach`, a Portfile whose archive depends on `os.major`, as gh's does: two pairs, each with its own replacement;
- `TestAChangeTheArchivesShareIsSaidOnce`;
- `TestASpecifierAdmitsAsPackagingSays`, `TestAMovedPythonRequirementCarriesItsSpecifier`, `TestAPythonDependencyRespelledIsTheSameOne`, and `TestAPythonSubportNamesItsPackage`;
- `TestAPythonPinMacPortsCantMeetHolds`: unmet holds, met by the branch says nothing, unreadable is said, and PyYAML, which no dependency's name matches, is left alone.

Twenty-one mutations each fail a test.

Not checked live: neither gh nor sqlit-tui has a newer release than master has at c95ae6f.
