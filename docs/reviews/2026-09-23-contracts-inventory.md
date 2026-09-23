# 2026-09-23: the contracts inventory

The record behind the [contracts review](2026-09-23-contracts-review.md):
what dockhand says about itself on this date, gathered by two read-only
passes over the tree at `f7c2a0e9`. The first part is every production
interface with its methods and the rules its doc comments state, then
every rule a package or type documents, cited by file and line. The
second part is every rule the design documents and the README state,
cited the same way. Near-duplicates within one document are merged; the
same rule stated in two documents appears under each. Nothing here is a
judgment; the review is where the judgments are.

## Interfaces

- **credential.Store** (`internal/credential/credential.go:21`) — `Get`, `Put`. No rule-bearing doc comment.
- **credential.DeviceFlow** (`internal/credential/credential.go:32`) — `Authorize`. No rule-bearing doc comment.
- **credential.Remover** (`internal/credential/credential.go:36`) — `Delete`. No rule-bearing doc comment.
- **proc.Engine** (`internal/proc/manager.go:11`) — `Cycle`, `Status`. No doc comment.
- **verify.Provider** (`internal/verify/provider.go:92`) — `Capabilities`, `Submit`, `Reconcile`, `Observe`, `Cancel`, `Release`.
  - "Submit enforces capacity and is idempotent by request ID across processes." — `provider.go:94`
  - "Reconcile returns an existing run, durably closes a request to further submission, or reports uncertainty." — `provider.go:96`
  - "A closed request must reject every later Submit, including calls from stale drivers." — `provider.go:97`
- **verify.LogReader** (`internal/verify/provider.go:110`) — `ReadLog`. No doc comment.
  - Optional capability: asserted via `p.(verify.LogReader)` at `internal/cli/progress.go:36,150`.
- **verify/github.actionsAPI** (unexported) (`internal/verify/github/config.go:25`) — `Workflow`, `Runs`, `Run`, `Jobs`, `JobLog`, `Rerun`. No doc comment.
- **verify.ArtifactPruner** (`internal/verify/artifacts.go:16`) — `PruneArtifacts`.
  - "removes diagnostic files only after resource release." — `artifacts.go:13`
  - "Calls must be idempotent and serialized against provider operations and log reads." — `artifacts.go:13-14`
  - "Durable execution identities and results must survive pruning." — `artifacts.go:14-15`
  - Optional capability: asserted via `c.Provider(...).(verify.ArtifactPruner)` at `internal/workflow/retention/resources.go:103`.
- **verify.LogCachePruner** (`internal/verify/artifacts.go:29`) — `PruneLogCache`.
  - "removes only local, re-downloadable diagnostics for an eligible terminal attempt selected by workflow." — `artifacts.go:25-26`
  - "It never removes evidence or remote logs." — `artifacts.go:26`
  - "a busy cache reports ErrCacheBusy" — `artifacts.go:27-28`
  - "Dry runs must not initialize storage." — `artifacts.go:28-29`
  - Optional capability: asserted via `c.Provider(...).(verify.LogCachePruner)` at `internal/workflow/retention/logs.go:46`.
- **verify/tart.machine** (unexported) (`internal/verify/tart/provider.go:34`) — `Version`, `Environment`, `InspectCapabilities`, `Running`, `Clone`, `Start`, `Ready`, `Stage`, `Launch`, `Inspect`, `Logs`, `Stop`, `Delete`. No doc comment.
  - Optional capability (distinct, ad hoc): `o.machine.(interface{ ReadLog(...) })` asserted at `internal/verify/tart/logs.go:34`.
- **state.ProviderStore** (`internal/state/provider.go:12`) — embeds `ImageCache`, `ImageCapabilityCache`; `ProviderPool`, `RegisterProviderPool`, `ProviderView`, `ProviderUpdate`.
  - "It is implemented by the same backend as Store, not an independent lock service." — `provider.go:11`
- **state.ProviderReader** (`internal/state/provider.go:20`) — `Execution`, `Occupied`. No doc comment.
- **state.ProviderTx** (`internal/state/provider.go:24`) — embeds `ProviderReader`; `PutExecution`. No doc comment.
- **state.ImageCache** (`internal/state/provider.go:35`) — `ImageDigest`, `PutImageDigest`. No doc comment on interface (related type `ImageDigest` is "a disposable observation of an image's content at one file stamp" — `provider.go:29`).
- **state.ImageCapabilityCache** (`internal/state/provider.go:40`) — `ImageCapabilities`, `PutImageCapabilities`. No doc comment.
- **state.Maintenance** (`internal/state/maintenance.go:9`) — `Backup`, `Check`.
  - "operates on the whole database, independently of repository or provider setup." — `maintenance.go:8`
- **state.Store** (`internal/state/store.go:34`) — `FindRepository`, `RegisterRepository`, `Repositories`, `View`, `Update`.
  - "Repositories lists every registration in registration order, so maintenance can reach work whose checkout no longer exists." — `store.go:37-38`
- **state.Scoped** (`internal/state/store.go:48`) — `Repository`, `View`, `Update`.
  - "a store bound to one registered repository: what every workflow operation reads and writes." — `store.go:44-45`
  - "The binding is made once, by Bind, so no operation chooses a scope again; the store still refuses an empty registration at each call." — `store.go:45-47`
- **state.Reader** (`internal/state/store.go:111`) — `PublicationForJob`, `ActivePublicationForHead`, `PullRequest`, `VerificationCandidates`, `Change`, `OpenChangeByBranch`, `Revision`, `Request`, `Job`, `JobForRequest`, `Attempt`, `Plan`, `Submission`, `Resource`, `Jobs`, `Changes`, `Revisions`, `AttemptsForJob`, `SubmissionsForAttempt`, `ResourcesForAttempt`, `Resources`, `Control`, `Controls`, `DueJobs`, `JobHistory`, `OpenContributions`, `OwedCleanups`, `CleanupCandidates`.
  - "ActivePublicationForHead finds the pending, applying, or uncertain publication holding a remote head branch, if any." — `store.go:113-115`
  - "DueJobs lists the jobs whose next action is due ... a nil selection means every job, an empty one none. It is bounded, not paged." — `store.go:138-140`
  - "JobHistory lists one contribution's jobs, newest accepted first." — `store.go:142-143`
  - "OpenContributions lists open contributions with a pull request whose next look is due ... longest waiting first." — `store.go:144-146`
  - "OwedCleanups lists merged contributions with a branch cleanup side still pending, oldest first." — `store.go:148-149`
  - "CleanupCandidates lists terminal jobs finished at or before the time ... the retention sweep's page." — `store.go:151-154`
- **state.Writer** (`internal/state/store.go:157`) — `PutPublication`, `PutPullRequest`, `PutChange`, `PutRevision`, `PutRequest`, `PutJob`, `PutPlan`, `PutAttempt`, `PutSubmission`, `PutResource`, `PutControl`, `ApplyControl`. No doc comment.
- **state.Tx** (`internal/state/store.go:172`) — embeds `Reader`, `Writer`. No doc comment.
- **tcl/syntax.Item** (`internal/tcl/syntax/types.go:12`) — `item()` (unexported marker method). No doc comment.
- **tcl/syntax.Segment** (`internal/tcl/syntax/types.go:32`) — `segment()` (unexported marker method). No doc comment.
- **tcl/syntax.Expr** (`internal/tcl/syntax/expr.go:9`) — `expr()` (unexported marker method).
  - "The tree gives a reader the shape of a condition ... without evaluating anything." — `expr.go:6-8`
- **github.TokenSource** (`internal/github/auth.go:20`) — `Token`. No doc comment on the interface itself.
- **forge.PullRequestInspector** (`internal/forge/pullrequest.go:18`) — `Inspect`.
  - "reports mergeability, review, and check status for a pull request." — `pullrequest.go:16-17`
  - "A forge that cannot report these is simply not an inspector." — `pullrequest.go:17-18`
  - Optional capability: asserted via `e.PullRequests.(forge.PullRequestInspector)` at `internal/workflow/contribution_lifecycle.go:137`.
- **forge.Accounts** (`internal/forge/pullrequest.go:25`) — `Name`, `Authenticate`, `AuthenticatedUser`, `NameFromRemote`, `RepositoryInfo`.
  - "is what a forge knows about names: its own, the caller's, the repository a remote URL names, and a repository's facts." — `pullrequest.go:22-23`
  - "Authenticate checks the credentials and nothing else." — `pullrequest.go:23-24`
- **forge.PullRequests** (`internal/forge/pullrequest.go:35`) — `Find`, `Observe`, `Create`, `Update`.
  - "finds, observes, and writes pull requests." — `pullrequest.go:33`
  - "One that can report a pull request's status is also a PullRequestInspector." — `pullrequest.go:34`
- **forge.Repository** (`internal/forge/repository.go:34`) — `Name`, `Tag`, `ListTags`.
  - "binds tag observations to one validated repository." — `repository.go:31`
  - "Catalog methods return complete observations or an error, never partial success." — `repository.go:32`
  - "Tag resolves an exact name to a commit; an absent ref reports ErrNotFound." — `repository.go:33`
- **forge.ReleaseRepository** (`internal/forge/repository.go:40`) — `Releases`. No doc comment.
  - Optional capability: asserted via `repository.(forge.ReleaseRepository)` at `internal/upstream/latest.go:55` and `internal/upstream/livecheck.go:68`.
- **forge.FileRepository** (`internal/forge/repository.go:47`) — `File`.
  - "reads one file of the repository at a commit, for manifests such as go.mod that a git-fetched port never downloads." — `repository.go:44-46`
  - "An absent file reports ErrNotFound; the read is bounded by limit bytes." — `repository.go:46`
  - Optional capability: asserted via `repository.(forge.FileRepository)` at `internal/upstream/manifest.go:31`.
- **macports.Observer** (`internal/macports/observation.go:61`) — `Observe`.
  - "adds request-scoped source and fetch observations to native metadata." — `observation.go:60`
- **macports.Projection** (`internal/macports/context.go:20`) — `EnsurePort`, `EnsureAll`, `Whole`.
  - "A sparse workspace holds a port's directory and _resources and brings more when a consumer asks; a plain materialization holds everything and needs nothing." — `context.go:16-18`
- **macports.Reader** (`internal/macports/reader.go:30`) — `Evaluate`, `Resolve`.
  - "a reader materializes what it resolves ... so the target it returns can be evaluated where it was resolved." — `reader.go:27-29`
- **macports.SelectedReader** (`internal/macports/reader.go:37`) — `EvaluateSelected`.
  - "evaluates only the selected port for counterfactual probes. Whole-Portfile validation continues to use Reader.Evaluate." — `reader.go:35-36`
- **macports.BatchReader** (`internal/macports/reader.go:43`) — `OpenBatch`.
  - "opens one evaluator for many probes of the same tree ... instead of starting an interpreter per probe." — `reader.go:41-42`
- **macports.Batch** (`internal/macports/reader.go:48`) — `Evaluate`, `EvaluateSelected`, `Close`.
  - "evaluates within one interpreter until closed." — `reader.go:47`
