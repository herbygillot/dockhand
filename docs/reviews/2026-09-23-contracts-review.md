# 2026-09-23: the contracts, and what they bound

A review of every contract dockhand states, written to answer one
question: which of them draw bounds that the program's purpose does not
need, and what those bounds cost. The purpose is the README's: from an
upstream release to a submitted port update, through the person's own
fork, built the way a MacPorts pull request is built, never guessing;
and the [coordination note](../instance-coordination.md)'s statement of
the domain, one Mac, one person, a few terminals.

Three kinds of contract were surveyed: the explicit ones, Go interfaces
and the exported types callers program against; the ones written into
doc comments, which say what a package or method promises and refuses;
and the ones that live as prose in the design documents under `docs/`,
where most of the rules that shape the program actually are. The
inventory is a companion document, linked at the end. The judgments are
the body.

## The contracts, by kind

**Persistence.** `state.Store` is a registry and two transaction entries;
`state.Scoped` binds one to a repository; `Reader`, `Writer`, and `Tx` are
record-specific methods with bounded queries, and the design forbids an
interface per table and a generic key/value or whole-database API. The
provider store is pool-scoped and separate. Transaction callbacks run once,
synchronously, change only records, and never call a provider, forge, Git,
or the network. The README promises that stored data is never discarded:
every schema change is a migration, seventeen so far. These contracts are
the program's spine and they fit it. Their cost is that a new record kind
is an interface change, SQL, a migration, and tests, which is a price the
promise to the person makes worth paying.

**The driver.** `proc.Engine` is a two-method view of the workflow engine:
cycle and status. A cycle claims eligible work, calls outside the write
transaction, and records under a fresh one; claims have owners,
generations, and leases; provider idempotency and reconciliation exist
because a rejected stale write cannot stop a paused process from making a
late external call. The architecture document adds the prohibitions:
`dockhand` itself is the driver, there is no separate executable, no
automatic background launch, no Unix socket or local request transport,
no process registry, singleton lock, or operating-system service. These
are decisions of the first implementation stated in the grammar of
principles. That matters, and section 3.4 says why.

**Verification.** `verify.Provider` has six methods and one strong rule,
that `Submit` is idempotent by request ID across processes and that a
request `Reconcile` has closed refuses every later `Submit`, stale
drivers included. Log reading and pruning are optional capabilities found
by type assertion. `verify/ledger` now keeps the rows and locks that rule
rests on. Evidence applicability compares the complete tree, target,
variants, platform, environment digest, verifier digest, source-build and
test choices, and the provider's frozen configuration; a miss authorizes
nothing. The verdict follows the MacPorts workflow: lint, build, and
install decide, tests are reported and not decisive unless asked. These
contracts are sound and, for the ledger and the recovery rule, unusually
well specified. What the applicability rule costs is in section 3.2.

**Evaluation and editing.** `macports.Reader` and its extensions, the
selected-only, observing, and batch readers, have one implementation, the
native evaluator, and the doc comment says so: a reader that cannot
observe cannot edit. `Tree` and `Context` bind a materialized root to a
source identity and a platform, and the platform is the host's, by
contract: a different or incomplete platform is rejected and
cross-platform simulation is deferred. The workspace projects one git tree
onto a directory and rests on an invariant the design document is honest
about: a sparse projection is correct provided the evaluation reads
nothing outside the port's directory and `_resources`, and that proviso is
supported by a survey of today's tree, not by a proof. `portedit` prepares
edits in a disposable workspace, imports git for one value, and creates no
branches, jobs, or publications. The pre-fetch guard judges a hook's text
statically because previews may run no hook. Sections 3.3, 3.5, and 3.7
take these up.

**Sources and forges.** `upstream.Catalog` binds a forge instance and
repository name to `forge.Repository`, `ReleaseRepository`, and
`FileRepository`, without observing anything at binding. `forge.Accounts`
and `forge.PullRequests` are the publication side, with the inspector
optional. GitHub implements all of it; GitLab implements tags. Publication
policy lives in `publish`, adapters know no policy, and `app` alone
constructs a client. Generic HTTP discovery is, by design, not a forge
repository. These fit MacPorts, which lives on GitHub, and they are cheap
to extend.

