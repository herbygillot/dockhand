# Part 4, item 2: the command package split

The roadmap's part 4, item 2. A go/types cross-reference of
internal/command at 81f3ec00 found the hubs (settings, used by 23 other
files; `supportsJSON` by 18; `branchFlags` by 12) and the backward edges
a cut must remove first. Architecture checked the map and the order
(2026-10-22):

- **Packages**, under internal/command/:
  - `view`: the JSON shapes and the writers and words they share.
  - `run`: following and driving a queued check.
  - `host`: the Mac, its timings, and `machineHost`, the one place that
    imports tart, credential, keychain, and macos.
  - `settings`: Settings, open, places, the editor, `SupportsJSON`.
  - `branchflags`: the flags, the branch choice and selection, and
    `namedOrLatest`; it takes the engine and the streams, not Settings.
  - `cmdtest`: the shared test helpers.
  - The verb packages, each with what shares its engine calls: edit and
    revbump with `author`, rebase with `tidy`, archive with `clean`.
- **Order**: step 0 inside the package; then view, run, the shared three,
  cmdtest; then the verbs, smallest first, with status and check last.
  Each step goes green on its own.
- **Tests**: a verb's end-to-end tests become an external package
  importing root, each its own binary. The cross-cutting ones (rollout,
  stealth, baseline, json, helpreference) stay root's.
- **boundary_test** walks the subpackages and asserts the shape. No verb
  imports another verb, but bump imports author and status its own watch.
  Verbs import view, run, the shared three, term, and the engine. View,
  run, and the shared three import no verb.

## Step 0, inside the package

- review.go and its test are tidy.go and tidy_test.go, since they hold
  tidy and undo (reviewpr.go is the pull request review).
- The eleven methods on settings and host that verb files defined are
  functions taking them, context first: `githubLogin`, `githubLogout`,
  `cleanupAfter`, `imagesOf`, `providerLines`, `tartReadiness`,
  `githubReadiness`, `missingSteps`, `editorCommand`, `publishing`, and
  `serveOptions`.
- `runQueued` takes `config.Places` and a drain function in place of
  settings. Settings' `drain` starts the background drain.
- status --watch is given the command line's runner (`settings.command`)
  rather than calling `Run` itself. This is a scaffold: with watch in the
  status package, watch calls the status renderer directly, and the
  injection goes in step 5.
- **Two text dependencies typed in the engine.**
  - `PortDiff.Change()` gives `PortAdded`, `PortRemoved`,
    `PortRevisionOnly`, or `PortChanged`. impact tested diff's words
    (`kind != "changed"`), and the JSON replaced the words' spaces. Both
    now read the kind, and the JSON values are unchanged.
  - `SubmitPlan.Push()` gives `PushOthersPushed`, `PushNothingNew`,
    `PushReplaces`, `PushAdds`, or `PushCreates`, which `pushWords` words.
- `underUpstream`, a one-line wrapper, is gone; its callers call
  `engine.UnderUpstream`.