- **macports.Evaluator** (`internal/macports/reader.go:58`) — embeds `Reader`, `SelectedReader`, `Observer`, `BatchReader`.
  - "Every edit is judged across observed contexts, so a reader that cannot observe cannot edit; the one implementation is eval.Evaluator." — `reader.go:56-57`
- **macports.NativeEvaluator** (`internal/macports/reader.go:66`) — embeds `Evaluator`; `NativePlatform`.
  - "is an Evaluator that also reports the native platform." — `reader.go:65`
- **macports/portedit.ManifestSource** (`internal/macports/portedit/prepare.go:112`) — `Manifest`.
  - "reads one file of a port's source repository at the resolved release." — `prepare.go:110-111`
  - "An absent file reports macports.ErrManifestMissing." — `prepare.go:111`
- **macports/portedit.snapshotEvaluator** (unexported) (`internal/macports/portedit/source.go:381`) — `Evaluate`, `EvaluateSelected`.
  - "the reader used for one evaluation: the service's evaluator, or a session bound to the workspace tree." — `source.go:379-380`
- **macports/portindex.Source** (`internal/macports/portindex/source.go:17`) — `Index`.
  - "Consumers that resolve names, discover dependents, select a survey's ports, or pack a source for verification ask a Source and never stage for themselves." — `source.go:15-16`
- **workflow.ReleaseResolver** (`internal/workflow/engine.go:110`) — `ResolveRelease`. No doc comment.
- **workflow.SourcePreparer** (`internal/workflow/engine.go:114`) — `Prepare`. No doc comment.
- **workflow.DependentDiscoverer** (`internal/workflow/dependents.go:13`) — `Discover`. No doc comment.
- **workflow/choice.Local** (`internal/workflow/choice/choice.go:30`) — `BuildConfig`.
  - "fails with verify.ErrImageUnavailable when no image serves it and with verify.ErrExecutableUnavailable when Tart itself is absent." — `choice.go:27-29`
- **workflow/choice.LocalImages** (`internal/workflow/choice/choice.go:36`) — `BuildConfigForImage`.
  - "for a dependent whose image a person chose." — `choice.go:35-36`
  - Optional capability: asserted via `p.Local.(LocalImages)` at `choice.go:104`.
- **workflow/choice.Remote** (`internal/workflow/choice/choice.go:42`) — `BuildConfig`.
  - "configures a build on GitHub's workflow, pushing the candidate to the person's fork." — `choice.go:40-41`
- **tart/provision.machine** (unexported) (`internal/tart/provision/provision.go:59`) — `LockSetup`, `Images`, `Pull`, `Clone`, `Configure`, `Start`, `BootstrapAgent`, `ReadyAgent`, `EnsureToolchain`, `InstallXcode`, `InstallMacPorts`, `WriteManifest`, `Validate`, `Stop`, `Delete`, `Rename`, `Adopt`. No doc comment.
- **tart/provision.imageManager** (unexported) (`internal/tart/provision/adopt.go:11`) — `Images`, `Clone`, `Rename`, `Delete`. No doc comment (rule lives on function `adopt`, see Part 2 note under proc/tart if needed: "Keep the previous image until its replacement is cloned, and restore it if cloning fails or is canceled." — `adopt.go:18-19`).
- **upstream.documents** (unexported) (`internal/upstream/http.go:83`) — `Document`.
  - "fetches a livecheck document from the forge ... so the user's credentials and the forge's rate-limit handling apply." — `http.go:78-80`
  - "served is false for a URL the forge does not serve, and the plain fetch is used instead." — `http.go:81-82`
  - Optional capability: asserted via `s.Catalogs[spec.Forge].(documents)` at `internal/upstream/http.go:117`.
- **upstream.Catalog** (`internal/upstream/discovery.go:25`) — `Repository`.
  - "binds an interpreted Portfile source to its remote repository." — `discovery.go:24`
- **upstream.VersionProbe** (`internal/upstream/bound.go:12`) — `Port`, `EvaluateVersion`.
  - "evaluates a source version against one captured Portfile." — `bound.go:11`
  - Optional capability: an anonymous inline interface `interface{ EvaluateVersions(...) }` is asserted on the bound probe at `internal/upstream/bound.go:31-33`.
- **upstream.versionSelector** (unexported) (`internal/upstream/latest.go:20`) — `SelectVersion`, `ExtractVersions`.
  - "orders and captures versions the way MacPorts does: vercmp for order and its native regex for livecheck captures." — `latest.go:18-19`

## Doc-comment rules, by package

### record
- "state implementations own persistence and integrity checks." — `internal/record/doc.go:11`
- "callers preserve accepted inputs and copy mutable data when sharing ownership." — `doc.go:12-13`
- "Disposition ... is independent of the outcomes of individual jobs." — `change.go:9`
- "Change ... identity survives branch renames and commit rewrites." — `change.go:24`
- "Both are deleted only while they still hold Published, so nothing unpublished is lost." — `change.go:93-94`
- "each side settles on its own, so a process exit or a failed remote call leaves an obligation the next cycle ... takes up rather than a branch nothing revisits." — `change.go:94-96`
- "A resumed attempt uses these values rather than current application defaults." — `attempt.go:26`
- "These details do not participate in capability matching." — `attempt.go:81`
- "Planned dependencies on future outputs must be resolved to Artifacts before the specification is submitted to a provider." — `attempt.go:107-108`
- "Evidence carries the verdict separately from this lifecycle state." — `attempt.go:136`
- "Its specification remains fixed while admission, observations, and cancellation progress." — `attempt.go:170-171`
- "Owned resources have separate records and can outlive the attempt." — `attempt.go:171`
- "A lifecycle state alone never establishes that verification passed." — `attempt.go:197`
- "FailureKind ... independently of whether the proposed source change caused that failure." — `attempt.go:217-218`
- "Identifying the failing package alone does not establish attribution." — `attempt.go:233`
- "Running observations have an unknown verdict; a terminal outcome must be explicit." — `attempt.go:278-279`
- "Referenced artifacts and logs may be stored outside the state store." — `attempt.go:279`
- "ProviderExecution ... independently of the workflow's adoption of those effects into attempts and resources." — `provider.go:25-26`
- "Defining an action here does not imply that its intake or execution is implemented." — `job.go:8-9`
- "Whether the invoking CLI waits does not change this destination." — `job.go:30`
- "Skipping verification must be explicit." — `job.go:43`
- "JobSpec records immutable accepted intent, independently of CLI attachment." — `job.go:53`
- "A terminal state does not imply that resource cleanup has finished." — `job.go:120`
- "Resources associated with a terminal job may still require cleanup." — `job.go:141`
- "Terminal jobs retain the phase in which they settled." — `job.go:152`
- "The driver owns its progress after intake; follow-up requests receive their own jobs rather than reopening this one." — `job.go:195-196`
- "Claim ... Recording a result requires the same owner and generation and an unexpired lease." — `job.go:237-238`
- "Lease expiry does not stop an external operation that has already begun." — `job.go:238-239`
- "no applied time, which only the driver records." — `job.go:280-281`
- "CoverageIntent says which members of a shared release a job's verification must build." — `release_scope.go:58-59`
- "An initiating target that builds nothing in this scope is an error, never a silent widening to every member." — `release_scope.go:74-75`
- "SameMembership prevents corrections and branch adoption from dropping targets or changing the protected source identities accepted by the original bump." — `release_scope.go:117-118`
- "This lifecycle continues independently of job completion." — `resource.go:6`
- "The provider must preserve this identity for recovery and must not recycle it for another attempt's resource." — `resource.go:23-24`
- "A terminal job does not establish that its resources have been released." — `resource.go:31-32`
- "Targets may refer to future outputs; an Attempt's BuildSpec holds only resolved inputs." — `verification.go:20-21`
- "Planning coverage does not establish provider admission." — `verification.go:21`
- "Confirming the head alone does not establish that the title and body match." — `publication.go:110`
- "Drivers take a lease inside a write transaction, perform the external call outside it, and release the lease when they record the result." — `lease.go:6-9`

### state
- "Views provide a consistent read-only snapshot." — `internal/state/doc.go:4`
- "Updates commit related record changes together, including claims that authorize later external work." — `doc.go:4-6`
- "Transaction callbacks execute once, use their supplied context, return write errors, and finish before the transaction deadline." — `doc.go:9-11`
- "They must not perform external work, nest store calls, or retain a reader or transaction afterward." — `doc.go:11-12`
- "A reader or transaction is used serially within its callback; store operations may run concurrently." — `doc.go:12-13`
- "Callers decide eligibility and policy." — `doc.go:15`
- "Stores preserve repository scope, relationships, immutable identities, and monotonic lifecycle checkpoints." — `doc.go:15-16`
- "workflow owns evidence reuse and publication authorization." — `doc.go:16-17`
- "ProviderStore ... is implemented by the same backend as Store, not an independent lock service." — `provider.go:10-11`
- "ImageDigest is a disposable observation of an image's content at one file stamp." — `provider.go:29`
- "Maintenance operates on the whole database, independently of repository or provider setup." — `maintenance.go:8`
- "Backup identifies a complete standalone snapshot. It excludes external files and services." — `maintenance.go:14`
- "MigrationRequiredError identifies a supported schema that needs a writable upgrade." — `store.go:23`
- "the store still refuses an empty registration at each call." — `store.go:46-47`
- "Ordering by time and paging by ID cannot be combined, since a due-ordered page can skip records." — `store.go:77-79`
- "Tree omission is for a recent-result diagnostic, not proof of applicability." — `store.go:103`
- "Limit must be between 1 and 32." — `store.go:104`
- "Results contain original terminal attempts with evidence, newest first." — `store.go:104`