**Records and identity.** A change is the logical contribution with an
identity independent of branch, commit, job, and PR number; revisions are
immutable snapshots with a base; a job is one accepted request with an
immutable spec, and follow-ups get their own jobs; attempts, resources,
and publication actions have their own lifetimes; a phase is monotonic and
the store refuses any step the records' rule does not name. `record` must
not become a miscellaneous collection. Every one of these is right. Two
things ride on them that are not stated as rules but act as rules: a
contribution changes one port directory, and accepted intent carries no
alternatives. Sections 3.1 and 3.6.

**Optional capabilities.** Ten contracts are not declared but
discovered, by a type assertion at the point of use: a provider that
reads logs, prunes artifacts, or prunes log caches; a forge that inspects
pull requests, lists releases, reads files, or serves livecheck
documents; a local provider that binds images; a version probe that
evaluates in batches; and the Tart machine's guest log reader. This is
idiomatic Go and it keeps the required contracts small. Its cost is that
the compiler checks none of them: a provider that loses a method loses
the feature silently, and `--trace` simply says the log is unavailable.
One test per implementation asserting what it is expected to satisfy
would make these visible without changing their shape.

**Promises to the person.** The README and the usage document promise
that nothing is pushed to MacPorts directly, that a failed build keeps the
branch and opens no PR, that an already-current port says so and stops,
that every accepted job is durable, that Ctrl-C detaches without
canceling, that dry runs create nothing, that a PR body a maintainer
edited is kept whole, and that the tool never guesses. The CLI design
adds the shape of attachment, `--to`, `--detach`, and a single JSON
document on stdout. These are the contracts the rest exist to keep, and
none of them is questioned below.

## The invisible bounds

Each of these is a real constraint on what dockhand can be asked to do.
For each: where it is written, what it makes impossible or expensive,
whether the purpose needs it, and what relaxing it would take. They are
ordered by how much they cost the person using the tool.

### 3.1 A contribution is one port directory and one commit

Where it is written: `git/changeset.ScopeOf` refuses changed paths in two
port directories, with `_resources` allowed beside one; `changeset`
requires a candidate to be exactly one commit above its base;
`publish.UntrackedSource` derives a single-commit contribution and its
port directory; the CLI design, at line 436, refuses "root commits,
merges, multiple unpublished commits, empty changes, and changes outside
one port directory"; the README tells the person to keep a hand-made
branch to one commit in one port directory. Nothing in the principles
asks for this. It is the shape the first slice took and every later
piece inherited.

What it blocks: the most common MacPorts pull request that is not a
plain bump, a library update with the revision bumps of its dependents,
which the MacPorts guide asks for as separate commits in one PR. Dockhand
cannot prepare that, cannot adopt a branch that holds it, and cannot
publish it. Shared releases do not help: they widen the targets within
one directory, not the directories. The dependents machinery that exists
finds what to *build*, not what to *bump*, and the roadmap's automatic
dependent revision bumps are blocked here before they start.

Whether the purpose needs it: no. The one-directory rule made scope,
verification targets, and the PR body simple, and it kept the guarantee
that a contribution's evidence covers what it changed. A multi-directory
contribution keeps that guarantee as long as every changed port is a
verification target, which the coverage plan already models.

What relaxing it takes: `Scope` becomes a list of port directories with
one commit each, in order; `changeset` accepts a candidate that is N
commits above its base when each touches one directory; the PR body
lists each commit's port; `UntrackedSource` derives the list rather than
the one; and the publication content names every target's evidence. It
is one design, not a series of patches, and it is the largest capability
this program's contracts currently exclude.

### 3.2 Evidence is bound to the whole tree, the verifier's bytes, and the image's bytes

Where it is written: the principles say to bind evidence to explicit
inputs and that commit identity alone does not define build equivalence;
the architecture adds the complete tree, target, configuration coverage,
platform and environment identity, and artifact inputs; `verify/reuse.go`
compares the environment digest, the verifier digest, the capability
digest, and the provider's frozen configuration byte for byte.

