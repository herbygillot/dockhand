# Intents

**Status: provisional.** This is a catalogue and the reasoning behind it, not a
commitment. Nothing here has entered the decision log, because nothing here has
been decided — see *Candidate decisions* at the end for the entries this
document would generate if the shape holds.

**Scope, as of 2026-08-24.** The catalogue stands. Toolchain-constraint
propagation across `depends_build` is explicitly deferred and is *not* an
intent. Four entries are first-class for near-term work — `Bump`, `Obsolete`,
`RefreshChecksums` and `BumpRevision` — and the remainder are kept, with the
long tail of scalar metadata edits probably collapsing into a single verb (see
*The `modify` boundary*).

---

## What qualifies as an intent

The one-pager's phrasing is load-bearing: dockhand takes a port and a *desired
end state*. That gives an admission criterion sharper than "is it mechanical" —
an intent must be a state that can be **named without knowing how to reach it**.

`version = 1.4.2` is namable. "Make this port build on Apple Silicon" is not: it
is a goal, and the set of edits reaching it is not derivable from the statement.

This has a consequence for the ladder. T4 currently means "work that genuinely
needs a human", but the criterion splits it in two:

- **Unstatable.** No end state can be written down. "Fix the build." Dockhand
  cannot accept the request at all, and no amount of better recognizers changes
  that.
- **Unreachable.** The end state is perfectly clear, but dockhand cannot get
  there — the edit needs judgement about *content*, as in authoring patch hunks.

The two fail differently and should be reported differently. The first is a
rejected request; the second is an accepted request with a refused plan, and it
is the one that can still produce a useful diagnostic. Conflating them means the
second is reported as though nothing could be learned, which is false.

---

## The catalogue

| Intent | Shape | Tier |
|---|---|---|
| `Bump {port, version}` | point | T0–T3 |
| `RefreshChecksums {port}` | point | T0 |
| `BumpRevision {port}` | point | T0 |
| `BumpEpoch {port}` | point | T0 edit, T4 decision |
| `RegenerateVendored {port}` | point | T3 |
| `ChangeDependency {port, spec, kind}` | point | T1 |
| `DropObsoletePatch {port, file}` | point | T1 |
| `RefreshPatches {port}` | point | T2/T4 boundary |
| `SetMetadata {port, field, value}` | point | T0 |
| `Obsolete {port, replaced_by}` | point | T1 edit, T4 decision |
| `MigrateIdiom {selector, from → to}` | **sweep** | T1–T2 |

### Notes on individual entries

**`Bump`** spans the entire ladder by itself. It is T0 when the version is
literal and nothing else moves, T2 when the version is computed and the
constrained evaluator must locate which literal to touch, and T3 when it drags a
vendored dependency block along with it. Every other intent sits at one or two
rungs.

The general form of that observation: **tier is a property of the triple
`(intent, port, target)`, never of the intent alone.**

*Amended by field evidence.* This originally said *pair* — `(intent, port)`.
Fifty ports of real maintenance falsified that. A `skopeo` 1.22→1.24 bump is one
literal on one line, T0 by every Portfile-shaped measure, and it was wrong: the
Go module path had been renamed `github.com/containers/skopeo` →
`go.podman.io/skopeo` upstream. Ten more ports in the same session moved
comparably — `package main` relocating, root modules vanishing, `Makefile`
becoming `Justfile`, and in one case a wholesale rewrite from Go to Rust.

None of that is visible in the Portfile at plan time, which means classification
cannot be answered from the ports tree alone. It requires reading the target
upstream tree, and therefore lands squarely on the read side of the read/write
invariant. Whether this generalises beyond Go is unproven — Go's module path is
unusually load-bearing — but a classifier that ignores the target is answering
an easier question than the one asked.

**`RefreshChecksums`** is the trap in the set. Two lines change, the tier is T0,
and every rule in D9 therefore says ship it unattended. But a checksum that
changes for an *unchanged* version means upstream re-rolled the tarball, which
is a supply-chain event rather than maintenance. This is the one case where tier
and evidence both point the right way and the correct answer is still to stop
and tell a person. Either D9 acquires an escape hatch, or this intent is
non-autonomous by construction. The latter is preferable — a policy exception
that lives in the intent is visible; one that lives in the gate is not.

