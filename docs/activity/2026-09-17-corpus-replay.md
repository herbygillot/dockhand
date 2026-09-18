# Corpus replay after the 2026-09-17 coverage changes

The first item of the reconciled queue: replay the pinned 147-Portfile corpus and the named controls after the day's coverage changes, and settle the unexplained shared-release publication refusal. Per-port outcomes are in [the comparison table](2026-09-17-corpus-replay.tsv); the "Before" column is the 2026-09-15 baseline's "After".

## Replay

Same 147 Portfiles, at the same committed tree as the 2026-09-16 replay (`87ff2b89b`), in a git worktree, with the current binary:

| Outcome | 2026-09-15 baseline | 2026-09-16 replay | Today |
|---|---:|---:|---:|
| input-found | 85 | 83 | 119 |
| unsupported | 54 | 53 | 18 |
| unknown | 8 | 11 | 10 |

43 entries changed against the baseline, 37 of them to input-found: legacy checksum blocks (AppHack, mime, socket, cvsgraph and others), python ports through their stubs (py-plotly, py-fiona, py-openssl and the rest of the `py-` set), git-fetched and vendored Go ports (helm, terraform, codex, mrustc), and the classifier's sinks. Six entries are not input-found where the baseline was better or different:

- **aqbanking6, qjson, rocs, mythtv.27, qfsm**: all five load the qt4 PortGroup, whose `file exists ${qt_frameworks_dir}/QtCore/QtCore` reads the installed Qt to choose a build layout. Since the 2026-09-17 host-input work, a read of installed state is a gap unless it is explained, and this one is not: the PortGroup, not the Portfile, makes it, and the classifier cannot tell that the result stays in build options. They were input-found on 2026-09-16 only because that binary did not see the read. This is a deliberate classification, recorded here as its cost; explaining PortGroup-level host reads is a separate piece of work.
- **rep-gtk**: unsupported before, unknown now, for a checksum declaration no observed context covers. Not a regression in what dockhand can do; a different, more accurate reason.

Two regressions the replay caught and this change fixes: a2ps and fheroes2 read `configure.compiler` to select a patch file and a legacysupport option. Compiler-selection probes are tolerated when the Portfile's toolchain reads sit in build-only positions, and `patchfiles` and `legacysupport.` were missing from the benign set. Patch selection changes what is applied after extraction, never which archive is fetched or how it is checked, and the patch check reads the native selection; a case in the classifier's tests that refused a patch name formatted from the deployment target moved to the accepted list with that reason.

## Controls

On the current tree, each with an explicit version: `terraform-1.16 --version 1.16.2`, `deno --version 2.9.6`, `codex --version 0.155.0-alpha.15` (prerelease, reported as leaving stable), `wasmer --version 7.4.2`, `gh --version 2.83.0`, and `helm-4.2 --version 4.2.0` and `4.2.1` all pass the candidate checks. `helm` itself is the series metaport that follows `helm-4.3`, so a version on it is refused for touching an independent release, which is the intended scoping rule, not a control failure.

## The publication refusal

The second `py-idna` run's "missing shared-release target py310-idna" was examined against the records once more. Both runs' specs name `py314-idna` with identical fields, both plans hold that one root, and the revision's scope lists the stub as metadata-only with five buildable members; the coverage rule returns the one initiating member for that spec, and the pinned test agrees. The only code path that yields the message is the rule's fall-through to every buildable member when the spec's initiating target matches none of them, which the records do not show. It stays an unreproduced one-off, with the test guarding the path and the fall-through named as the only candidate mechanism.

## Throughput

`assess` now assesses its ports through a bounded pool, eight at a time on this host, each with its own interpreters, sharing only the read-only snapshot and PortIndex; results keep the selection's order. The corpus went from 217s to 28s once the tree's index existed, and the thirteen-port timing sample from 15s to 10s. Running batches as separate processes, the earlier survey's approach, is slower than one process: each pays about six seconds to materialize the tree before assessing anything. The pool imports nothing new; the package's import-boundary test keeps it that way.