What it costs: every rebase onto a newer master changes the tree, so
`rebase` always rebuilds, even when the port's directory, its PortGroups,
and everything it depends on are unchanged. Every change to the guest
script or its launch protocol changes the verifier digest, so upgrading
dockhand invalidates all prior evidence for reuse. Every `setup` that
refreshes an image changes the environment digest, likewise. None of
these is wrong; each is the conservative reading of "bind evidence to
explicit inputs", and the architecture calls the first policy
"deliberately conservative" in as many words. But the rebase case is the
one the person meets most, and it is the case where the conservative
reading is furthest from the truth: a MacPorts build reads the port, its
`_resources`, and its dependency closure, and the index already knows
that closure.

What relaxing it takes: a second, narrower equivalence key computed at
verification time, a digest over the port directory, `_resources`, and
the Portfiles of the platform's dependency closure from the staged index,
recorded on the attempt beside the tree; reuse could then accept a later
tree whose closure digest matches under the same everything else. The
whole-tree rule stays as the default and the closure rule is an explicit
`--reuse closure` a person opts into, since a PortGroup a dependency
loads at build time is the one thing the closure does not name. For the
verifier digest, a protocol version the guest and host agree on would
replace a content hash, so that a wording change in the guest script no
longer discards evidence. These are refinements, not a new design.

### 3.3 The host's platform is the only platform

Where it is written: `macports.ErrPlatform`, "evaluation requires the
native platform"; the architecture's "a requested different or
incomplete platform is rejected; cross-platform simulation is deferred";
every binding in `app` asks the evaluator for the native platform and
freezes it into the accepted build. The Tart provider then refuses an
image whose platform is not the accepted one.

What it costs: a Sonoma host verifies on a Sonoma image and nothing
else, though `setup` can prepare images for five macOS releases and the
MacPorts workflow builds every one of them. Dependent coverage is
Tart-only, the usage document says at line 190, so it is host-platform
only twice over. The GitHub provider is the
asymmetry that shows the cost: it runs the fork's whole runner matrix,
because the matrix is the workflow's and not dockhand's. On Linux the
tool "describes a Mac" and the description is one fixed platform. The
prepared edit is checked on one platform too, so a Portfile whose
checksums or versions branch on the OS is prepared for the host and
built for the host only.

Whether the purpose needs it: partly. Evaluation runs MacPorts Base on
the host, and Base derives `os.major` from the machine; the evaluator
cannot lie about it without a different Base. So the bound on
*preparation* is MacPorts' own. The bound on *verification* is not:
nothing in a Tart build requires the image's platform to equal the
platform the edit was evaluated under, only that the accepted build
record name the image's platform and that the guest confirm it.

What relaxing it takes: separate the evaluation platform from the build
platform in the accepted spec, let `choice` resolve a build for each
platform an image serves, and let a job carry several targets on several
platforms, which the plan already supports for targets. The fidelity
check stays host-bound and says so. This turns a Tart verification into
what the GitHub one already is, and it is the most valuable thing
GitHub's asymmetry points at.

### 3.4 The driver is whoever is running

Where it is written: the principles, "commands do not spawn background
drivers"; the architecture, "there is no Unix socket or separate local
request transport", "no process registry, singleton residency lock, child
driver, socket, or operating-system driver service"; `proc.Manager`'s
"claims in state allow multiple callers; there is no resident singleton
lock", pinned by tests; the state contract's "the database is the
coordination domain"; and an engine bound to one repository, with only
`gc` reaching across all.

What it costs: after a command exits, nothing advances its work until a
driver runs again; a VM finishes and its result waits. A `status` table
in another terminal sees the driving process's progress lines only as
recorded detail, and the coordination note lists which lines are not
recorded. `cancel` is durable intent that only a cycle applies. A dead
driver's claims expire on the step's deadline, ten to fifteen minutes,
because claim owners are anonymous and a peer cannot tell dead from
slow. Two database files cannot see each other's reservations. A resident
`serve` serves one checkout.

Whether the purpose needs it: for one Mac and one person, the claims
model is correct and the prohibitions kept the first implementation
honest. The [coordination note](../instance-coordination.md) has already
done the analysis and names the three redesigns and their order. What
this review adds is about the contract's wording. The prohibitions are
written as principles, in the same voice as "never guess", when they are
decisions of a first implementation with conditions under which they
would be revisited. A future session-and-lease model, or a client and a
service, would have to argue against the principles document rather than
extend it. Rewording them as decisions, with the coordination note's
pains as the conditions, costs nothing now and removes a bound that is
only in the prose.

