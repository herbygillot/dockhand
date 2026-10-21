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

## The engine's stand-ins, a run's

The engine's stand-ins aren't package variables any more: `testPreparer`,
`testForge`, the port, dependent, outdated, and project readers,
`testHTTPS`, `testArchiveFetcher`, `testActions`, `testLeftovers`, the
engine's Tart lookup, and `gitHubCLI`. `Run` takes none; `run` takes a
command line's `compose.Dependencies`. command's tests keep each test's
own by its name (stands_test.go: `stands(t)`, `standsFor(t)`, `runFor`),
a subtest taking its parent's. So tests no longer share them through the
package, which `t.Parallel` needs.

What's still the package's: the login's parts (`authFlow`, `authStore`,
`authAPI`, `openBrowser`), serve's and watch's timings and launchd, Tart's
images and support for setup's report, and cleanup's start. Those are the
host's, and go next, as `Host` and its `Timings`. Command's tests also
set the process's environment (HOME, MACPORTS_TREE), which `t.Parallel`
refuses. That waits for one `Invocation` carrying the environment (the
split's 7d rows 1 and 10), also in this item.

## The host's seams, a run's

The host's seams aren't package variables any more either. `host`
(internal/command/host.go) holds what a command line reads of the Mac it
runs on: serve's, watch's, and logs --follow's `timings`; serve's clock,
identity check, watched executable, notifications, launchctl, and target
OS; cleanup's start; Tart's lookup, images, and support for setup's
report; and the login's device flow, keeping, client, and browser.
`Run` gives `run` the Mac's own (`machineHost`); a test's is its own
(`hosted(t)`, from `testHost`, which holds what TestMain used to set for
every test). Settings carry it, and the functions that read it are its
methods or take its timings. The engine's Tart lookup now falls back to
the host's, so one stand-in covers both.

Kept the type unexported, as `host` and `timings`: nothing outside the
command package names it, and its fields' types (`tartImages`,
`credentials`) are the package's own.

Left for this item: the device flow to `github.Login`/`Logout`,
`engine.Setup`'s move and `offerSetup`, one `Invocation` carrying the
environment, so command's tests can take `t.Parallel`, the spend log's
variable to compose, and the `term` cut.

## The login's device flow, GitHub's

`github.Login` signs in with a device flow and keeps the login in a
store, showing the code only where GitHub's address is over https and
keeping nothing where the sign-in doesn't finish; `github.Logout`
removes it and says where at GitHub the saved login's app is revoked.
`github.LoginStore` is where it's kept, in place of command's own
`credentials` interface. setup github keeps its wording, the browser,
and the override notice. Their own tests are in
internal/github/signin_test.go.

## Setup's findings, the engine's; the login's source, GitHub's

`engine.Setup` (engine/setup.go) does what runSetup did of the checkout:
refuses a Git too old before anything's recorded, opens the engine, finds
MacPorts' remote and where master comes from, places worktrees (the
named directory, else the configured one, else the default, with
`Unchosen` where setup may ask, `Choose` for the answer, and `Remember`
for what the configuration should keep), and finds the MacPorts prefix.
Its `SetupReport` is setup's `--json` record, which command's
`setupReport` embeds beside the providers and GitHub lines, so the JSON's
fields are unchanged. runSetup asks the worktrees question and words the
report.

`github.OverridingSource` is GH_TOKEN, then GITHUB_TOKEN, as
`SystemCredentials.Token` reads them, in place of command's
`overridingToken`. `github.LoginSource` is where a login comes from
without asking GitHub, the variable or a kept login that hasn't ended,
which setup's providers line, its Next: steps, and its login offer each
walked themselves (the split's 7a row 2, left partial). One change in
what's said: a kept login that has ended (an earlier dockhand's, or past
six months) now reads as none in the providers line and the Next: steps,
as the offer already took it; the publishing line still says why.

offerSetup is left as questions: its rules are now github's
(`LoginSource`), the engine's (`SuggestMaintainerAtMaster`), and Tart's
(`HasHostImage`, `HostRestorable`).

The boundary test's rows for buildenv/tart, github, credential, and
keychain say "setup's, until part 4.2", and buildenv's and ghactions'
say the vocabulary check's words use (architecture, at 970b86e9).

## The environment's catalogue

`config.Environment` lists every variable dockhand reads, each marked
where serve's agent keeps it (`config.AgentVariables`), in place of
serve.go's hand-kept `agentVariables` (the split's 7d row 10). Two tests
keep it true: usage.md's Environment table and its sentence on the agent
name exactly the catalogue, and every `os.Getenv` or `os.LookupEnv` of a
literal name in internal/ is in it (PATH, DOCKHAND_FAILPOINT, and
DOCKHAND_TEST_* aside). The first found usage.md four rows short,
DOCKHAND_GITHUB_LOG, DOCKHAND_READING_CACHE, VISUAL, and EDITOR, and its
agent sentence missing DOCKHAND_READING_CACHE, which the agent already
kept; both are fixed.

Next, the Invocation: one environment and working directory for a
command line, which compose resolves into explicit fields once, so that
nothing below it reads the process's environment and command's tests can
run in parallel (proposed to architecture, 2026-10-21, and agreed with
its rules: resolved fields for what's read, one explicit `Env []string`
for what's handed on to git, port, tart, and guest SSH through
`subprocess.Spec.Env`; a boundary test below compose for `os.Getenv`,
`os.LookupEnv`, `os.UserHomeDir`, `os.Environ`, and bare `exec.Command`,
first with today's readers allowed, then shrinking; config keeping only
the catalogue, with meaning and default, its `Path()` resolved in
compose; token precedence compose's, github taking a `Token` and a
`LoginLockPath`; and `Invocation.Dir` in place of `t.Chdir`).