### workflow
- "Binding freezes source, targets, verification requirements, and publication inputs before acceptance." — `internal/workflow/doc.go:4-5`
- "The state store is the handoff between intake and execution." — `doc.go:16`
- "callers own process lifetime, repeated cycles, waiting, and presentation." — `doc.go:17-18`
- "External actions follow a claim, call, and record sequence." — `doc.go:22`
- "Short transactions establish intent and ownership; external calls run outside the write transaction; a later transaction checks the claim before adopting results." — `doc.go:22-24`
- "Provider idempotency and reconciliation remain necessary because rejecting a stale write cannot prevent a paused driver from making a late external call." — `doc.go:24-26`
- "Cleanup has separate claims and remains eligible after a job finishes." — `doc.go:26-27`
- "Acceptance, provider admission, and successful completion are distinct milestones." — `doc.go:31-33`
- "Milestone describes attachment, independently of the accepted destination." — `progress.go:6`
- "Those forms are mutually exclusive [All vs explicit job IDs]." — `scope.go:15`
- "All is limited to the engine's registered repository." — `scope.go:16`
- "per-item reports, including lost claims, rather than errors that abort the pass." — `cycle.go:18`
- "It spans several transactions and reads; use Engine.Status for a consistent view of durable progress." — `cycle.go:25-26`
- "If Engine.Cycle returns an error, the result can describe only part of the pass." — `cycle.go:27`
- "Concurrent callers must leave its fields unchanged and provide dependencies and a clock that support concurrent calls." — `engine.go:50-51`
- "Submit, Control, and Status do not call it [Provider]." — `engine.go:69`
- "Sharing an owner identity does not permit replacing a live claim." — `engine.go:85-86`
- "Job and control requests share the workflow request-ID namespace." — `engine.go:29`
- "Preserve the original request for retries, including after an uncertain commit." — `submit.go:112`
- "A stored record.JobSpec may contain source fields filled by intake and is not a substitute for the original request when retrying." — `submit.go:113-114`
- "Receipt confirms durable acceptance of one job. It does not establish driver pickup, provider admission, or completion." — `submit.go:120-122`
- "Equivalent retries return the same job identity and acceptance time." — `submit.go:122-123`
- "the branch's base must be on [Upstream], and an empty Upstream skips that check." — `adopt.go:27-28`
- "AdoptResult is the contribution adoption recorded, or with DryRun would record." — `adopt.go:47`
- "KeepBody records that the pull request's body is its author's: no publication rewrites or appends to it." — `adopt.go:33-34`
- "Timeouts bounds one external call, not the lifetime of a verification build." — `timing.go:9`
- "StatusFilter narrows a recorded snapshot without selecting work for execution." — `status.go:21`
- "ContributionSelector locates existing work without choosing a new source." — `contribution_select.go:16`
- "Refresh reports remote facts separately from the local decision to retire a contribution." — `contribution_lifecycle.go:20`
- "Resolution is what one selection means for one action: the one value ... computed once by the engine." — `resolution.go:46-48`
- "It decides nothing about master or a prior job, and its Source is the revision's [Tracked kind]." — `resolution.go:40-41`

### workflow/policy
- "answers read-only questions about recorded workflow state." — `internal/workflow/policy/doc.go:1-2`
- "It reads through state.Reader and never claims, writes, or calls a provider, so both intake and drivers can ask the same questions." — `doc.go:4-6`
- "Both combined and standalone publication must prove the entire cohort, including after process restart." — `coverage.go:15-16`
- "publication must cover the tracked contribution target." — `publication.go:33`
- "an unverified publication requires a job that explicitly skipped verification." — `publication.go:37`
- "a verified publication requires a job that requested verification." — `publication.go:42`
- "ValidatePublicationAction checks that a recorded publication action still matches the job's accepted intent." — `publication.go:90-91`
- "SelectVerification finds applicable passing evidence for one build question, or explains why a new build is needed." — `reuse.go:15-16`

### workflow/retention
- "It reads and writes the store and the repository and calls providers through what the engine gives it." — `internal/workflow/retention/doc.go:4-6`
- "it knows neither the engine nor the cycle: releasing a resource goes through the engine's claimed release path, handed in as a function." — `doc.go:6-8`
- "The branch-deletion decision lives here once." — `doc.go:8-9`
- "Item is one action the collection took or previewed." — `collector.go:19`
- "Result can describe partial progress when collection returns an error." — `collector.go:31`
- "It never advances jobs or forgets their identities." — `collector.go:54-55`
- "Explicit collection also releases retained failed environments." — `collector.go:55-56`

### workflow/preparation
- "Service materializes disposable snapshots, resolves and rechecks upstream releases, invokes macports/portedit, and stores the edited tree as Git objects." — `internal/workflow/preparation/doc.go:3-4`
- "It re-evaluates that stored candidate before returning edits and commit intent." — `doc.go:5`
- "Workflow owns durable preparation checkpoints, commit and branch integration, and subsequent verification or publication." — `doc.go:6-7`
- "Prepared is the evaluated snapshot of the prepared tree, bound to its committed source identity once the candidate tree is written." — `preparation.go:35-36`
- "Workspaces hands out one projection per source; nil opens one per preparation." — `preparation.go:60-61`

### workflow/choice
- "It is the one place the fallback, the test policy defaults, the per-target image binding, and the choice between failing and recording a requirement are written." — `internal/workflow/choice/choice.go:4-6`
- "app wires the two providers in and the engine's bindings call what it returns." — `choice.go:6-7`
- "It knows the providers' configuration types and their sentinel errors, which is why it is a leaf beside the engine rather than part of it." — `choice.go:7-9`
- "Local ... fails with verify.ErrImageUnavailable when no image serves it and ... verify.ErrExecutableUnavailable when Tart itself is absent." — `choice.go:27-29`
- "Preserve records a local provider's failure as an evidence requirement ... a preparation preserves, a verification fails." — `choice.go:60-63`

### verify
- "Providers implement idempotent submission, recovery, observation, cancellation, and resource release; optional capabilities supply logs and artifact cleanup." — `internal/verify/doc.go:7-8`
- "Workflow selects stored attempts, owns claims and scheduling, and records the results." — `doc.go:10-11`
- "Empty Platforms defers platform validation to Submit." — `provider.go:18`
- "Dockhand ... The provider reports it rather than these contracts reading it, which keeps this package to records and Git as its dependency test requires." — `provider.go:49-51`
- "CancelRequested forbids starting new external work; an existing run may still be returned for cancellation or observation." — `provider.go:74-76`
- "A closed request with an Unsupported submission records a definitive rejection, rather than permission to retry admission with a new request identity." — `provider.go:80-82`
- "Problems are discovery gaps that may conceal additional targets." — `coverage.go:6`
- "IndexedDependencies are estimates from default-variant metadata, not a guest's resolved dependencies." — `coverage.go:15-16`
- "HostMacPortsVersion ... selects nothing; the provider warns when the image's observed Base differs." — `build_options.go:19-20`
- "Commit, base, and revision IDs describe provenance; the full tree describes bytes." — `reuse.go:15-16`
- "ErrCacheBusy ... is distinct from a cache that was never written, which prunes nothing and reports no error." — `artifacts.go:21-22`

### verify/tart
- "Provider binds image identity and capabilities to accepted build inputs, prepares source before reserving shared capacity, and records recoverable execution identities." — `internal/verify/tart/doc.go:3-4`
- "It stages and observes guest builds, preserves diagnostic logs, and handles cancellation, release, and artifact retention." — `doc.go:5-6`
- "provider state coordinates concrete executions across processes and repository registrations." — `doc.go:10-11`
- "TestTimeout bounds the port's test phase in the guest; zero means DefaultTestTimeout. A timed-out test counts as a failed test." — `config.go:206-207`
- "IndexCache ... is disposable and therefore not part of the frozen configuration." — `provider.go:20-22`
- "Workspaces ... nil materializes one for staging alone." — `provider.go:27-28`
- "The guest owns its staged source after admission; retries never restage it." — `retention.go:72-73`
- "Preserve valid historical fingerprints so running jobs can still collect their results. New observations exclude diagnostic agent version strings." — `capabilities.go:35-36`

### verify/github
- "Provider validates the supported workflow and source, conditionally pushes the branch, and tracks matching runs and matrix results through durable execution records." — `internal/verify/github/doc.go:4-6`
- "Cancellation ends a request's tracking without canceling a shared Actions run." — `doc.go:6-7`
- "Config freezes the remote destination; command defaults cannot redirect recovery." — `config.go:19`
- "The workflow controls mutable hosted runners. This identifies the recipe, not a VM image." — `config.go:42`
- "Identity is checked before the lock: a run that is not the recorded one is refused whether or not its cache is busy or already gone." — `retention.go:36-38`
- "The request lock exists once a log was downloaded; without it there is no cache to prune." — `retention.go:41-42`

### verify/ledger
- "one row per request, reserved, admitted, closed, or released, scoped to one repository and one pool, and edited under a per-request file lock so that drivers in different processes take turns." — `internal/verify/ledger/ledger.go:2-5`
- "The rule it exists to keep is that a request the ledger has closed refuses every later submit." — `ledger.go:5-6`
- "what a provider answers when it refuses is the provider's own, as is everything in the row's payload and result." — `ledger.go:6-8`
- "Read is the request's row ... It takes no lock: the row is read as it stands." — `ledger.go:31-32`
- "Entry is one request's row, held under its lock until Close." — `ledger.go:71`
- "CloseUnknown records the closed row for a request the ledger has never seen, so that no later submit can start it." — `ledger.go:99-100`

### verify/staging
- "Archive materializes an immutable Git tree, stages its PortIndex, checks target coverage, and atomically installs an archive with caller-supplied payload files." — `internal/verify/staging/doc.go:3-4`
- "It owns temporary source lifetime and packaging. Providers own capacity, environment creation, guest execution, and the payload protocol." — `doc.go:5-6`
- "Payload contains provider-owned top-level files outside the reserved ports tree." — `archive.go:32`

### publish
- "Service resolves destinations, checks contribution scope and supplied verification evidence, prepares pull-request content, and observes and writes pull requests." — `internal/publish/doc.go:4-6`
- "Observations are checked against the accepted source and remote identity." — `doc.go:6-7`
- "Workflow coordinates guarded Git pushes, complete dependent coverage, claims, and durable checkpoints around these external effects; forge adapters implement the remote protocol." — `doc.go:7-9`
- "A remote whose URL names it is the upstream whatever it is called, and never a push destination." — `publish.go:21-22`
- "An empty Remote is resolved automatically: the remote pushing to the fork the authenticated user owns, or the only remote that is not the upstream." — `publish.go:26-28`

### forge
- "Its contracts describe tags, releases, repository identity, pull requests, and remote failures." — `internal/forge/doc.go:4-5`
- "Adapters translate service responses into these values; upstream and publish apply port-selection and publication policy." — `doc.go:5-6`
- "Catalog methods return complete observations or an error, never partial success." — `repository.go:32`
- "Tag resolves an exact name to a commit; an absent ref reports ErrNotFound." — `repository.go:33`
- "An absent file reports ErrNotFound; the read is bounded by limit bytes." — `repository.go:46`
- "A forge that cannot report these is simply not an inspector." — `pullrequest.go:17-18`
- "Authenticate checks the credentials and nothing else." — `pullrequest.go:23-24`
- "Publication policy decides whether to request that write." — `pullrequest.go:43`

### upstream
- "MacPorts source conventions and version selection determine which candidates apply." — `internal/upstream/doc.go:3-4`
- "Results retain unknown and incomplete observations, and source checks detect a selected tag moving to a different commit." — `doc.go:5-7`
- "Callers own workspaces, preparation, and workflow acceptance." — `doc.go:8`
- "served is false for a URL the forge does not serve, and the plain fetch is used instead." — `http.go:81-82`
- "A bounded observation must fail as incomplete, never yield a truncated candidate set." — `http.go:136-137`
- "Discovery ... owns neither a Git checkout nor a workflow job." — `bound.go:18`
- "MacPorts still evaluates each candidate; batching only shares the interpreter." — `latest.go:153`