### 3.5 Previews may run no hook, so the guard reads Tcl

Where it is written: the bump-coverage document, at line 29, as a rule
in its own right, "do not execute fetch hooks to discover whether they
mutate something"; the compatibility note, "the probe never executes a
fetch hook"; the components document, "unknown hooks or changed hook
structure require another preparer; no arbitrary hooks run during
preview"; the fetch-guard package, which judges a pre-fetch hook's text
by a grammar and an effect rule, five procedure levels deep; and,
pulling the other way, the principles, "MacPorts is the authority on
what the Portfile means".

What it costs: the guard is a static analysis of Tcl in Go, nine hundred
lines and growing with every hook shape the tree presents, and it is the
one place dockhand interprets Tcl rather than asking MacPorts to. Its
soundness is over all branches of a hook, which is more than the
preparation needs, because section 3.3 already fixes the platform: the
build that will run is the host's platform, and the only question that
matters is whether the fetch inputs dockhand computed are the ones
MacPorts will use. The Java ports, one hundred and eighty of them, stop at
an `exec` the grammar cannot admit, and the policy decision on the
roadmap is about widening the grammar again.

What relaxing it takes: the principle that MacPorts evaluates points at
the other implementation. Run the pre-fetch hook in the evaluator, on the
host's platform, in a Tcl interpreter with `exec`, `file` writes, and
network hidden behind refusing stubs, snapshot the fetch inputs before,
and compare after: a hook that changed nothing is admitted on the path it
actually took, one that called a stub is refused with the command named.
That is the same judgment the grammar makes, made by the authority, on
the one platform that matters, with no grammar to grow. The grammar's
one advantage, judging every platform's branch, buys nothing while
verification is host-platform only, and would buy a cross-platform
preparation that 3.3 says MacPorts itself cannot give. The rule against
executing hooks was written when a hook was arbitrary code with the
host's filesystem and network at hand; a hook run under refusing stubs
with its effects compared is a probe, which the same rule already runs
for everything else in the Portfile. The rule deserves the argument, not
a quiet exception, which is why the recommendation is an experiment
beside the grammar and not a replacement.

### 3.6 Accepted intent is immutable and carries no alternatives

Where it is written: "a later configuration change must not silently
change an accepted job's source, build question, or destination"; "a
cycle never fills missing build inputs from current defaults";
"explicit provider settings remain authoritative; Tart capacity pressure
never adds a GitHub submission"; the CLI design's "a configured provider
registry never falls back to a different provider" and "a full Tart
queue waits rather than switching to GitHub"; "failed builds are not
automatically retried"; and the README's "never guesses".

What it costs: there is no way to say "build this on Tart, and on GitHub
if Tart is busy or has no image", or "retry an errored build once",
because the accepted spec names one provider and one configuration and
the driver may not choose another. A job accepted with requirements and
no matching evidence ends needing attention. These are each the right
refusal of a *guess*. But a person who says the alternative in advance
is not guessing, and immutability does not forbid recording an ordered
list of what the person authorized.

What relaxing it takes: the accepted spec carries the alternatives the
person named, an ordered list of providers or a bounded retry, frozen at
intake like everything else; the driver moves to the next entry on a
refusal it is allowed to act on, and records which entry served. Nothing
about intake's immutability changes; what the person meant gets wider.

### 3.7 The sparse workspace rests on evidence, not a proof

Where it is written: the workspace design's invariant section says so
itself, with the three changes that would make the evaluator's read
trace a proof: tracing separated from declarations, callback frames
recorded, enumeration paths captured.

What it costs: nothing today, by the survey's measure. What it risks: a
PortGroup or Portfile that reads a sibling port at evaluation would be
prepared from a projection that lacks it, and the preparation would be
wrong without saying so. The design confines the risk to the one-port
preparation and refuses to index or resolve over a sparse root, which
closes the failure that would corrupt state outside the run. The
remaining exposure is one wrong edit, caught by verification, on a tree
that the survey says has no such Portfile.