The boundary test landed first (internal/compose/environment_test.go):
below compose, apart from command and testsupport, each package's calls
to `os.Getenv`, `os.LookupEnv`, `os.UserHomeDir`, `os.Environ`,
`os.Getwd`, `exec.Command`, and `exec.CommandContext` are counted against
`readersBelow`, which holds today's: tart 7, config 4, engine 4, github 4,
tart/guestssh 3, buildenv/script 2, git 2, and one each in archive,
buildenv/tart, macports/portindex, tart/host, and tcl/shell. A count may
only fall, and must be lowered as it does. subprocess (where every
program starts, with the Env it's given), forgetest, and the acceptance
build's failpoints stay.

## The command line's invocation

`Run` makes an `invocation`, the arguments with `os.Environ()` and
`os.Getwd()`, and nothing in command reads the process's environment
after it: settings carry the invocation's environment and directory
(`getenv`, `places`), and compose reads them in place of the process's
(`compose.Options.Env` and `Dir`, with `Getenv` and `Places`). The tree
defaults to the invocation's directory, MACPORTS_TREE's worktree is found
from it, and diff's paths are read against it.

config reads nothing itself. `config.Places`, a home and a working
directory, carries what read HOME and the working directory:
`ConfigPath` (in place of `Path`, $DOCKHAND_CONFIG's value made absolute
against the directory, else ~/.dockhand/config.toml), `Load` with the
worktrees' ~ expanded (plain `Load` keeps the ~ as written), `Tilde`, and
`Absolute`. The writers that abbreviate a path take the places. serve
--install's agent, its LaunchAgents folder, its database path, and the
variables it carries are the invocation's; so are setup github's client
ID and the editor.

The boundary test's config row is gone. The catalogue's test now also
reads `Getenv`/`getenv` calls, which caught compose's HOME; HOME is
exempt with PATH.

Tests still set the process's environment and run with it; the readers
below compose are next, then the tests' own environment.

## The engine's home and caches, compose's

engine reads no environment. `engine.Options` gains `Home`, which
`DefaultWorktrees` is under (beside the clone where it's empty), and
`IndexMirror`; `Indexes`, the port index cache, is now required where an
index is staged, and `ReadingCache` and `IndexCache` are gone. compose
resolves all of them from the invocation: $DOCKHAND_READING_CACHE and
$DOCKHAND_INDEX_CACHE made absolute against its directory, else
`dockhand/readings` and `dockhand/indexes` in the user cache directory,
which `Options.cacheDirectory` finds as `os.UserCacheDir` does
(~/Library/Caches on macOS, else $XDG_CACHE_HOME or ~/.cache), from the
invocation's HOME. The boundary test watches `os.UserCacheDir` too, and
engine's row is gone. engine's own tests had cached port indexes in the
person's real cache directory and read their real home; the fixture now
has its own of both.

## GitHub's login, reached as compose says

github reads no environment. `github.Signin` is how the person's login
is reached: an overriding `Token`, the `Store`, the renewal's `Lock`, and
the GitHub CLI's path, `CLI`. `SystemClient`, `NewRuntime`, and
`LoginSource` take one; `SystemCredentials` carries `Override`, `Lock`
(a renewal is refused without one), and `CLI` (none where empty), and
`github.CLI` with no `Path` isn't installed. `OverridingSource` and
`LoginLock` are gone, and `Spend.Log` takes the log's path.

compose's `Options.Signin(store)` holds the precedence, stated in the
catalogue: GH_TOKEN's token, trimmed, else GITHUB_TOKEN's, ahead of the
kept login, then gh, which `Options.LookPath` finds on the invocation's
PATH; the lock is ~/.dockhand/github-login.lock under the invocation's
home. `Options.SpendLog` is $DOCKHAND_GITHUB_LOG made absolute. The
engine's GitHub CLI is gh on that PATH too.

In command, setup's login readers (`providerLines`, `githubReadiness`,
`missingSteps`, `publishing`, `githubLogin`, `githubLogout`,
`tartReadiness`) are settings' methods, reading `settings.signin()`, the
invocation's Signin over the host's keeping; `host.authAPI` and
`compose.Dependencies.GitHubClient` take a Signin. github's own tests
give the token, store, and gh as fields, and its precedence tests run in
parallel; compose's signin_test holds the precedence. The boundary test's
github row is gone.

## Tart's homes and keys, compose's

tart, tart/provision, tart/guestssh, and buildenv/tart read no home or
variable. `compose.Options.TartPlaces` resolves dockhand's Tart home
($DOCKHAND_TART_HOME or ~/.dockhand/tart), the person's ($TART_HOME or
~/.tart), and the SSH keys' folder ($DOCKHAND_SSH_DIR or
~/.dockhand/ssh) from the invocation, and `Options.TartProvider` makes
the provider in them, for the engine and for setup's report alike.
`buildenv/tart.Provider` gains `Personal` and `Keys`; `provision.Config`
`Personal` and `Keys`; `tart.Client.Resolve` refuses an empty home;
`tart.PersonalHome` and `guestssh.DefaultKeys` are gone; and
`tart.StateDirectory(home)` is the directory the home is in, which is
~/.dockhand for ~/.dockhand/tart and the scratch directory for the
acceptance test's DOCKHAND_TART_HOME, as before. Every caller of the
state directory passes dockhand's own home, so the locks and records
stay where they were. tools/facts places its Tart through compose.

The provisioning code's pin (`setupPin`) is updated, with
`SetupProtocol` kept at 3: what's given changed, not what an image
holds. tart's and provision's tests give their homes and run in
parallel where they set nothing. The boundary test's tart and guestssh
rows are down to each one `os.Environ`, which the Env slice takes next.