### macports
- "Tree and Context tie a materialized snapshot to its source identity, target, variants, and platform." — `internal/macports/doc.go:3-4`
- "Reader is the evaluation contract used by preparation, discovery, and verification." — `doc.go:4-5`
- "A plain materialization holds everything and needs nothing." — `context.go:17-18`
- "Projected ... which the repository validated when it opened, as opposed to a plain directory." — `context.go:86`
- "Base is the root of the projection this tree overlays, or the root itself." — `context.go:91-92`
- "ObservationRequest ... never changes the native platform used by Reader.Evaluate or establishes build evidence." — `observation.go:9-10`
- "SelectedOnly omits sibling metadata; it is not suitable for final fidelity." — `observation.go:14-15`
- "Consumers must resolve these frames to unique syntax before editing [Declaration]." — `observation.go:21`
- "Distfile ... does not mean an archive was fetched." — `observation.go:33`
- "Whole-Portfile validation continues to use Reader.Evaluate." — `reader.go:36`
- "a reader that cannot observe cannot edit; the one implementation is eval.Evaluator." — `reader.go:56-57`
- "Root is not persisted; a snapshot read back has none, and normalization then leaves values as they are." — `metadata.go:47-49`

### macports/portedit
- "Callers supply an exclusively owned workspace and retain it for the lifetime of any VersionProbe, which must be used sequentially." — `internal/macports/portedit/doc.go:13-14`
- "Git object storage, branch integration, and workflow state belong to the caller." — `doc.go:14-15`
- "It checks the edited evaluation against the intended change and returns edits, commit intent, and fidelity diagnostics." — `doc.go:9-10`
- "Assessment shares the pre-download checks and reports untested stages explicitly." — `doc.go:10-11`
- "Workspace is the projection of the source the edit reads and never writes; every candidate is an overlay of it." — `prepare.go:41-42`
- "It is the caller's to open and close, and it outlives every probe made from it." — `prepare.go:43-44`
- "Neither kind records a build; verification providers establish build results [ContextCoverage]." — `prepare.go:64-65`
- "Prepared ... the state every later step reads ... set with each fidelity report, so it is the last report's snapshot without any reader having to know that." — `prepare.go:77-81`
- "a rejected patch is a finding, not a refusal." — `prepare.go:85-86`
- "An absent file reports macports.ErrManifestMissing [ManifestSource]." — `prepare.go:111`
- "Assessment describes preparation evidence, not whether a port will build." — `assessment.go:14`
- "Its presence alone does not establish that any particular release is editable [VersionInput]." — `assessment.go:27`
- "Probing evaluates candidates as overlays of the workspace and never writes into it; it does not fetch archives." — `probe.go:16-18`
- "Callers must keep the workspace alive and use the probe sequentially [VersionProbe]." — `probe.go:27`
- "full edit validation uses evaluateEdit so unrelated subport changes remain visible." — `source.go:374-375`

### macports/workspace
- "The tree is the truth; the directory is a lazily materialized, scoped projection of it." — `internal/macports/workspace/doc.go:1-3`
- "An edit is never written into a workspace; it is an Overlay, a sibling projection sharing the base's tracked files by hardlink with the edited ones replaced." — `doc.go:4-6`
- "A workspace owns the interpreter session bound to its root, and the session serves its overlays." — `doc.go:7-8`
- "_resources is present after any Ensure [Scope]." — `workspace.go:24`
- "A base owns its directory, its session, and its overlays; an overlay shares the base's tracked files and replaces the edited ones." — `workspace.go:35-36`
- "adopted marks a workspace over a directory someone else owns and filled ... Close leaves the directory." — `workspace.go:53-58`
- "Open claims a directory for the tree, creates it, and materializes nothing into it." — `workspace.go:61-62`
- "Registry hands out one workspace per source within a process and closes it when the last holder releases it ... overlays are not registered." — `registry.go:12-15`
- "A nil Registry shares nothing: Acquire opens a workspace of its own and release closes it." — `registry.go:15-16`

### macports/portindex
- "Stage reconciles mirror or cached indexes with the selected source and installs a full and quick index into a materialized tree." — `internal/macports/portindex/doc.go:4-5`
- "staging and collection share a profile lock." — `doc.go:6`
- "callers evaluate concrete targets and decide verification policy." — `doc.go:8`
- "Consumers ... ask a Source and never stage for themselves." — `source.go:15-16`
- "installs a tree's index into the tree's root once ... opens it from there after, and remembers what it staged, so a command that resolves names repeatedly against one tree indexes it once." — `source.go:22-24`
- "A tree that names no platform is indexed for the native one." — `source.go:25`
- "WithoutBase builds a tree's index from the nearest cached generation rather than from the tree's recorded base." — `source.go:27-28`
- "Index reads names and metadata from one generated MacPorts PortIndex." — `reader.go:29`

### macports/eval
- "It implements macports.Reader; source contexts and observations remain independent of this implementation." — `internal/macports/eval/doc.go:4-5`
- "Native runtime facts remain separate from modeled platforms." — `doc.go:7`
- "Evaluation observes Portfiles and does not decide which declarations to edit." — `doc.go:8`
- "Model ... zero selects DefaultModel. A Mac always describes itself." — `evaluator.go:27-29`
- "Session ... Each evaluation still opens the port afresh, so rewritten contents are observed." — `evaluator.go:152-153`

### macports/fetchguard
- "reads a port's pre-fetch hooks and decides whether the fetch is standard, guarded by a rejection that changes nothing fetched, or custom." — `internal/macports/fetchguard/grammar.go:1-3`
- "It is static analysis over the hook text and the definitions the observation worker ships, with no interpreter of its own." — `grammar.go:3-4`
- "Origin ... Unknown when Label is empty." — `grammar.go:19`
- "post-fetch hooks can modify archive preparation." — `grammar.go:29` (rejection reason encoded in `Assess`)

### macports/selection
- "resolves port names in a captured source before native evaluation." — `internal/macports/selection/doc.go:1`
- "Index must stage or open an index belonging to the supplied tree, never the checkout [Reader]." — `selection.go:17`

### macports/dependents
- "Service stages a matching PortIndex, selects direct build, library, and runtime dependents, and evaluates their targets." — `internal/macports/dependents/doc.go:4-5`
- "It projects verify.Coverage with tool requirements while keeping native evaluator and index types local." — `doc.go:5-6`
- "Dependency closures are default-variant estimates; callers decide revision edits, verification plans, and scheduling." — `doc.go:7-9`
- "stages an index and evaluates candidates against the same immutable source, outside workflow transactions." — `discover.go:31`
- "Dependent variants use MacPorts defaults; explicit root variants remain confined to their corresponding root questions." — `discover.go:32-33`
- "Workspaces shares the prepared tree with Tart staging; nil materializes one for discovery alone." — `discover.go:26-27`

### git
- "Repository supports immutable objects, source capture and materialization, checked edits, refs, and guarded local and remote branch updates." — `internal/git/doc.go:3-4`
- "Explicit preconditions and operation locks protect mutations shared by drivers." — `doc.go:5`
- "Callers own the lifetime of materialized snapshots and the workflow meaning of branches and commits; this package supplies Git facts and mechanisms." — `doc.go:6-7`
- "Materialize reads raw blobs, avoiding checkout filters and archive attributes. The returned directory is owned by the caller until Close." — `snapshot.go:52-53`
- "Branch resolves a literal local branch, without revision-expression fallback." — `snapshot.go:22`
- "Only a private index is written. Two matching reads detect ordinary concurrent edits; this is not a filesystem-wide atomic snapshot." — `worktree.go:31-32`

### git/changeset
- "It composes checkout or branch capture, explicit-base deltas, single-commit inspection, and correction candidates using git.Repository." — `internal/git/changeset/doc.go:4-5`
- "changeset leaves branch adoption and durable bookkeeping to them." — `doc.go:6-7`
- "a change under _resources alone names no port and is refused, as is a second port directory or a path outside any port directory." — `scope.go:38-40`
- "Files under _resources may change alongside it, since a port's update sometimes needs the group it loads to change too." — `scope.go:36-38`

### app
- "Build opens repository-scoped state and constructs services whose lifetime ends with Services.Close." — `internal/app/doc.go:4-5`
- "Provider selection and request binding happen here; accepted-job progression and bookkeeping belong to workflow, and presentation belongs to cli." — `doc.go:7-8`
- "the state database is opened for writing, created when absent, and the checkout is registered in it." — `app.go:69-70`
- "BuildForReading assembles the same services for a command that records nothing, a dry run: the state database is opened for reading when it [exists]." — `app.go:89-90`

### cli
- "It parses flags and selectors, invokes application services, and renders human or JSON results, progress, and logs." — `internal/cli/doc.go:3-4`
- "Commands select whether to attach through admission or completion; proc drives the cycles and workflow owns durable transitions." — `doc.go:4-6`
- "Run returns errors for the entry point to map through ExitCode." — `doc.go:6`
- "The defaults are the foreground and the PR: a command stays through verification and publication unless told to stop earlier or to detach." — `cli.go:24-26`
- "Envelope is the shape of every JSON result: the verb, the exit code the process will return, the one-line error if any, and the command's result." — `cli.go:60-61`

### proc
- "Manager runs a persistent loop or attaches to explicit jobs until an admission or completion milestone." — `internal/proc/doc.go:3-4`
- "It owns loop pacing, context cancellation, and observer callbacks." — `doc.go:4-5`
- "Workflow and state coordinate claims across processes; stopping an attachment leaves accepted work recorded and does not request its cancellation." — `doc.go:5-7`
- "Attachment is for seeing one job through; a resident driver runs under the plain context and is the audience for the driver's own reports." — `manager.go:20-22`
- "Claims in state allow multiple callers; there is no resident singleton lock." — `manager.go:76`
- "Stopping attachment does not request cancellation of accepted work." — `manager.go:85`

### filelock
- "Acquire initializes the lock path and waits for shared or exclusive ownership." — `internal/filelock/doc.go:4`
- "TryExisting attempts an existing lock without waiting or creating paths, so maintenance can skip active work." — `doc.go:5-6`
- "Callers close the returned file to release ownership and preserve the lock path so cooperating processes lock the same file." — `doc.go:6-8`
- "Shared permits other shared holders." — `filelock.go:19`
- "Exclusive excludes every other holder." — `filelock.go:20`
- "ErrBusy means another process currently owns an incompatible lock." — `filelock.go:59`

### scratch
- "one run root under the system temporary directory that every short-lived directory dockhand makes is created inside." — `internal/scratch/scratch.go:1-3`
- "The root is removed when the process ends normally, and it is held under an advisory lock while the process lives." — `scratch.go:4-6`
- "a root whose lock can be taken belongs to a process that died without cleaning up and is swept by the next process that opens a root, and by gc." — `scratch.go:6-8`
- "Nothing meant to outlive a command lives here: the state database, verification artifacts and logs, the Tart pool, and the index cache have their own configured homes." — `scratch.go:8-10`
- "The caller removes it when done; the root's removal at exit, or a later sweep, catches what it did not [Dir]." — `scratch.go:40-41`
- "A root that was removed from under the process ... is replaced by a new one under the temporary directory of the moment [Root]." — `scratch.go:52-54`
- "The process's own root is never listed [Stale]." — `scratch.go:115-116`
- "A root another sweeper is removing at the same time is left to it [Sweep]." — `scratch.go:128`

## Prose contracts, by document

### architecture.md