*Three causes wear one edit.* The byte change is identical in each case and the
correct disposition is not:

| Cause | Right response |
|---|---|
| Stealth update — upstream re-rolled the same version | Investigate. Possibly a security event. Never unattended. |
| Distfile renamed by a portgroup change, content byte-identical | Benign. No revision bump. |
| A checksum type added or upgraded | Mechanical. |

The middle case is the one field evidence produced: `gitea-tea`, where a
`github.tarball_from` change altered name, size and every checksum while the
extracted tree differed only in root directory name and 24 bytes of tar
metadata.

The useful consequence is that **dockhand can usually establish the cause
mechanically** — fetch both distfiles, compare the extracted trees. That turns
the intent from "refresh and hope" into "refresh, having classified why", and
the classification is exactly what decides whether a human must look. So the
intent carries its cause, and refuses to auto-promote when it cannot establish
one.

*The stealth-update edit is a four-part cascade, not a checksum swap.* From a
real merged PR (`pr-evidence.md` §5): refreshed checksums **plus** a revision
increment **plus** `dist_subdir ${name}/${version}_1` so cached old distfiles
cannot collide **plus** a dated comment marking the `dist_subdir` for removal
at the next real update, with a Trac ticket as provenance. Every dockhand
document until now modelled this as two lines of checksums. The gate stays
human; the edit, once approved, is fully mechanical and larger than assumed.

**`BumpRevision`** is listed above as T0, and the *edit* is. The *decision* is
not — but it has a single criterion, stated by a reviewer in exactly one
sentence (`pr-evidence.md` §4): *"this changes the installed files and thus
should increase the revision."* The five rules field evidence observed are
corollaries: version changed → reset (new files regardless); build fixed at the
same version → increment (files differ); whitespace or a gating annotation →
nothing (files identical); a byte-identical distfile relocation → nothing — a
case that took a deliberate re-fetch and tree comparison to establish.

The criterion is *measurable*: two destroots, one manifest diff. That moves
`BumpRevision` from pure judgement toward evidence — dockhand cannot decide
"should this revbump", but it can answer "did the installed files change",
which is the same question. The measurement sits at build-verification depth,
so it is not free; but when a build has already run for other reasons, the
revbump verdict is nearly free alongside it. It belongs in the class below
rather than beside `SetMetadata`.

Two further properties. Applied across a selector — `rdependentof:openssl` and
similar — it is the tree's principal mass operation, and therefore the intent
where "never outrun review capacity" bites hardest; the 192-file annotation
commit in the field evidence is exactly this shape. And **3,542 Portfiles (18%)
carry no `revision` line at all**, so for those the edit is a line *insertion*
rather than a span replacement, which requires a placement convention the tree
is not consistent about. See `reading.md` open question 3.

**`Obsolete`** was filed below as a trivial edit with a hard decision. The tree
says otherwise; it is the most structurally complex of the near-term four.
Drawn from the 109 obsoleted ports currently in the tree and from
`_resources/port1.0/group/obsolete-1.0.tcl`:

- **The common case is reduction to a template, not an edit.** Real commits run
  8 insertions against 82 deletions, 8 against 42, 6 against 37. The median
  obsolete port is 17 lines. The portgroup nulls `depends_*`, `patchfiles`,
  `distfiles` and `archive_sites` itself, so everything else is simply deleted.
  `Obsolete` therefore needs four *values* — name, version, categories,
  revision — which evaluation already supplies, and no spans at all.
- **The target is a port name, not a file.** In the minority case `libftdi`
  obsoletes only its top-level port, via `if {${name} eq ${subport}} {
  replaced_by libftdi0; PortGroup obsolete 1.0; epoch 1 }`, while `libftdi0` and
  `libftdi1` stay live; `cmake-devel` declares obsolete subports inline. 52 of
  the 108 obsolete ports have subport blocks. Those cases insert a guarded block
  into a file that must survive.
