# 2026-09-30: holds scoped to what the build reads

Batch 9 of the roadmap's smaller items. With the copyright years gone (batch 2), holds on files the port's build never reads were the most common false stop of an unattended submission (the flatbuffers run's finding 2, and the chezmoi run).

**Which build systems a port uses is MacPorts' to say.** The evaluator now reports the PortGroups a port loads, by name, as Base records them in `PortInfo(portgroups)` for the registry and the PortIndex (`dockhand.portgroups`), and reads `use_configure` and `configure.cmd`. `macports.PortInfo.BuildSystems` maps them to build systems: the PortGroups that build a port or bring a language's dependencies to it, cmake, meson, golang, cargo and rust, python, perl5, ruby, npm, java and maven, R, and the rest, and autotools where the port runs a configure script, as Base does by default. A PortGroup that builds nothing, such as github or legacysupport, names none. Where it can tell of none, as for a port that builds by its own commands, nothing is known not to be used. `PortInfo(portgroups)` is a Base variable rather than a documented interface; dockhand's compatibility check already relied on it, and Base's installer and PortIndex read it.

**Each upstream file belongs to one.** `sourcecompare` tags each change with the build system its file belongs to (`Change.System`): CMakeLists.txt to CMake, package.json to Node, Package.swift to Swift, pyproject.toml to Python, and so on. A license file belongs to none.

**A file of a build system the port doesn't use holds nothing.** The engine sets such a change apart, saying why: "Package.swift changed …; flatbuffers builds with cmake, not swift, so it holds nothing". A manifest's dependencies are counted in one line, as a proven manifest's are (D9), since none holds: "package.json: 8 dependencies changed; flatbuffers builds with cmake, not node, so it holds nothing", each counted once across archive pairs. A Python requirement of a port that doesn't build with Python isn't checked against MacPorts' versions (batch 4). Where MacPorts can't say what the port uses, every file holds, as before.

**A build file that changed only the version it names holds nothing** (the same finding): nuspell's CMakeLists.txt changed only `project(nuspell VERSION 5.1.9 …)`. `Compare` is given the update's two versions, and where each changed line of a build file reads as the new one once the old version in it is the new one, the change is said with the line, as a license's copyright years are. A line added or removed, anything else changed, or a version other than the update's, holds.

**D12**, decided today: past that, any other change to a build file holds, as flatbuffers' change to its source lists and tests does. CMake's text isn't read for what packaging would need.

Not done here: chezmoi's `pyproject.toml` is its documentation's, and a Go port's; it's now set apart as Python's. Which manifests below the top level the build reads, as beekeeper-studio's yarn workspace, stays with batch 13.

Checked live, `update flatbuffers --plan --new` at master 2a5756b: CMakeLists.txt holds, Package.swift and package.json are said and hold nothing.

Tests:
- `TestAPortsBuildSystemsAreItsPortGroupsAndItsConfigure` and `TestTheEvaluatorReportsThePortGroupsAPortLoads`;
- `TestAChangeNamesItsFilesBuildSystem` and `TestABuildFileNamingTheNewVersionHoldsNothing`;
- `TestAChangeTheBuildDoesntReadHoldsNothing`, flatbuffers' shape, with two archive pairs.

Eleven mutations were tried. Two survivors were dead code, a check `ReplaceAll` already makes and a path depth nothing reaches, and are gone. A third was batch 2's: `yearsOnly`'s line-count check had no case, a license file without a final newline, now tested; a mutation meant for the new `versionOnly` had matched the identical lines in `yearsOnly` first, which is how it showed.