- architecture.md:9 — A later configuration change "must not silently change an accepted job's source, build question, or requested destination."
- architecture.md:9 — "Credentials remain outside durable job records."
- architecture.md:17 — Writable opening initializes the schema and registers the repository; read-only status "does none of those things."
- architecture.md:17 — "Help, completion generation, and previews do not open the database."
- architecture.md:21 — Linked worktrees share an entry; "separate clones remain distinct even with the same remote."
- architecture.md:21 — "A moved repository requires explicit reassociation rather than automatic matching by URL."
- architecture.md:23 — "Every workflow read and transaction has an explicit repository scope"; repository-qualified relationships prevent cross-repo joins.
- architecture.md:27 — Git remains the source repository, "with no authoritative state ref, derived ledger, source pins, or Git operation journal."
- architecture.md:29 — The store "never exposes a whole-database map for mutation."
- architecture.md:31 — Transaction callbacks run once, synchronously, and "must not call provider, forge, Git, or other external operations."
- architecture.md:31 — "Nested store calls and automatic callback replay are unsupported."
- architecture.md:35 — "Only confirmed commit returns an acceptance receipt"; reading status "neither claims work nor refreshes external observations."
- architecture.md:39 — Before consuming source, an operation checks the objects it needs; "never retarget accepted work to the current branch head."
- architecture.md:41 — "SQLite and Git do not form one atomic transaction."
- architecture.md:47 — A tracked change has "a stable identity independent of a branch name, commit SHA, job, or PR number."
- architecture.md:51 — A follow-up request "does not reopen a completed job or silently retarget an active build to a moving branch."
- architecture.md:53 — A publication job "finishes when its requested revision and metadata have been confirmed on the forge. It does not wait for merge."
- architecture.md:55 — "A later branch edit does not inherit authorization to publish from a completed job."
- architecture.md:63 — "No provider reads a mutable checkout during a queued or running build."
- architecture.md:65 — "Standalone verification must retain its source and targets without claiming the branch as an exclusive contribution."
- architecture.md:67 — Publication "does not inherit a pass from matching only the port's version or some edited files."
- architecture.md:87 — "Failure in one job or an error polling one provider must not abort unrelated work for the entire pass."
- architecture.md:91 — "There is no separate driver executable, executable-path configuration or lookup, or automatic background launch."
- architecture.md:97 — "Starting a driver grants no additional publication authority."
- architecture.md:111 — A refusal or failed verification "must not be displayed as successful admission or completion."
- architecture.md:113 — "The CLI must not independently poll the provider to decide a verdict, settle a build, or release an environment."
- architecture.md:117 — "There is no Unix socket or separate local request transport."
- architecture.md:125 — A request's caller-owned ID "must be retained across retries"; target order and nil/empty variant maps "do not change request identity."
- architecture.md:129 — "Action, destination, and verification policy must be explicit."
- architecture.md:131 — "Reusing the ID with different intent or repository is an error"; state writes "neither scan nor pin recorded source objects."
- architecture.md:139 — A cycle "does not sleep or wait for a build."
- architecture.md:141 — Cycles "select at most 64 controls, jobs, and cleanup actions each."
- architecture.md:143 — "Candidate selection grants no ownership"; an idle cycle "makes no provider calls."
- architecture.md:147 — "A cycle never fills missing build inputs from current defaults. Admission creates neither a new source revision nor publication authority."
- architecture.md:149 — "Only provider Submit decides admission."
- architecture.md:153 — "Failed builds are not automatically retried"; "there is no account-wide rate-limit coordinator or blanket elapsed-time failure cutoff."
- architecture.md:161 — "Do not use an independent lock backend to authorize state changes: the ownership check and mutation must share one transaction boundary."
- architecture.md:165 — "Claim checks alone cannot undo an external action."
- architecture.md:169 — Submit "must be durable and idempotent by submission ID" and "reject reuse of an ID with different build inputs."
- architecture.md:171 — The provider "must not close an admitted run"; if it cannot guarantee closure, "the driver does not resubmit."
- architecture.md:173 — "Submission IDs remain stable through capacity waiting and uncertain outcomes; they change only after confirmed closure."
- architecture.md:181 — "A dead driver does not prove that its build stopped. An unreachable builder does not prove that it is safe to destroy."
- architecture.md:187 — Provider release "is never invoked for an active or unresolved attempt, a foreign handle, or an orphan resource without an owning attempt."
- architecture.md:227 — "Missing objects are errors; the current checkout is never substituted."
- architecture.md:229 — "MacPorts itself evaluates Tcl; Dockhand does not infer version values from source syntax."
- architecture.md:291 — Integration "refuses to recreate" an absent branch: "absence cannot distinguish a never-created branch from one the user deleted."
- architecture.md:306 — "MacPorts exposes credential presence rather than credential values"; archive bytes "are not stored in SQLite or retained in a cache."
- architecture.md:323 — "Only a pass is reusable; a miss cannot authorize a new execution."
- architecture.md:334 — "Authentication remains outside repository and workflow state."
- architecture.md:336 — "Only the resolved token enters the authenticated client in memory. Accepted records retain no credential."
- architecture.md:348 — Acceptance creates the contribution, revision, request, job, and publication intent atomically; "failure leaves no partial adoption."
- architecture.md:350 — "An active-action uniqueness constraint reserves each forge/head-repository/head-branch across all repository entries in the DB."
- architecture.md:358 — "Existing PR bodies are preserved... neither can be regenerated."
- architecture.md:362 — `--unverified` is "the one phase transition the store allows to skip a phase."
- architecture.md:366 — "Failed verification never enters publication."

### cli-design.md

- cli-design.md:9 — `sync` deletes local and fork branches "only while it still holds the published commit" and the branch is not checked out.
- cli-design.md:9 — PR review/check status "is shown by sync and status and never acted on: no rerun, comment, push, or edit follows from it."
- cli-design.md:17 — Explicit flags override the environment; "an explicitly empty path is rejected."
- cli-design.md:21 — "Parsing and help do not check that the selected paths exist or create them."
- cli-design.md:28 — Old `--lock-dir`, `-L`, and `--lockfile` flags are rejected, "no compatibility alias."
- cli-design.md:35 — Status uses read-only access: absent db/unregistered repo yields empty results, "unless a specific job ID was requested, which returns not-found."
- cli-design.md:37 — `status` and `serve` cover the selected repository, "with no implicit all-database scope."
- cli-design.md:43 — Help and completion "do not open state or require a Git repository or provider, and create no directories or files."
- cli-design.md:49 — `auth login` does not require a ports checkout, open the workflow database, or create a workflow job.
- cli-design.md:53 — A custom API origin "requires an explicitly supplied credential source so credentials for github.com cannot be sent to another host."
- cli-design.md:55 — `auth status` names the rejection source; "Dockhand does not try another identity after rejection."
- cli-design.md:57 — `auth logout` "does not revoke the token on GitHub or modify environment variables or gh credentials."
- cli-design.md:60 — The driver "checks again immediately before a branch push and immediately before a PR create or update."
- cli-design.md:62 — "Credentials do not enter accepted job records, the database, logs, or JSON results."
- cli-design.md:77 — Backup requires no checkout/provider config; "an existing destination is refused"; missing database is an error.
- cli-design.md:79 — `db migrate` "needs no checkout or external tools, creates no job, and advances no workflow."
- cli-design.md:89 — `--check` "refuses a missing image and performs no pull or installation."
- cli-design.md:95 — Adoption takes a per-image write lock and verification takes the corresponding read lock while hashing/cloning.
- cli-design.md:117 — "Missing or unprepared contributions cannot fall back to the checkout."
- cli-design.md:117 — "A port with an open contribution cannot adopt a second branch."
- cli-design.md:117 — "A refused or dry-run adoption leaves no branch behind."
- cli-design.md:123 — "A matching negative result prevents fallback to an older pass"; an explicitly selected image that fails setup "is never replaced from history."
- cli-design.md:125 — `--fresh` requires new execution "even when a pass applies"; the choice is "recorded at acceptance and survives detachment."
- cli-design.md:125 — "Legacy results without a verifier identity are not reused."
- cli-design.md:127 — The `--from-source` choice "is recorded at acceptance, so a changed CLI default does not alter existing jobs."
- cli-design.md:129 — "No later job joins that fixed selection" after branch/job selection at command start.
- cli-design.md:131 — The JSON envelope is "written once after the command runs so the exit code is known; a refused command still writes the envelope with a null result."
- cli-design.md:147 — Every checkout-selecting command validates it "before touching state or fetching anything into it"; a wrong directory "is never registered in the state database."
- cli-design.md:149 — Assessment evaluates Tcl in an isolated materialization; "this is not a sandbox for untrusted Portfiles."
- cli-design.md:159 — "Successful assessment does not promise that a download, full preparation, or build will succeed."
- cli-design.md:172 — "The accepted source stays fixed across retries and later upstream movement."
- cli-design.md:172 — A shared revision edit "that changes unselected siblings, changes other evaluated metadata, or fails evaluation is refused."
- cli-design.md:174 — Preview "creates no branch, commit, job, database, or verification environment and leaves the user's checkout/index alone."
- cli-design.md:189 — "An unchanged version is refused."
- cli-design.md:191 — "Release metadata does not veto a tag in tag mode."
- cli-design.md:197 — An already-current job "closes the initial contribution intent without creating a branch or verification attempt; an unavailable image does not prevent this no-op."
- cli-design.md:201 — A checksum value with no single owner "has no owner and the port stays unsupported"; "never rewritten whole."
- cli-design.md:227 — "Every changed path must stay within that port directory or under _resources."
- cli-design.md:229 — If the recorded target changes without the revision changing, "acceptance still fails rather than silently adopting a different scope."
- cli-design.md:231 — "A renamed or missing tracked branch requires an actionable error rather than silent reassociation."
- cli-design.md:245 — "Empty selectors are rejected"; a missing/unprepared contribution "never falls back to verifying the checkout."
- cli-design.md:249 — "Once accepted, the Git tree is immutable; the driver never recaptures the checkout."
- cli-design.md:251 — "Subsequent edits, commits, or branch movement cannot change a queued or running build."
- cli-design.md:257 — "Standalone verification does not establish an exclusive contribution association for its branch."
- cli-design.md:370 — "CLI exit stops observation... `--detach` changes attachment, not the requested destination."
- cli-design.md:374 — Reject incompatible requests such as "`--trace --unverified` or `--trace --detach`... with a clear usage error."
- cli-design.md:378 — "Cancellation preserves the branch and completed evidence; it does not undo an already-published PR."
- cli-design.md:384 — "`--unverified` is the one way to publish without a build... Dockhand never infers that choice from unavailable tooling."
- cli-design.md:386 — "A negative build result is never a pass."
- cli-design.md:398 — "Status does not initialize or migrate the database, register repositories, or mutate workflow records."
- cli-design.md:400 — `--dry-run` "does not edit the working tree, create a branch, persist a job, start a build, or publish a PR."
- cli-design.md:436 — "Root commits, merges, multiple unpublished commits, empty changes, and changes outside one port directory are refused."
- cli-design.md:436 — "The push repository must be owned by the authenticated GitHub login" or it is refused before acceptance.
- cli-design.md:467 — GitHub's test policy is fixed; "`--tests` is a Tart option and is refused with `--provider github`."
- cli-design.md:472 — "A configured provider registry never falls back to a different provider."
- cli-design.md:476 — "Only absence of credentials permits anonymous reads; inaccessible, malformed or rejected credentials are reported" without silent fallback.
- cli-design.md:483 — "A full Tart queue waits rather than switching to GitHub."
- cli-design.md:491 — Holding one shared-release subport back "is not supported yet."

