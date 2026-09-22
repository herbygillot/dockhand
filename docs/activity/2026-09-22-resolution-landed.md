# 2026-09-22: the resolution landed

Steps 2 through 6 of the [resolution design](../resolution-design.md)'s
sequence, in five commits after [provider choice](2026-09-22-provider-choice.md)
went first. What a selection means for an action is now one value,
`workflow.Resolution`, computed once by `Engine.Resolve` and consumed by
the bump, the preview, the verification, adoption, and the corrections.

**The value and the method.** Four kinds: Fresh from master, Continue
from a prior job's recorded source, Onto a contribution's current
revision, and Adopt, which tracks a hand-made branch and then reads as
Onto. `Resolve` is the one engine method that accepts a nil store: with
no database every selection is Fresh and adoption is refused. The engine
fetches master itself when a resolution needs it; `Offline` refuses the
fetch as a value, `Preview` fetches master but checks no continuation
and writes nothing, `Lookup` stops at the records, and `Require` refuses
a selection with no contribution in the words the records use. A
Continue reached with master unreachable is degraded and says so in its
detail rather than failing, which is the answer to the second review's
finding that a preview and a bump read the port from different places.
An action that prepares no update, a verification or a correction,
resolves to the contribution as recorded, with its change and current
revision, by name, by branch, or by change. Every path is under test in
`resolution_test.go`, with master pointed at the fixture through Git's
URL rewriting and an unreachable master mapped to a missing path.

**The consumers.** `Engine.BindPreparation` takes a resolution in place
of a source, an onto target, a change, and the inherited fields; the
binding checks the kind against what it found and fills the author.
`app.BindPreparation` resolves and hands the value over, and the three
decisions it made, continue, onto, or fresh, and the intent merge, are
gone from `app`. The preview resolves under `Preview` with the store
read-only when the database exists and with no store otherwise, so a
dry run of a port mid-contribution previews onto the contribution the
way the bump would land, and the CLI's dry-run adoption is the Adopt
kind rather than a separate call; the state database is still never
created by a dry run that finds none. `app.BindVerification` resolves
the tracked contribution, by target, branch, change, or the current
branch, and the engine's verification binding takes the resolution in
place of a selector; the adopt-but-tracked guard is the same lookup
with the same not-found tolerance. `BindCorrection` resolves its
contribution the same way, and its transaction rereads it by branch
with the pull request as before.

**Size.** `app/preparation.go` lost 90 lines net and `app` no longer
imports the contribution lookup; `workflow` gained the value, the
method, and their tests. The messages a person sees are unchanged, and
the CLI's continuation, adoption, and preview tests pass unchanged but
for one addition: a preview onto a contribution asserts the branch, the
diff on the contribution's Portfile, and the wording.

**Left as found.** Two defects the design's validation surfaced are on
the roadmap: an update onto an adopted stub contribution inherits no
stub redirection, and `--dry-run --adopt` builds the services and so
creates the database. `Engine.PreparationInput` survives as a public
wrapper for one lifecycle test. Whether the four bindings and `Resolve`
lift into a `workflow/intake` leaf is the sequence's seventh question,
and the answer waits on whether the bindings now read alike enough for
the boundary to be obvious.