- **Argument order varies and both orders are legal.** `pev` and `libftdi` set
  `replaced_by` *before* `PortGroup obsolete 1.0`; the portgroup carries
  explicit handling for each (an `option_proc` for after, an `info exists` check
  for before). A recognizer must accept both.
- **It cascades into two other intents.** `revision` must move so users are
  migrated, and `epoch 1` appears in both `libftdi` and `cmake-devel` because a
  replacement's version can run backwards. So `Obsolete` pulls in `BumpEpoch`,
  filed here as rare and independent.
- **A dated convention lives in a comment.** `pev` carries
  `# This port can be removed on May 17, 2025`. Writing that means dockhand
  owning a policy on how long stubs live, recorded nowhere a parser will find
  it.

One cheap check falls out and is worth having: `replaced_by` must name a port
that exists in the tree. That is a real error class, caught for the price of a
lookup.

*(Noticed in passing: `devel/cmake-devel` has `### PortGroup obsolete 1.0`
commented out on a subport that still sets `replaced_by`. That looks like a live
bug in the tree rather than an idiom to support.)*

**`BumpEpoch`** and **`BumpRevision`** share a shape: the *edit*
is trivial and the *decision* is not. Both are T0/T1 mechanically and T4 in judgement. They are
worth carrying precisely because dockhand can do the edit faithfully once a
human has made the call, and doing it faithfully is not nothing — epoch and
`replaced_by` are both easy to get subtly wrong by hand.

**`DropObsoletePatch`** is the only patch-related intent that is fully
mechanical *and* verifiable, per D12: a patch that reverse-applies cleanly means
upstream merged the fix, and the correct edit is to delete the file and its
`patchfiles` entry. It is separated from `RefreshPatches` for that reason —
keeping them as one intent would drag the verifiable case down to the tentative
case's autonomy.

*Amended by field evidence.* The two do not cover the space. Both `mage` and
`mp4ff` carried patches that no longer applied and wanted neither disposition:
the patch's purpose was still needed, so dropping was wrong, and its anchor was
gone, so refreshing had nothing to refresh against. Both needed the patch's
*semantic intent* re-derived against a restructured tree. That is a clean
instance of the **unreachable** half of the T4 split — the end state is
perfectly nameable and dockhand cannot get there — and, as that split predicts,
it can still emit a useful diagnostic rather than a bare refusal.

**A finding the catalogue implies but never names: missed dependent
revbumps.** PR evidence §6 is a nine-port repair PR titled "Rev bump
dependents" that exists because the original bump missed them — and the
coordination mechanism that failed was a Portfile *comment* instructing future
updaters which dependents to bump. After any `Bump`, "dependents whose
revision did not move" is an enumerable, cheap check (`dependentof:` plus a
revision read), and its output is a proposed grouped `BumpRevision` — the
promotion path this document already defines. This is not the deferred
toolchain-floor propagation; it is core bump workflow.

**`MigrateIdiom`** is the only sweep, and it is where most of the fleet-scale
value sits: `PortGroup` version updates, `cxx11 1.1` removal, `master_sites` to
`github.setup`, compiler blacklist syntax. It is also the only intent whose
input is not a port.

---

## The `modify` boundary

The long tail — `SetMetadata` and its relatives — plausibly collapses into one
verb rather than one verb per field. The risk is that a general `modify` becomes
the escape hatch everything else leaks through. A rule that holds the line:

> A field belongs to `modify` if setting it has **no cascade** and **no
> semantics dockhand would have to own**.

That admits `maintainers`, `categories`, `license`, `homepage`, `description`,
`long_description` — scalars where the value is the whole story. It excludes
`version` (cascades), `revision` (a decision procedure with five inputs),
`checksums` (computed, never set), and `depends_*`, `patchfiles` and `variants`
(structured, each needing its own recognizer and its own tier).

The rule also explains the shape of the catalogue: every entry that survived as
its own verb fails the rule, and each fails it for a different reason.

---

## Shape: point, sweep, and cohort

A sweep is not a point intent applied N times, and should not be modelled as
one. The difference is in rejection semantics:

- A point intent that declines is a **refusal**. The user asked for one thing
  and did not get it.
- A sweep that succeeds on 300 ports and declines on 40 is a **success**. The
  declines are the expected tail, and they are output, not failure.

Cardinality also changes everything downstream — gating (D9, D10), PR
granularity, and above all review capacity. "Never outrun review capacity" is a
constraint that only bites on sweeps; no single-port intent can violate it.

### A third shape *(field evidence)*

`skalibs` records its configure target in a file that `execline` and `s6` then
compare verbatim. The recorded value contained the kernel point release, so any
macOS update broke both dependents. The repair had to touch all three ports, and
`skalibs` had to be rebuilt first or the consumers still mismatched.

That is neither shape. A sweep is N independent applications; this is **one
change whose unit of correctness spans several ports, with an ordering
constraint between them**. Call it a cohort. Its distinguishing property is that
the intermediate states are broken by design — after port one and before port
three, the tree is in a state no one should ship.

The consequence is a genuine conflict with the tree's own conventions. One PR
per port is the norm, because different maintainers review different ports — but
a cohort's per-port PRs are individually unmergeable. A single PR spanning three
maintainers' ports is the alternative, and it is antisocial for the same reason
a 192-file annotation sweep is unreviewable as one. This is recorded as
unresolved; see `cli.md` open question 2.

*Resolved by PR evidence.* The conflict claim was wrong. Observed practice
(`pr-evidence.md` §1–2): a nine-port revbump PR across four maintainers merged
without granularity objections; a four-port binutils update merged with zero
comments; and in one review the cross-port pairing was *required* — "the
revision increase for the toxic port needs to be done at the same time."
The norm is one PR per logical change, and the unit reviewers police is the
**commit**. A cohort is an ordinary PR whose commits are grouped by logical
change. What remains of the cohort shape is only what was always real: the
build-ordering constraint between its members.

---

## Cascade, and where `Plan` becomes partial

Intents cascade. `Bump` implies a checksum refresh, implies possibly
regenerating a vendored block, implies resetting `revision`, implies possibly
discovering patch rot. The human asked for one thing and the plan touches five
spans.

This does not threaten diff minimality — minimality is a property of spans, not
a count of fields moved. Bytes outside the touched spans are still identical.

It is, however, the precise origin of the open question about `Plan` being
partial above T2. The plan is partial because a T3 block does not exist until a
generator has run, and the generator is *downstream of the intent that triggered
it*. Partiality is not a property of T3; it is a property of cascade.

**Proposed resolution.** `Intent` is a small closed sum type of **root requests
only**. Cascade expansion belongs to planning, not to the type. That keeps
`Intent` user-facing, stable and small, while `Plan` carries the partial,
staged, messy structure that the cascade actually produces.

One consequence to record: the revision-reset-on-version-bump rule is MacPorts
*convention*, not something base enforces. Encoding it in the cascade is right,
but it is dockhand asserting a semantic, which brushes against "own the edit,
never own the semantics." It belongs with the recognizer set, in the small
blast radius, and it should be written down as a rule rather than buried in
`Bump`'s implementation.

---

## Findings propose intents

A `Finding` and an `Intent` are the same kind of statement — a gap between
actual and desired state — differing only in origin. One is observed, the other
requested.

That symmetry is exploitable:

- A pristine-environment build failure (D4's third proposition) *is* the
  observation "dependency X is used but not declared". Its remedy is a
  `ChangeDependency` intent.
- A reverse-apply probe that succeeds *is* `DropObsoletePatch`.
- A recognizer decline on a known idiom *is* a candidate `MigrateIdiom`
  selector.

*One correction from field evidence.* This section assumes a finding is about
the port under intent. It is not always. `copilot` failed to build; `copilot`
was fine. Its build dependency `packr` had been independently broken for months
— its Makefile's `build` target ran `go get`, impossible under an offline build
— and nothing else built `packr`, so it had sat unnoticed. A `Finding` therefore
needs the port it is *about* as a field distinct from the port under intent,
and a promoted intent may target a port the user never named.

If this holds, D7 gains a third exit disposition beyond dismiss and
debug-in-snapshot: **promote to intent**. The findings sink stops being a
terminus and becomes the loop's re-entry point, which is likely what the
workflow diagram's two human re-entry points were reaching for.

The gate matters here. A promoted intent has a machine as its origin, and D9's
autonomy rules were written for human-originated ones. A finding that proposes
its own remedy and then acts on it unattended is a closed loop with no human in
it — which is exactly what D9 exists to prevent. Promotion should therefore
produce a *proposal*, never an execution.

---

## Open questions

1. **Is `Intent` an end state or a command?** If `Classify` and `Verify` are
   intent variants, then `Intent` means "anything dockhand can be told to do"
   and the intent-to-diff signature dies, since neither produces a diff.
   Recommendation: keep `Intent` strictly end-state, and expose classification
   and verification as separate entry points. They are pipeline stages, not
   requests.

2. **Does `Intent` carry a variant scope?** `Metadata` is variant-relative, so
   "add `depends_lib port:foo`" is ambiguous until the variant is named. `Bump`
   mostly escapes this; `ChangeDependency` does not. If the answer is yes, the
   flattening in the superseded interface sketch has to go, and `Diff` must
   assert that both sides used the same variant set.

3. **T1 and T2 are undefined.** Every document leans on the five-rung ladder,
   but only T0 (literal substitution), T3 (regenerated dependency blocks) and
   T4 (human) are ever stated. The tier column above uses T2 to mean "the
   version is computed and the constrained evaluator is needed to locate the
   literal", inferred from D3, and T1 to mean "structured but literal edits
   beyond single-token substitution". Neither is authoritative. The ladder
   should be defined once, in one place, before the recognizer set is designed
   against it.

4. **Is port creation in scope?** A new port has no existing spans to preserve,
   so D2's read-as-Tcl-write-as-text asymmetry has nothing to act on. It is
   generation rather than editing, and probably a different tool. Worth an
   explicit non-goal if so.

   *Counter-argument from field evidence.* "A working port for upstream X at
   version Y" passes the nameable-end-state test cleanly, so the admission
   criterion does not exclude it — only D2 does. And creation is plausibly the
   hardest task facing exactly the newcomers a PR-based workflow implies, with
   the most unwritten convention attached. The documents do not currently say
   whether the omission is deliberate.

5. **What replaces `SetMetadata`?** If the `modify` boundary above holds, the
   entry disappears from the catalogue and becomes one verb over an allowed
   field list. The name is unsettled: `modify` promises a generality the rule
   deliberately withholds, and `set` is more honest about the limit.

6. **Do the three verification propositions span the space?** Field evidence
   says no: four ports compiled, linked, destrooted and passed `port lint` while
   the version stamp the update existed to change had silently not happened.
   Edit fidelity passed, the build passed, dependencies were declared, and the
   outcome was wrong. This bears on D4, which is a committed decision, and is
   developed in `cli.md` open question 1.

---

## Candidate decisions

Entries this document would add to the log, once the shape settles:

- **Intent is a closed sum type of root requests.** Cascade expansion belongs
  to planning. *Cost to reverse: moderate* — it is the public surface, so
  widening it later is easy and narrowing it is not.
- **Sweep is a distinct constructor, not repetition.** *Cost to reverse: high* —
  gating, PR granularity and review-capacity limits all attach here.
- **Findings may propose intents, never execute them.** *Cost to reverse: low* —
  policy, and additive.
- **T4 splits into unstatable and unreachable.** *Cost to reverse: low* — a
  reporting distinction, though it changes what the findings sink can say.
- **`RefreshChecksums` carries its cause, and refuses to auto-promote without
  one.** *Cost to reverse: low* — additive, and the cause is derivable.
- **`modify` is admitted by rule, not by taste.** *Cost to reverse: moderate* —
  the rule is cheap to state now and expensive to impose on a verb that has
  already accumulated fields.
- **Toolchain-constraint propagation is out of scope.** *Cost to reverse: low* —
  it is a finding source, not an intent, and nothing depends on its absence.