What closing it takes: the three changes the design lists, and a refusal
in the evaluator when a traced read leaves the scope. The design calls
them worth making; this review agrees and puts them ahead of any other
change to the workspace, because they turn a policy into a guarantee.

### 3.8 One repository per engine; one database as the world

Where it is written: `state.Scoped`; "an all-jobs selection does not
mean the whole database"; the repository as the git common directory,
so a moved clone needs `reassociate`; "drivers coordinating shared
external resources must use the same database".

What it costs: a person with two clones sees two worlds, and `serve`
drives one. `gc` alone reaches across. This is the right default and a
cheap one to widen when needed: a status across registrations is a loop
`gc` already writes.

### 3.9 Smaller bounds, listed

- Only the initiating target selects a contribution by name; a shared
  release's other members are not aliases. Right, and stated.
- One open contribution per port: a second `bump` continues the open
  one, and a second branch adopted for a port that has one is refused.
  Right for the ordinary case, since an update that supersedes another
  should land on the same pull request. Two documents disagree about the
  edges, though: the resolution design says "one open contribution per
  port" flatly, and the target workflow says "do not enforce one open
  contribution per target globally: manually adopted branches and
  existing duplicate contributions must remain representable, with
  ambiguity surfaced". The code does the second, and the first should
  say so.
- An existing PR body is kept whole and `--update-body` replaces one
  section; dockhand cannot regenerate a body a maintainer touched. Right.
- Failed environments are retained until `gc`; ordinary cycles apply no
  age policy. Right for diagnosis; the disk is the person's.
- `status` never contacts a provider or the forge, so PR state is as
  old as the last `sync` or `serve`. Right, and the age is shown.
- Branches are named `dockhand/bump/<port>-<suffix>` and commits carry
  the generated-by trailer. Conventions, not bounds.
- The provider's frozen configuration is compared byte for byte, so a
  field added to the Tart configuration invalidates reuse until the
  payload version says otherwise. Acceptable; `ProviderVersion` exists.
- The forge is one per client, and pull requests are GitHub's. Right for
  MacPorts.

## Contracts the code no longer keeps

Prose contracts drift, and a few have:

- The README said `review` appears in `--help` but is not implemented;
  the stub was removed on 2026-09-22 and `--help` no longer shows it.
  Fixed with this review.
- The components document said the index mirror's URL "remains recorded
  provenance but is not used"; the cache seeds a cold environment from
  the mirror within its bracket and re-indexes the difference, as
  `portindex/mirror.go` documents. Fixed with this review.
- The architecture document still says rebase and amend intake "remain
  explicitly unsupported", that publication and PR collections "acquire
  persistence when their executor is implemented", and that
  branch-reassociation commands "remain later work"; all three are
  implemented. Fixed with this review. It also calls ongoing PR
  monitoring a phase-two extension and dependent scheduling later work,
  both of which now exist in first forms; those sentences are left for
  the next pass over that document, which mixes rules with status
  throughout and would read better with the status lifted out.

## Recommendations

1. Reword the driver prohibitions in the principles and architecture
   documents as first-implementation decisions with their revisiting
   conditions, so the coordination note's designs are an extension of
   the principles rather than an argument against them. Prose only.
2. Design the multi-directory contribution, 3.1. It is the largest
   capability the contracts exclude and the one MacPorts practice asks
   for most.
3. Separate the evaluation platform from the build platform, 3.3, and
   let a Tart job build on every image that serves a platform, as the
   GitHub path already does through the fork's matrix.
4. Try the evaluator-run guard, 3.5, as an experiment beside the grammar
   on the survey's refused ports, before deciding the Java `exec` policy.
   If it admits what the grammar refuses without a false admission, the
   grammar can stop growing.
5. Add the closure digest as an opt-in reuse key, 3.2, and a protocol
   version for the verifier digest.
6. Make the workspace trace a proof, 3.7.
7. Let accepted intent carry named alternatives, 3.6.

## The inventory

The full inventory the judgments rest on is the companion
[contracts inventory](2026-09-23-contracts-inventory.md): every
production interface with its methods and the rules its doc comments
state, every rule a package or type documents, and every rule the design
documents and the README state, cited by file and line. It was gathered
by two read-only passes over the tree at `f7c2a0e9` and is the record of
what the program says about itself on this date; the body above is what
that record means.