### components.md

- components.md:5 — Add files as behavior is implemented; "this tree is not a request to create empty packages or implement phase two immediately."
- components.md:81 — "A separate Go package is justified by a useful dependency boundary, not by every lifecycle noun or CLI verb."
- components.md:87 — `app` "injects a state.Store; it must not acquire a second workflow sequence as commands grow."
- components.md:89 — `cli` "submits requests through the shared workflow API rather than writing record shapes itself; it never settles an attempt or performs driver bookkeeping."
- components.md:89 — "Domain packages do not print terminal messages or decide exit codes."
- components.md:91 — "No separate executable, executable-path discovery, or automatic child driver launch is needed."
- components.md:95 — "There is no socket or separate request transport."
- components.md:97 — Successful durable submission "does not mean a driver has claimed the job or a provider has admitted a build."
- components.md:99 — Status observation "preserve[s] observation timestamps and do[es] not poll a provider or run bookkeeping to answer status."
- components.md:103 — Record names "do not imply a package or state machine for every struct."
- components.md:105 — "`record` must not become a miscellaneous collection of services, provider SDK types, terminal strings, or duplicate versions of existing records."
- components.md:113 — "Every view or transaction is bound to one repository"; "there is no whole-state serialization API or interface per table."
- components.md:113 — "The registry methods stay on Store, and only app calls them."
- components.md:115 — "Claims stay in the same transaction as the state they protect... An independently replaceable lock backend must not authorize workflow writes."
- components.md:117 — `git/changeset` "knows the tree's layout... and no port: it does not interpret changed paths as MacPorts targets or decide workflow policy."
- components.md:117 — "Git operations and database writes cannot commit atomically together."
- components.md:121 — "There is no separate `driver` package."
- components.md:125 — "Neither a synchronous command nor an adapter gets its own alternative progression loop."
- components.md:127 — "A handler advances [phase] only in the transaction that records the checkpoint completing its own phase."
- components.md:129 — "A cycle claims a bounded action in a short state transaction, performs the work after commit, and records its result only if the claim and relevant revision remain current."
- components.md:131 — "It does not reimplement version comparisons, Tcl semantics, failure classification, or publication eligibility."
- components.md:137 — "The evaluator reports observations and does not choose edits or decide which releases to preserve."
- components.md:141 — "Only recognized literal dependency declarations are incorporated, never a generated Portfile as executable Tcl."
- components.md:143 — Version classification "records... whether it takes the port out of stable, which reporting states and nothing refuses."
- components.md:145 — `macports/fidelity` "knows nothing about editing, downloads, or workspaces."
- components.md:149 — "Standalone verification must not reserve a branch for one port."
- components.md:151 — "`--dry-run` calls the same preparation capability and renders its proposed changes without creating a job or moving tracked refs."
- components.md:155 — "A preliminary capacity check is advisory: actual admission must coordinate at the provider's resource scope."
- components.md:161 — "Resource ownership is per concrete attempt, not just per change and platform."
- components.md:165 — `macos` "imports no Tart, verification, or state packages and never chooses a host implicitly."
- components.md:167 — "An uncached image is inspected inside the admitted disposable clone before source staging, so inspection uses ordinary capacity and never changes the source image."
- components.md:171 — "A matching SHA does not establish that metadata is current, and an existing PR does not prove an attempted edit succeeded."
- components.md:179 — "`record` has no dependency on CLI, proc, workflow, storage, or concrete integrations."
- components.md:181 — "`state` depends on shared records and standard-library contracts, not Git, SQLite, or workflow policy."
- components.md:182 — "`credential` defines authorization and storage contracts without depending on a concrete forge, Keychain, CLI, or workflow."
- components.md:187 — "`forge` defines remote facts and access contracts... It imports no capability or concrete adapter."
- components.md:188 — "Neither adapter imports `upstream`, `publish`, nor `macports`."
- components.md:196 — "`workflow` depends on `state` and capability APIs. Capabilities do not depend back on the engine or write its records."
- components.md:197 — `proc` "does not judge evidence or choose the next business action."
- components.md:199 — "A later package split needs a concrete independent consumer or dependency boundary, not a file-count threshold."
- components.md:214 — "The command must not promise automatic workflow continuation after its process exits."
- components.md:214 — "`status` reads a projection; `outdated` reads discovery results; neither takes ownership of a job by observing it."
- components.md:243 — Resolution "does not use an installed PortIndex to resolve names in a different source snapshot."
- components.md:265 — "`Stage` refuses to generate an index over a root whose scope is not All."
- components.md:271 — "`git.EditTree` retains file preconditions and never changes the user's index or refs."
- components.md:304 — Standalone/detached verification "create no contribution."
- components.md:306 — Tart "always materializes the accepted tree."
- components.md:311 — "A newer matching negative result prevents reuse of an older pass."
- components.md:318 — "The repository always supplies exact-tag and tag-catalog access... This prevents tag and release readers for one selection from addressing different repositories."
- components.md:324 — Candidate match text "is never used to download source. Source downloads continue to follow evaluated master_sites and distfiles."
- components.md:328 — "A catalog failure cannot become evidence that a requested tag is absent."
- components.md:334 — "No state transaction spans Git or HTTP."
- components.md:334 — "An ambiguous response never authorizes repeating a create or update."
- components.md:349 — `record.PublicationSpec` "populated only once the prepared revision passes verification."
- components.md:361 — "Canceling tracking does not cancel a shared Actions run."

### state.md

- state.md:12 — "Do not create an interface per table or a generic key/value API. Introduce methods for the records and queries actually consumed by the workflow."
- state.md:34 — "`state` imports neither Git nor the concrete backend."
- state.md:72 — "`View` exposes one consistent, read-only snapshot. It does not copy the database into memory."
- state.md:72 — Callbacks "perform no provider, forge, Git, or other external work"; "a view or transaction must not escape its callback."
- state.md:72 — "Nested store calls inside a callback are unsupported"; "never automatically replay a callback after a conflict or uncertain commit."
- state.md:74 — "A receipt is returned only after confirmed commit."
- state.md:74 — "Read-only lookup never registers a repository."
- state.md:78 — "The state implementation receives ordinary paths and IDs; it does not discover Git repositories itself."
- state.md:80 — "Linked worktrees share a repository entry. Separate clones have distinct entries even when their remotes match."
- state.md:82 — "A reader/transaction is bound to exactly one registered repository... an ID belonging to another repository is not accessible through that view."
- state.md:82 — "Reusing a request ID for another repository conflicts with the original receipt."
- state.md:84 — "A missing or renamed branch is an actionable error; no automatic branch guessing or evidence reassignment occurs."
- state.md:86 — "An all-jobs scope means all jobs in that repository, not all repositories in the file... an omitted repository must never silently mean the whole database."
- state.md:112 — "Do not store a second authoritative copy of a source or relational key inside JSON."
- state.md:114 — "Preserve the existing negative and running evidence semantics; terminal results cannot be overwritten by a later poll, and retrying a build creates another attempt."
- state.md:118 — "Only confirmed provider closure permits a replacement identity, created in the same transaction as recording closure."
- state.md:122 — "The write API may repeat a workflow precondition only when it defines a durable relationship... It does not compare build inputs to choose reusable evidence."
- state.md:130 — "Claim owner and deadline are either both present or both absent."
- state.md:130 — "A pass selects at most 64 controls, jobs, and cleanup actions each."
- state.md:132 — "Do not inject an independent lock backend to authorize a state write: checking ownership in one system and writing in another would introduce a gap."
- state.md:134 — "Lease expiry does not prove an external action ended or that provider capacity is free."
- state.md:134 — "Reservations have no lease expiry; only confirmed VM shutdown frees capacity."
- state.md:134 — "Different DB files do not coordinate claims for shared external resources."
- state.md:140 — "Recheck the version under the migration write transaction so concurrent openers cannot apply the same migration twice."
- state.md:142 — "The backend's read-only opening mode does not create the database, register a repository, or migrate the schema."
- state.md:144 — "A command that accepts or advances work opens writable state and initializes it lazily."
- state.md:150 — "State persistence makes no Git mutations and creates no source pins."
- state.md:150 — "Never replace a job's source with the current branch head to make it runnable."
- state.md:154 — "There is no `git_operations` table or promise of atomicity between SQLite and Git."
- state.md:156 — "Matching port name, version, and revision alone does not establish equivalent verification inputs."
- state.md:160 — "Neither operation [backup/check] repairs or resumes work."
- state.md:164 — "Database backups exclude external files and effects... database integrity does not prove that resuming it is safe."
- state.md:180 — "Terminal results are immutable, and closed/released identities cannot be revived."
- state.md:182 — "No database transaction spans cloning, booting, source transfer, guest work, stopping, or deletion."
- state.md:186 — "Settings are included in accepted request identity and cannot be silently replaced during retry."
- state.md:192 — "The checkpoint's branch and source are immutable once present; integration-started cannot be cleared."
- state.md:194 — "Foreign-key checks run before completing the transactional table rebuild."
- state.md:196 — "Read-only opening does not migrate older databases; a writable command performs the upgrade."
- state.md:201 — "The write API requires a matching bump request and complete source identity, then forbids replacement or removal once present."
- state.md:203 — "No database transaction spans tag lookup, source evaluation, or downloading."
- state.md:205 — "A NoUpdate checkpoint is valid only for an automatic bump completed without a prepared candidate or result revision."
- state.md:214 — "The write API checks that the reference exists in the job's repository and prevents it from being replaced or removed."
- state.md:216 — "Reused jobs never become candidates themselves."
- state.md:222 — "Confirmed and definitively rejected actions retain history without keeping that reservation."
- state.md:224 — "The store preserves the action unchanged afterward."
- state.md:233 — "No cache row claims that a VM is available, stopped, or safe to delete."
- state.md:249 — "The store refuses any other step, so phase movement is monotonic, and terminal jobs retain their last phase for diagnosis."
- state.md:255 — "A concurrently accepted job cannot be partially included or silently join an existing cancellation."
- state.md:261 — "Losing it causes another probe; it does not erase accepted requests or attempt evidence."
- state.md:265 — "The setup manifest is a declaration checked against the observed guest. It is not trusted in place of the probe."
- state.md:269 — `changes.generated_commit` "is immutable across amendments and branch renames."
- state.md:275 — "Missing and unrecognized databases are not initialized; newer schema versions require a newer Dockhand build."
- state.md:279 — "Each attempt still has at most one live submission. A remote run identity does not confer resource ownership or cancellation authority."
- state.md:285 — "No jobs are combined merely because their targets match."
- state.md:291 — "Only the pruning timestamp and pruning retry/error metadata may advance before pruning completes; a completed marker cannot be erased."

