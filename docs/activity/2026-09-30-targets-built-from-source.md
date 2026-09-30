# 2026-09-30: every target built from its source, and each variant's build from clean work

The dogfood run's `check --variants each` of s2n-tls ([s2n-tls, and check --variants](../reviews/2026-09-28-hugo-bump-exercise.md)) found two bugs in the Tart guest program. One of them was serious: checks had passed targets they never built.

## A target installed from MacPorts' packages (finding 2)

The guest installed each target with `port -dk install` (`-dkn` without variants), and MacPorts' default, `buildfromsource ifneeded`, takes a published archive wherever one matches the port's version, revision, and variants. check-52's s2n-tls default build went "Fetching archive for s2n-tls", from packages.macports.org, straight to "Installing", with no configure, build, or destroot. The archive is master's Portfile built by the buildbots, not the branch's.

The run scanned every check log and found the same in check-38, on macOS 12 and 26, for ttyd, luv, luv-luajit, uvw-headers, and uvw-static. It found it again in check-43, for uvw-headers and uvw-static. All of them were `--also` dependents of libuv #34620, which are there to be rebuilt against the branch's libuv, and were never rebuilt. The pull request's checklist had said "dockhand builds from source as MacPorts CI does".

Only a target whose version, revision, and variants match a published archive was taken this way:
- every unchanged `--also` port;
- a changed port whose version and revision didn't move, for a lint fix, a dependency spec, a non-default variant, or platforms.

A version or revision bump names an archive no buildbot has made.

Now the target installs with `-s` (`-dks`, `-dkns`): built from its source, as CI's `mpbb install-port --source` builds it. Its dependencies are installed in the step before, from archives as before, and `-s` covers only what that one install brings, as a variant's own dependencies.

## Each variant's build from clean work (finding 1)

The guest kept each target's work directory (`-k`), and nothing cleaned it before the next target of the same port. So check-52's `+tests` build was refused: "Requested variants "+tests" do not match those the build was started with: "+debug"". `--variants each` couldn't get past its second variant. Each target's earlier work is now cleaned before it builds (`port clean --work`), as CI cleans up between ports.

## What was recorded before

The guest program's changes raise `VerifierProtocol` to 2, which is part of every Tart environment's identity. So:
- no result the guest recorded under protocol 1 is reused;
- none of those results stands for a check any more;
- status says the environment was made again, and asks for another check.

That's deliberate. Any of them may be an archive install taken for a build, and they can't be told apart. Branches checked before this need checking again.

## A passing target's tests said (finding 3)

check-53 printed "s2n-tls +tests ✓" of 284 required tests that passed. A target whose tests ran and passed now reads "✓ tests passed", on the terminal and in the pull request, as failing ones already did.

## Tests

- `TestTheGuestBuildsEachTargetInCIsOrder` covers the clean, and `-dkns` for the target's install.
- `TestTheVerifierProtocolCoversTheGuestProgram` and `TestAnEnvironmentsIdentityIsItsImagesOrigin` cover protocol 2.
- `TestTargetWordsAreDesignV3s` covers "✓ tests passed".
