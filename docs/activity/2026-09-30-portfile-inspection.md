# 2026-09-30: a conservative Portfile inspection

Item 6's third piece: the private-helper review's finding 2, with its follow-up's criteria, and the git run's finding 5.

Three readers interpreted Portfile source with regular expressions. The planner decided a change was revision-only by deleting every line that looked like `revision N` from both versions, so "revision 1" becoming "revision 2" inside a `set` value, data, passed for a revision bump, which `--accept` then lets a failure through for. The shared-code traversal read `PortGroup` lines with a regex and `strings.Fields`. And `commitrules` read a port's version with two regexes, which tidy used to name a bump made by hand: they knew five forges' setup lines but not `go.setup`, could return a substitution, and took the first setup line anywhere, so git-devel's `github.setup` in its subport block was read as git's version, and tidy couldn't name git's own bump.

`macports/portfile` now has an inspection built on the Tcl parser (`inspect.go`). It reads commands only where MacPorts would run them: a control structure's bodies, as the parser knows them, and a procedure's, platform's, variant's, or subport's block; everything else, a `set` value included, is data. It says when it can't prove something:
- `RevisionOnly`: with each revision command MacPorts runs set aside, with its line where it stands alone on one, the rest of the two sources must be the same, byte for byte. A revision added where there was none is still revision-only, as a revbump of a port at revision 0 is;
- `DeclaredVersion`: the version a port declares, literally and unconditionally, the last declaration MacPorts runs winning, from a `version` command or any PortGroup's setup command that carries one, `go.setup` among them (the setup positions are now one function, `setupVersionIndex`, which the editor's version candidates use too). A subport's own declaration in its literal block comes first, and otherwise it has the main port's; another subport's block is never read for it. A version that's computed or conditional isn't proven;
- `DeclaredRevision`: the main port's literal revision and its line;
- `PortGroupReferences`: the PortGroups a Portfile or PortGroup loads, named literally where MacPorts would run the command, with whether that's all it could load: not where a PortGroup's name or version is computed, or `_resources` is mentioned outside a comment.

The planner's revision-only decision, its shared-code traversal, `commitrules`' revision-reset rule, and tidy's subject for a hand-made bump read these. The engine keeps the tree reads and the final target kind; `commitrules` keeps the rule.

Tests:
- `TestARevisionOnlyChangeIsProvedFromTheSource`, with the review's `set contents` reproduction, a subport's and a condition's revision, and a revision added or removed;
- `TestADeclaredVersionIsTheOneThePortDeclares`, with git's shape, `go.setup`, computed, conditional, braced, and bare non-version words, and a subport's own and inherited;
- `TestADeclaredRevisionIsTheMainPorts` and `TestPortGroupReferencesAreReadFromTheSource`;
- `TestTidyNamesAHandMadeBumpByThePortsOwnVersion`: "jq: update to 1.8.2", not its subport's setup line's 1.9.0;
- the command's diff test, whose revision added to a Portfile without one is revision-only.

Seven mutations each fail a test, after two cases were added for survivors.