### workspace-design.md

- workspace-design.md:39 — "An edit is never written into a workspace; it is an overlay."
- workspace-design.md:61 — A workspace "never rewrites a tracked file it wrote; an edit is an Overlay."
- workspace-design.md:109 — Overlay's "git tree is not computed until Commit."
- workspace-design.md:139 — A session "accepts a context whose base root is its own," refusing a mismatched root.
- workspace-design.md:196 — "Files are created 0600; the read-only property is a discipline, that tracked files in a base are never opened for writing, and Overlay is the only way to change contents."
- workspace-design.md:210 — The invariant: evaluating a target in scope "yields the evaluation the whole tree would, provided the evaluation reads nothing else in the tree."
- workspace-design.md:251 — "The policy: a sparse scope is used only where one port is prepared and nothing else is asked of the tree."
- workspace-design.md:253 — "`Stage` refuses to generate an index over a root whose scope is not All, and so does name resolution's directory enumeration."
- workspace-design.md:257 — "Installing an already cached generation into a sparse root is allowed."
- workspace-design.md:349 — "`EditTree` refuses a file that does not exist in the base, so an overlay cannot add a file."
- workspace-design.md:336 — "Hardlinked overlays require the base and the overlay on one filesystem"; a writer through a link "would corrupt the base."
- workspace-design.md:114 — "An overlay is built from the base's tree entries, never from a walk of the base directory."
- workspace-design.md:126 — Comparators normalize each side "by its own root," since two roots would otherwise compare as a changed `filespath`.
- workspace-design.md:183 — Index installation "becomes a write to a temporary name and a rename, so a concurrent reader... never sees a truncated index."
- workspace-design.md:292 — A correction's scope check "refuses any changed path outside the tracked port's directory."

### resolution-design.md

- resolution-design.md:88 — `Degraded` "is only ever set with `Kind == Continue`"; unreachable master with no prior job is an error.
- resolution-design.md:141 — `Require` "refuses a selection with no open contribution rather than resolving Fresh."
- resolution-design.md:151 — `Offline` "forbids the master fetch... rather than made"; it "is a refusal, not a degradation."
- resolution-design.md:166 — "A nil State means no records: every selection is Fresh, or Onto for an explicit adopt, and nothing is read."
- resolution-design.md:184 — "A nil store with `Adopt` is an error: adoption needs the store."
- resolution-design.md:214 — "It answers without a database, and never writes for a preview."
- resolution-design.md:222 — "`Preview` skips the continuation check, because `CheckContinuation` refreshes the pull request... which records what it observed."
- resolution-design.md:228 — "It degrades explicitly. Master unreachable with a prior job is a Continue with `Degraded` set, never a silent Fresh."
- resolution-design.md:233 — "It is fallible... nothing caches [a resolution] across commands."
- resolution-design.md:305 — "The state database is never created or written by a dry run," except the acknowledged `--dry-run --adopt` inconsistency.
- resolution-design.md:326 — "One open contribution per port; a second branch adopted for a port with one is refused as today."
- resolution-design.md:415 — "An action that prepares no update... resolves to the contribution as recorded whatever else is asked."
- resolution-design.md:424 — "Adoption is not a mode of `Resolve`... `Resolve` reads and never writes."
- resolution-design.md:441 — "A method that accepts a nil store on an engine whose every other method refuses one is a rule to state once and test."

### usage.md

- usage.md:7 — Every checkout-scoped command "refuses anything else... before touching the state database or fetching into it."
- usage.md:23 — "Dockhand does not try another identity after rejection."
- usage.md:23 — `logout` "does not revoke the token on GitHub or modify environment variables or gh credentials."
- usage.md:59 — "A pull request from someone else's fork is pushed to like your own when GitHub allows it... and refuses otherwise."
- usage.md:84 — "`--working-tree` explicitly captures tracked checkout contents... without changing the index or branch."
- usage.md:86 — "Wait/cancel freeze the pending jobs at selection; later submissions do not join."
- usage.md:86 — "Quitting the table stops its processing; accepted work stays recorded for the next console, wait, or serve."
- usage.md:90 — "Older results without a recorded verifier identity require a fresh build before they can be reused."
- usage.md:92 — "`assess` and `outdated` never use the mirror and make no network request for the index."
- usage.md:94 — "`wait` resumes a fixed job selection; it never submits another verification."
- usage.md:105 — `sync` "retires a closed or merged contribution only when no job is pending and the published revision still matches both the PR head and the local branch."
- usage.md:105 — "A deleted local branch or deleted fork does not prevent recognizing a matching completed PR."
- usage.md:107 — `abandon` "preserves branches, evidence, and any remote PR; it does not close the PR."
- usage.md:107 — "Refreshing a reopened PR never reopens a retired local contribution or redirects newer work."
- usage.md:115 — "A failed fetch stops the request without falling back to stale source."
- usage.md:130 — A git-fetched port "that also declares checksums, or a generated Go or Cargo dependency block, is refused."
- usage.md:145 — "Already-current automatic bumps complete without a PR."
- usage.md:147 — Dockhand "writes `jq: ` itself and refuses a subject that already carries it."
- usage.md:158 — "An existing PR keeps its body, since the description may be a maintainer's and the checklist a reviewer's."
- usage.md:162 — "Cancellation cannot undo an already issued PR request."
- usage.md:181 — "This command does not regenerate those blocks or turn a changed upstream archive into a trusted release automatically."
- usage.md:190 — `--dependents` "requires local Tart verification and cannot be combined with `--provider github`, `--unverified`, or `--dry-run`."
- usage.md:201 — "A name outside the discovered cohort stops planning before builds start."
- usage.md:205 — "This option does not authorize edits or revision bumps to downstream ports."
- usage.md:227 — "Dockhand does not stage files or reset the checkout otherwise."
- usage.md:229 — "Unexpected remote changes require reconciliation."
- usage.md:243 — `outdated` "does not fetch MacPorts master, initialize a database, download source archives, create branches/jobs, or grant publication authority."
- usage.md:251 — "These selectors do not expand workflow or publication authority."

### target-workflow.md

- target-workflow.md:25 — "A contribution exists once preparation is durably accepted. It may have no branch or revision yet."
- target-workflow.md:27 — A missing/renamed/deleted branch "must not trigger fallback to the current checkout or to an older successful job."
- target-workflow.md:29 — "Continuing commands first select the recorded contribution... They must not resolve against a fresh upstream index or the unrelated current checkout."
- target-workflow.md:31 — "Never silently include untracked files."
- target-workflow.md:32 — "Standalone `publish` does not implicitly build, commit edits, or grant new publication authority."
- target-workflow.md:33 — "A different explicit version, or a newer discovered release on a later bump, must not overwrite an existing branch or human corrections."
- target-workflow.md:34 — A successful already-current discovery "creates no branch and starts no build"; must not be "labeled merged, published, or failed."
- target-workflow.md:42 — "A read-only status command must remain a database snapshot, not fetch Git refs, inspect a VM, or reconcile the workflow."
- target-workflow.md:60 — "Concurrent equivalent bump requests must not create two contributions or two active preparations."
- target-workflow.md:62 — `GeneratedCommit` "is currently immutable from insertion"; the narrow transition, once set, "remains immutable."
- target-workflow.md:64 — "Never combine unrelated jobs merely because their target matches."
- target-workflow.md:72 — "A concurrent new revision cannot redirect an already accepted job."
- target-workflow.md:76 — "Dirty working-tree-only evidence cannot authorize publishing a different committed tree."
- target-workflow.md:88 — Discovery "must not translate Tcl expressions into Go regexes or scrape human-readable `port livecheck` output."
- target-workflow.md:88 — "Do not infer series by splitting the port name, choose a newer different series, or change independently pinned siblings."
- target-workflow.md:90 — "A changed listing must not silently retarget an accepted preparation."
- target-workflow.md:106 — "Do not confuse dependents with targets whose source declarations changed."

### bump-coverage.md

- bump-coverage.md:29 — "Do not execute fetch hooks to discover whether they mutate something."
- bump-coverage.md:31 — "`known_fail` alone is not a license to discard a context or bypass a hook."
- bump-coverage.md:31 — "Do not remove the guard, claim build coverage, or suppress failure if the user actually verifies that platform."
- bump-coverage.md:33 — "A unique uncovered artifact or changed pin must still block the plan."
- bump-coverage.md:53 — "Keep context selection policy in `portedit`; strengthen the evidence supplied by `eval`."
- bump-coverage.md:55 — "Reads in formatting/build arguments must not be silently ignored merely because their command is usually unrelated to fetching."
- bump-coverage.md:57 — "Observing several profiles is not proof of arbitrary Tcl coverage."
- bump-coverage.md:63 — "An entire Portfile must not automatically become one release scope."
- bump-coverage.md:65 — "Matching version strings, names, or merely living in one file are insufficient" for shared-release membership.
- bump-coverage.md:67 — "Without the flag, sibling changes remain a refusal."
- bump-coverage.md:71 — "Shared-release targets are not reverse dependents: do not set `IncludeDependents` as a shortcut."
- bump-coverage.md:79 — "Do not choose the first archive or infer ownership only from a filename."

### portindex.md

- portindex.md:9 — "A completed generation is identified by source tree plus indexing environment... does not depend on the requesting command, verification provider, contribution branch name, or which seed produced it."
- portindex.md:37 — "Host facts that affect generation must either be controlled or represented in compatibility checks."
- portindex.md:41 — "A small pointer... is a seed-selection hint; it cannot substitute for an exact source match."
- portindex.md:43 — "An index file's mere existence does not certify complete or usable coverage."
- portindex.md:47 — "Existing unrelated index failures must remain visible; an index is discovery metadata, not proof that every port builds."
- portindex.md:51 — "An incompatible indexing environment, missing source objects needed for comparison, invalid cache metadata, or an unprovable invalidation set also requires a safe rebuild rather than assumed reuse."
- portindex.md:53 — "A heuristic covering the most recent number of commits does not establish that relationship."
- portindex.md:57 — "Completed generations are immutable."
- portindex.md:59 — "Once a seed is copied into a private workspace, release any seed-copy protection before candidate indexing."
- portindex.md:61 — "Cache eviction does not invalidate recorded verification evidence."

### output.md

- output.md:15 — "Reports always go to stderr and results always to stdout, at every level, so `--json` output stays parseable while reports scroll."
- output.md:17 — "No logging library is adopted. Reports are user-facing sentences, not log lines."
- output.md:31 — JSON envelope: "`error` is empty on success and the one-line failure otherwise; `result` is typed per command and null on failure."
- output.md:50 — "Identifiers never appear at the info level."
- output.md:50 — "Warnings that change what a person should expect... are info."
- output.md:66 — "A preparation that stops before creating a branch retires its contribution rather than leaving an empty open one."
- output.md:74 — "Keys map onto existing verbs and add no authority."
- output.md:76 — "Verify, publish, cancel, and abandon confirm before acting, since they cost minutes, push, or discard."

### operations.md

