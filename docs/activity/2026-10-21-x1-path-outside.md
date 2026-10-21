# X1: a branch found from outside the ports tree

Part 1's item 6, its first step, at the architecture thread's order: (c),
then the port reader to macports/treequery, then the GitHub runtime and
compose.

`dockhand path <branch>` run outside any checkout failed "not a MacPorts
ports tree", though the branch's worktree was recorded, since the tree is
opened before the database (the exiv2 field notes).

- `Store.Repositories` lists the Git common directories registered,
  oldest first.
- `engine.Checkouts` opens the database without a tree and gives each
  registered clone whose .git is still there.
- A command naming a branch, by `-b`, `-p`, or `--pr`, or `path`,
  `status`, and `archive` by its name, takes the one registered checkout.
  That's where no `--tree` or `MACPORTS_TREE` is given, and the directory
  isn't a ports tree or isn't in a Git checkout at all (`notCheckout` is
  `ErrNotPortsTree` too). With several registered, it refuses, naming
  them. `path` alone still means here.

## The port reader leaves engine (CX-5's package half)

At the architecture thread's word: `internal/macports/treequery`, not
`sourcequery`, since "source" at the top level means upstream's.

- `treequery.Reader` is engine's `evaluatedPorts`: a directory's ports,
  its family, a port's directory, the index's spellings, dependents, the
  ports each directory defines, and the dependency graph. With it come
  archive planning and fetching (`FetchArchives`, `ArchivePlan`,
  `FetchPlanned`), and the types its answers are, `Dependent`,
  `FetchedArchive`, `ErrNoArchives`, and `ErrNoPort`, which engine names
  as treequery's and doesn't alias. `NotInTree` says a port a tree
  hasn't, for a reader standing in for this one.
- distfetch's `OwnArchives` went with its one caller, as treequery's
  `ownArchives`, and distfetch no longer imports depblock.
- treequery imports nothing of engine, whose interfaces it satisfies by
  its methods. engine still builds it as its default reader, in
  `portReader`, until compose fills it. engine's boundary test names it.
- Its tests moved with it: dependents in the index, fetching from the
  mirror after a stealth update, the fetch plan's refusals, and an
  unknown name being no port.

## One GitHub runtime, its first slice (CX-11)

`github.Runtime` holds the process's one GitHub client, which the
architecture thread placed in internal/github. The engine's forge,
upstream discovery's GitHub catalog, the source archiver (discovery
itself), and create's project inspection share one forge client made
from it, `Engine.github`, made once, where each call made a new client
with its own login and rate limits. command's settings hand the engine
the runtime that Actions uses too. `dropForge`, serve's account change,
is now `Runtime.AccountChanged`: the client forgets its authenticated
API client, the credentials it held, and what it remembered of finding
none, in one place for everything sharing it.

Then:

- The forge's archive bodies are fetched over the runtime's HTTP, as
  Actions' logs are.
- The pending login, a renewal the store didn't take, is the runtime's,
  not the process's (R3-32). `SystemClient` gives its credentials one,
  and `Runtime.Identity` watches the login from the client's own
  credentials. So serve's identity watcher, which now reads the engine's
  runtime, shares it. The process-global map and `SystemIdentity` are
  gone.

Then, two of field testing's from Later (2026-10-19, at 6dd7745c),
`outdated` over ryandesign's 2,133 ports and subports having said ~5,500
calls of 5,000 and gone on:

- A look weighs its estimate, about 2.6 requests a port, against what's
  left of the hour. That count takes subports sharing a Portfile once.
  GitHub's rate_limit endpoint, which counts against nothing, says what's
  left (`Runtime.Allowance`, `Engine.GitHubAllowance`). Past it, the look
  asks first on a terminal. Without one, it's refused unless
  `--over-allowance`, on outdated, update, and ship, says to go on. The
  outdated service asks before its first lookup (`Service.Allow`). Where
  GitHub can't be asked, the look goes on as it did.
- Subports of one Portfile that share its version and the options naming
  its source are looked up once, the rest waiting for the first's answer,
  each with its own copy of the release (`sharedLookups`). One cut short by
  its own context isn't shared.

Still to come in this step: the spend log's environment half moving to
compose.

## compose, its first slice

`internal/compose` assembles the engine for a command. `Settings`
resolves the engine's options from the command line's, the environment,
and the configuration file. `Open` opens the engine and gives it its
builds, providers, GitHub runtime, and the stand-ins a test hands it
(`Dependencies`). It also falls back to the registered checkout for a
command naming a branch, as settings did. command imports compose, the
architecture thread's acceptable alternative, so command's own tests can
give it stand-ins without an import cycle. The person's answer on a
migration under a live process stays command's, which asks and opens
again with `MigrateUnderLive`.

For now settings hands compose the stand-ins the tests set as package
variables (`settings.dependencies`). Making them per-test, so command's
tests can run with `t.Parallel`, is the next slice, with the host's
seams and their `Timings`.
