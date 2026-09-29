# 2026-09-28: ports on mirror groups compared, and a refused ready

The hugo exercise's sshuttle run ([review](../reviews/2026-09-28-hugo-bump-exercise.md#sshuttle-through-paths-not-yet-taken), findings 1 and 2; verified in the roadmap's Reviews).

## Ports on mirror groups compared (finding 1)

An update of sshuttle, a python PortGroup port fetched from PyPI, got no upstream comparison: "the current version's archives could not be fetched: … only direct HTTP(S) or FTP master sites are supported". What couldn't be compared holds (D4), so every `bump` of it would stop for a person.

The new version's archives were fetched fine. They come from MacPorts' own fetch plan for the Portfile, which the evaluator observes: `portfetch::checkfiles` and its URL map, with each mirror group expanded as MacPorts expands it. The current version's came from `archives.Sources`, dockhand's own reading of `master_sites`, which takes only direct URLs. A mirror group, such as `pypi:s/sshuttle`, `gnu`, or `sourceforge:…`, has no host, so it was refused.

That reaches far past PyPI. In the ports tree, 2,569 python PortGroup ports rely on its default `master_sites pypi:…`, 2,029 perl5 ports on CPAN's, and 3,106 more Portfiles name only mirror groups: about a third of the tree, none of which could be submitted unattended.

**What changed:**
- The current version's archives now come from MacPorts' fetch plan too (`shippedPlan`): the port's native observation, under the same archive policy as before.
- `archives.Store.Shipped` takes a fetch plan, each archive with its locations. It fetches from the first location that serves the archive, as MacPorts' own fetch does, then holds it to the Portfile's checksums, and falls back to MacPorts' distfiles mirror as before.
- A missing plan is the observation's to explain (`macports.PortObservation.FetchPlan`), with MacPorts' own reasons.
- `diff --archive` fetches from the same plan, through the evaluator's observation rather than its plain evaluation.
- The dependency path of Go and Cargo ports, whose sources are chosen differently, keeps `archives.Sources` and passes its direct locations through `archives.FetchPlan`.

**Tests:**
- `TestAnUpdateOfAPortOnAMirrorGroupIsCompared` defines a mirror group in the fixture tree's own `_resources/port1.0/fetch/mirror_sites.tcl`, as the ports tree defines PyPI's, and updates a port that fetches through it. MacPorts expands the group, and the current version's archive is compared. With the old reading, the test fails with sshuttle's error.
- `TestShippedTakesTheFirstLocationThatServes` covers the order and the mirror when there are no locations.
- `TestTheShippedPlanKeepsTheFetchPolicy` covers the policy.
- The stealth-update test for `diff --archive` covers a missing plan, and the policy there.

Eight mutations each fail a test. An update doesn't reach the policy check before the plan in practice, since the new version's archives are held to the same policy first. It keeps what the old reading enforced, so it has a direct test.

**Seen, not changed:** the dependency path of a Go or Cargo port still takes only direct master sites. Those ports fetch from GitHub or crates.io, whose URLs are direct.

## A refused ready says what to do (finding 2)

`submit --ready` on macports-ports failed: GitHub refused the mutation that takes a draft out of draft, since "the `macports` organization has enabled OAuth App access restrictions". dockhand passed that on with nothing to do about it. The description had been refreshed in the same run, so the pull request was left a draft.

The error now says it's still a draft, and how to finish: on its page, or with `gh pr ready <n> --repo <repository>`. The GitHub CLI signs in as its own app, which the organization allowed where it refused dockhand's. The GitHub layer's part of the message no longer repeats which pull request it was marking.

Not changed: which login dockhand uses. Making that one call with the GitHub CLI's login when dockhand's is refused would change what acts for the person, which is theirs to decide.

`TestSubmitCheckPassingAndReady` now has GitHub refuse first, and checks the whole message and that nothing was marked.