- operations.md:12 — Backup "must not exist" at destination; "an absent database is an error, not an invitation to initialize one."
- operations.md:14 — "An ordinary copy of a live state.db alone can omit committed WAL data; use `db backup` instead."
- operations.md:16 — Backup "does not include Git objects, source checkouts, VM images, live VMs, build logs, or remote pull requests."
- operations.md:20 — "Stop every Dockhand driver using the affected database before changing its operational file."
- operations.md:20 — "Do not run a backup and the original as separate coordinators for the same Tart pool."
- operations.md:33 — "Merely waiting for an old claim to expire does not reconstruct missing history."
- operations.md:35 — "Restoring/overwriting the live database and repairing lost external-operation history are deliberately not automatic commands."
- operations.md:39 — "Successful and canceled runs are released even when the flag was set."
- operations.md:39 — "It does not change verification evidence compatibility."
- operations.md:41 — "Unknown leftovers from process death are not otherwise swept blindly."
- operations.md:60 — Diagnostic files "can be removed only after both the job and the confirmed resource release are older than the threshold."
- operations.md:62 — "Active jobs, unresolved attempts, uncertain ownership, and unregistered directories are not discovered by scanning the filesystem for garbage."
- operations.md:66 — "Pruning logs does not invalidate a verification result."
- operations.md:68 — "Unlinking a lock while another process holds or awaits its inode can create two independent locks for the same operation."
- operations.md:74 — "A root whose lock is held belongs to a live process and is never touched."
- operations.md:80 — Cleanup "never removes the staged index already copied into a build."
- operations.md:82 — GitHub log cleanup "does not delete anything on GitHub or require authentication."

### github-verification.md

- github-verification.md:3 — "Selecting it authorizes pushing the candidate branch to that fork."
- github-verification.md:23 — "Dockhand does not silently try another credential after rejection."
- github-verification.md:23 — "Dockhand does not require Actions write access to stop tracking a job."
- github-verification.md:27 — "It does not pattern-match that workflow's shape"; a push trigger excluding the branch "is refused, because that run would never exist."
- github-verification.md:33 — "Explicit variant overrides, `--tests declared`, `--tests skip`, `--image`, and `--from-source` are refused with this provider."
- github-verification.md:39 — "Recovery does not dispatch or rerun a workflow."
- github-verification.md:41 — "The provider still compares before it writes, so a branch holding anything else is left alone and reported rather than replaced."
- github-verification.md:43 — "The provider refuses to replace an existing different contribution head."
- github-verification.md:47 — "Canceling an admitted job... does not cancel the remote Actions run"; "no branch or workflow run is deleted during cleanup."
- github-verification.md:61 — "There is no elapsed-time failure cutoff... A late run cannot revive canceled tracking."
- github-verification.md:65 — "Nothing here happens on a driver's initiative; it happens when someone asks for a verification."
- github-verification.md:69 — "Completed caches can be read without resolving credentials or contacting GitHub."
- github-verification.md:73 — "Tart capacity waits and failed local builds do not cause GitHub submission."

### bump-planner.md

- bump-planner.md:8 — "Observation does not authorize an edit."
- bump-planner.md:13 — "Test the actual requested candidate and reject ambiguous edits."
- bump-planner.md:15 — "Concrete evaluator dependencies belong in application wiring. Capabilities consume small interfaces, not evaluator constructors."
- bump-planner.md:22 — "Modeled contexts are explicitly identified and cannot establish that a build ran on another OS."
- bump-planner.md:26 — "Alternate contexts must be request-scoped and isolated: do not change a shared evaluator's default platform or allow one modeled context to leak into another."
- bump-planner.md:28 — "`assess` and `bump` must use the same planning mechanisms; assessment reports unperformed download/helper/build stages explicitly."
- bump-planner.md:40 — "A changed observation is evidence of impact, not automatic permission to broaden the requested update."
- bump-planner.md:60 — "Changes to unselected sibling metadata are still refused; a shared multi-subport update needs an explicit contribution scope."

### human-corrections.md

- human-corrections.md:7 — "No marker in Git config, commit trailers, private refs, or tags is required."
- human-corrections.md:9 — "A rebase or amended commit does not invalidate evidence solely because the commit ID changed."
- human-corrections.md:11 — Commands "must not silently adopt a moving branch while executing an already accepted job."
- human-corrections.md:11 — "A newer revision supersedes publication authority for the older one; the driver checks the current revision before any remote effect."
- human-corrections.md:17 — "Dockhand does not automatically stage edits"; amendment "does not absorb unrelated commits or silently stage new files."
- human-corrections.md:21 — "Any other checkout is the person's work: preserve the candidate and report the conflict; never reset or discard it."
- human-corrections.md:29 — "The user's checkout is not left in the middle of a rebase."
- human-corrections.md:33 — "A missing recorded branch is an actionable error, not evidence that the contribution was deleted."
- human-corrections.md:39 — "A local rename does not rename or recreate the remote PR."
- human-corrections.md:45 — "There is no unconditional force push."
- human-corrections.md:47 — "Preserve human-edited PR text by default; do not regenerate the whole body from stale templates."
- human-corrections.md:47 — "Closed or merged PRs require a new explicit decision, not automatic reopening or recreation."
- human-corrections.md:49 — "A passing root build cannot substitute for missing, failed, or incomplete downstream coverage."
- human-corrections.md:49 — "Discovery alone never authorizes edits to downstream Portfiles."

### instance-coordination.md

- instance-coordination.md:9 — Detaching on the presence of a leader "would make one command mean two things depending on invisible ambient state."
- instance-coordination.md:9 — "Observing keeps every command's meaning fixed."
- instance-coordination.md:11 — "A second serve does not exit and does not compete."
- instance-coordination.md:21 — "A standby cannot tell a dead leader's claim from a live one" without an identified claim owner.
- instance-coordination.md:27 — "The manager's contract says otherwise today: 'Claims in state allow multiple callers; there is no resident singleton lock.'"
- instance-coordination.md:44 — "What it cannot see is a reservation"; two databases sharing one Tart home "can each overshoot the limit by one."
- instance-coordination.md:58 — "Not worth reaching for: consensus libraries, a network database, or coordination across machines."

### principles.md

- principles.md:7 — "A syntax tree can locate an edit; it does not replace MacPorts as the authority on what the Portfile means."
- principles.md:13 — "Edit fidelity, successful builds, and complete dependency declarations are separate questions."
- principles.md:17 — "Deterministic decision logic judges those observations and the applicable policy. Keep decisions testable without running a VM or contacting GitHub."
- principles.md:19 — "Missing or unreadable evidence remains unknown. An explicit override records a policy decision; it does not turn failed or absent verification into a pass."
- principles.md:23 — "Reuse results only when those inputs satisfy the new request. Temporary staging paths and whether a CLI is attached do not change the build question."
- principles.md:25 — "A branch is a convenient reference to work, but its name is not evidence identity. Advancing a branch must not silently transfer an earlier verdict to new contents."
- principles.md:27 — "An executable's reported version does not establish which source release produced it... Reported version text alone must not reject a port update."
- principles.md:31 — "A PR association survives revision changes."
- principles.md:33 — "Discovery and PR monitoring produce observations that may motivate work. They do not by themselves authorize edits or publication."
- principles.md:39 — "Those lifetimes differ. Retrying a build preserves the earlier attempt."
- principles.md:43 — "Durable submission is distinct from driver pickup and provider admission... Waiting or tracing changes how long the CLI stays attached, not who owns the work or what publication is authorized."
- principles.md:45 — "Commands do not spawn background drivers... durable records alone do not execute pending work."
- principles.md:49 — "A snapshot timestamp describes when state was read, not when a provider or forge was last observed... Reading status does not authorize new external work."
- principles.md:53 — "Missing source requires an availability decision, never silent replacement with a moving branch tip."
- principles.md:55 — "Neither a lock nor a recorded intention alone guarantees that an external action happens only once."
- principles.md:59 — "A dependency outside the original dependent list is not automatically unrelated to the change."
- principles.md:61 — "Publication policy decides whether the recorded coverage is sufficient; it must not invent successful outcomes for untested targets."

### dependency-preparation.md

- dependency-preparation.md:10 — "A preparation requiring a missing helper fails before downloading or editing its source."
- dependency-preparation.md:16 — "Differences stop preparation so maintained overrides are not silently replaced."
- dependency-preparation.md:20 — "A helper's generated Portfile is never evaluated or substituted for the original."
- dependency-preparation.md:26 — "A Go port without a vendor declaration does not acquire a helper requirement merely by using the Go PortGroup."
- dependency-preparation.md:30 — "It never lowers a minimum, never adds one to a port that declares none... and never refuses a bump over it."
- dependency-preparation.md:41 — "Only branch selectors can be declared" for Cargo Git dependencies matching the lockfile.
- dependency-preparation.md:43 — Offline build "must declare every Git crate, so a tag, rev, or default-branch selector is refused."
- dependency-preparation.md:46 — "It does not invent a second GitHub archive URL convention."
- dependency-preparation.md:48 — "`cargo.dir` must stay inside the source archive. `cargo.update` must be disabled."
- dependency-preparation.md:58 — Dependency-block regeneration "does not create a branch, start a VM, or publish a pull request... generating a consistent dependency block is not build evidence."

### development.md

- development.md:9 — "A stamped tag always wins over that variable, so a checkout build cannot be mislabeled by a stale one."
- development.md:11 — "The Makefile sets `-mod=vendor`, so a missing or stale vendor directory fails loudly rather than downloading modules."
- development.md:15 — Executables under test "write it with testsupport.WriteExecutable, never with os.WriteFile and an executable mode."
- development.md:15 — "The helper writes while holding syscall.ForkLock, so no fork can start in that window."

### macports-compatibility.md

- macports-compatibility.md:3 — "Dockhand checks capabilities, rather than accepting or rejecting an installation solely by Base version."
- macports-compatibility.md:3 — "A source-review record does not certify a local installation or its PortGroups."
- macports-compatibility.md:7 — "The probe never executes a fetch hook, modifies source files, or fetches an archive."

### roadmap.md

- roadmap.md:31 — "Do not add a generic batch interface merely to implement one shared release."
- roadmap.md:53 — "Holding one such subport back is not a flag's job."
- roadmap.md:61 — "Read-only `outdated` and starting a driver must not silently grant publication permission."

### README.md

- README.md:1 — "Your stored data is safe: the state database is migrated forward by each newer Dockhand, never discarded."
- README.md:16 — "You look at the result at every step, and nothing goes out until it has been built or you have said it should."
- README.md:20 — "Nothing is ever pushed to the MacPorts repository directly. You do not need commit access."
- README.md:21 — "Lint, build, and install decide the verdict... a failing test suite does not fail the build."
- README.md:23 — "When a Portfile does something Dockhand does not understand, or a build fails, or upstream looks wrong, it stops, keeps what it has done, and tells you why. It never guesses."
- README.md:33 — "Your checkout is not touched." "If the build fails, the branch is kept and no pull request is opened."
- README.md:60 — "Every accepted job is durable... Ctrl-C detaches without canceling anything."
