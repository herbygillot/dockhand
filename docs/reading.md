# Reading Portfiles

**Status: provisional.** Supersedes the "Reading Tcl" section of `one-pager.md`
on one point of naming and one of scope. D2 and D3 are untouched by anything
here — this narrows what gets built, not what was decided.

---

## The fork is not parse versus execute

D3 forbids an *embedded* interpreter. It does not forbid execution, and D2's
first half **is** execution: Portfiles are evaluated through `port-tclsh`, with
the real MacPorts port API and every PortGroup actually running. That is not a
degraded read. It is more complete than anything that could be built in-house,
which is precisely why it is the oracle.

So the question is never whether to execute Tcl. It is:

> Given that evaluation already yields correct values, what is text parsing for?

**One thing.** Evaluation reports that `version` is `0.4.0`. It cannot report
that `0.4.0` occupies bytes 4231–4236. Values come from execution; locations can
only come from text. That is the whole of D2's asymmetry, and nothing below
weakens it.

---

## Where the version actually lives

Measured across the ports tree, `2026-08-24`. Reproduce with a `grep -lE` per
row over every `Portfile`; nothing here is worth persisting, only re-deriving.

| Carrier | Ports | Share |
|---|---|---|
| bare `version`, literal | 7,892 | 39% |
| `R` PortGroup (CRAN) | 5,114 | 26% |
| `github.setup` | 3,114 | 16% |
| `perl5.setup` | 2,011 | 10% |
| `go.setup` | 589 | 3% |
| `ruby.setup` | 485 | 2% |
| bare `version`, interpolated | 350 | 2% |
| gitlab / octave / php / other | ~485 | 2% |

Total Portfiles: **20,033**. (The "35,000-file test corpus" quoted elsewhere is
the whole tree, patchfiles and `files/` included, not the Portfile count.)

Three things follow, and all of them are load-bearing.

**Only 39% of ports carry the version in a `version` line.** For the majority it
is a *positional argument to a portgroup setup command*:

```
go.setup            github.com/robpike/ivy 0.4.0 v
```

**T0 is not one recognizer, it is roughly six** — one per carrier, each knowing
which argument position holds the version. The recognizer set is therefore more
coupled to PortGroups than the documents imply, and PortGroup drift lands on it
directly. That is an argument for keeping the coupling confined, not for
avoiding it.

**The empirical priority order is not the intuitive one.** `version` plus `R`
plus `github.setup` covers 81% of the tree. Go is 3% — and the entire field
evidence sample is Go. Designing to that sample would optimise for one port in
thirty-three.

---

## The artifact is a four-level CST, because Tcl has no higher grammar

*(This section originally argued the artifact down to a "command-and-word
lexer, not a CST". Half of that was right — the exclusions. The other half was
terminological, and design discussion reversed it: the consumers accumulated —
recognizers, lazy brace recursion, proc recognition for PortGroups, placement,
comment adjacency — until the artifact was a CST in everything but name. The
resolution below is what "proper CST" turns out to mean for Tcl, and it is
smaller than the argument that produced it.)*

The operation the recognizers need is: *find the command named `go.setup`;
take word 3; return its byte span.* Doing that correctly requires Tcl's
lexical rules — word boundaries, brace nesting, quoting, bracket substitution,
line continuations, comments. A regex gets
`{@alice example.com:alice} openmaintainer` wrong and gets
`${my_name}-${github.version}` wrong.

The decisive structural fact: **Tcl has no higher grammar.** `if`, `foreach`,
`proc`, `subport` are not syntax — they are commands. There are no statement
nodes, no expression trees, no declaration forms. The *complete* concrete
syntax tree of any Tcl script is four levels:

```
Script   → list of Commands
Command  → list of Words        (+ comments, separators)
Word     → list of Segments     (literal | ${var} | [cmd-sub] | brace | quote)
Segment  → spans; [cmd-sub] recurses as a Script
```

A "proper CST" for Tcl is this, with full fidelity to the dodekalogue's rules
and spans at every level. Anything more is not a CST of Tcl; it is a guess
about semantics, which D2 and D3 already forbid.

The fourth level is not gold-plating. The computed-version tail —
`version 20251208-4.2.1-[string range ${github.version} 0 7]` — requires
knowing a word's internal composition: which byte ranges are literal, which
are `${var}`, which are `[...]`. That segmentation is the input to
correspondence analysis, to the constrained evaluator's locator hints, and to
differential probing. The level that separates "lexer" from "CST" was never
optional.

### Brace content is opaque in the tree, reinterpreted through lenses

A brace-word's meaning belongs to the command consuming it — `if {$x} {...}`
holds a script, `maintainers {@alice example.com}` holds a list,
`description {Some text}` holds a string. No grammar can know which, so the
CST stores every brace body as an opaque span and consumers apply a lens on
demand:

- **Script lens** — re-parse as commands. Subport blocks, the
  `if {${name} eq ${subport}}` guard around `replaced_by`.
- **List lens** — split as a Tcl list, span-preserving. `maintainers`,
  `categories`; the old `tclsyntax.Words` logic, upgraded to keep offsets.
- **No expr lens, ever.** `{$x < 1}` is a third sublanguage whose meaning is
  semantics. It is not parsed, and nothing in the design needs it.

Every lens produces spans into the same buffer, so the concatenation
invariant survives reinterpretation: one tree, plural readings, all
verifiable.

### Testing the parser

Two harnesses matter more than any implementation choice:

1. **The concatenation invariant.** The spans of a parse, concatenated,
   reproduce the input byte-for-byte — across all 20,033 Portfiles and every
   PortGroup. D2's minimality invariant, applied to the parser itself.
2. **Differential testing against Tcl.** The oracle-is-never-dockhand
   principle applies here too: `port-tclsh` can be asked what a list splits
   into and whether a script is `info complete`, and the CST's boundaries are
   cross-checked against real Tcl's answers over the fixture suite. The parser
   is verified the way edits are — against the authority.

**tree-sitter: harness, not foundation.** Evaluated and declined for
production: Tcl's brace context-sensitivity is exactly what a context-free
grammar cannot decide (the lens decision lands back in our code regardless),
and edit-grade byte fidelity exceeds community-grammar quality — an
editor-highlighting miss is cosmetic, a span miss here corrupts a Portfile.
Two marginal uses stand: *disagreement mining* (parse the corpus with both
parsers; every boundary disagreement is a cheap bug lead for either side) and
*throwaway prototyping* of recognizer censuses before the CST exists.

---

## The unspecified hard part: value-to-span correspondence

Neither the CST nor the evaluator is the real open problem. The gap is getting
from *a value evaluation reported* to *the span that produced it*.

For the easy majority this is a search. Evaluation says `0.4.0`; the text holds
`0.4.0` as word 3 of `go.setup`; done.

For computed versions it is not a search at all. From `devel/cmake-devel`, real
and current:

```tcl
set my_name         cmake
github.setup        Kitware CMake 485f11a780435eb6495b79227d3237383778ac3e
version             20251208-4.2.1-[string range ${github.version} 0 7]
```

The evaluated version is `20251208-4.2.1-485f11a7`, and **that string does not
appear anywhere in the file**. No search finds it. Bumping this port means
editing the SHA in `github.setup`, the date, and `4.2.1` — three spans, none
locatable from the value, because locating them requires understanding how the
value was *constructed* rather than what it *is*.

This is the case that defeats "evaluate for values, search for spans", and it is
the genuine argument for a constrained evaluator — not convenience, but the only
proposal that addresses construction.

---

## Three ways to handle the computed tail

**Decline it.** Refusal is a feature, and the cost is smaller than it looks:
350 ports (2%) have an interpolated `version` line, plus some unmeasured
fraction of the portgroup carriers. `classify` establishes the real number in
one sweep with no evaluator written.

**A constrained evaluator** — D3's answer. `set`, interpolation, concatenation,
as a locator hint only, checked by the authoritative re-read. Keeping it
non-authoritative is not caution but necessity: a correct one is impossible.
`[string range ...]`, `[exec]` and arbitrary procs are all in play, and
`cmake-devel` uses the first inside the version line itself.

**Differential probing**, not previously considered. Rather than evaluating
`${major}.2` in-house, mutate a candidate span, re-evaluate through
`port-tclsh`, and observe whether the field changed as predicted. This locates
spans using the authoritative oracle instead of an in-house imitation, so search
and verification collapse into one operation and a located span is verified by
construction. It costs N evaluations per port, which is exactly the cost D3's
persistent `port-tclsh` helper exists to amortise. It identifies which spans are
load-bearing; it does not decide what to write in them, so it complements the
evaluator rather than replacing it.

---

## Writing: three edit shapes, not one

D2 says edits are byte-span replacements. That holds, but it describes one
shape and the intents need three.

**1. Replacement.** A span exists; swap its bytes. `Bump`,
`RefreshChecksums`, `modify`. D2's minimality invariant applies exactly.

**2. Anchored insertion.** No span exists; create one at a position defined
relative to a span that does. `BumpRevision` on the 17.7% of Portfiles with no
`revision` line.

**3. Reduction to canonical form.** Delete nearly everything and emit a
template. `Obsolete`.

### Obsoleting is shape 3, and it needs almost no parsing

Real commits, not inference:

| Commit | Insertions | Deletions |
|---|---|---|
| `libjpeg-turbo-devel` → obsolete | 8 | 82 |
| `ocaml-mode.el` → obsolete | 8 | 42 |
| `py-pymacs` → obsolete | 6 | 37 |

The median obsolete port in the tree is 17 lines and 58% are under 25. The
output is a template — `PortSystem`, `PortGroup obsolete 1.0`, `name`,
`version`, `revision`, `categories`, `replaced_by`, optionally
`obsolete.note` — and everything else is deleted, because the portgroup itself
nulls `depends_*`, `patchfiles`, `distfiles` and `archive_sites`, sets
`known_fail yes`, and errors in `pre-configure`.

So `Obsolete` needs four *values*, which evaluation already supplies, and no
spans at all. The exception is the subport-scoped minority — 52 of the 108
obsolete ports have subport blocks, and `libftdi`-shaped cases insert a guarded
block into a file that must survive. Those are shape 2, and they are the harder
half of a much smaller problem.

*Consequence, pending confirmation.* Diff minimality is **intent-relative**.
For `Obsolete` the diff is maximal and maximal is correct, so the invariant
belongs to *port-preserving* intents; obsoleting replaces a port with a
tombstone. The right test there is conformance to the template — equally hard,
equally checkable, a different property.

### Placement is observed, not owned

Across the 16,188 Portfiles carrying both a version carrier and a `revision`
line:

| Placement of `revision` | Share |
|---|---|
| Immediately after the version carrier | **79.5%** |
| Within three lines after it | 94.3% |
| Anywhere before it | 0.3% |

98.1% of all Portfiles have a recognisable version carrier; the 384 that do not
are a 1.9% decline. Alignment sits at column 21 in 87.4% of cases, so the rule
must take alignment **from the anchor line** rather than from a constant.

This matters because placement first looked like a formatting semantic dockhand
would have to invent, which "own the edit, never own the semantics" forbids. It
is not invented. It is a measured regularity in the tree, reproduced — and it is
testable in a way little else here is, because the rule must reproduce actual
placement across those 16,188 files and the pass rate is a number available on
day one, before a single Portfile is written.

*One economy.* `revision`'s anchor is the version carrier — the same span `Bump`
must already locate. Placement for the insertion case is therefore nearly free,
and belongs inside the carrier recognizer rather than in separate machinery.

### The CST is a locator for text that exists

Insertion does not strain D2: it is a zero-length span replacement, so the write
model is unchanged. What is missing is not a write primitive but a second
locator.

> The CST answers **where is X**. Insertion needs **where should X go**.

Both are pure functions over the same parse and both yield spans, but the first
is syntactic and exact while the second is conventional and probabilistic — it
has a pass rate, not a proof. Keeping them separate confines that uncertainty to
one named component with a measured accuracy instead of smearing it into the
parser.

`Placement = fn(parse, field) -> Option<Anchor>`, per-field, measured, declining
when the anchor is absent.

### Comments are data, not noise *(PR evidence)*

Three independent conventions live in comments: obsolete stubs carry removal
dates, stealth updates carry remove-at-next-update notes beside a temporary
`dist_subdir`, and libraries carry dependent-revbump instructions above their
`version` line — the last of which failed silently and produced a nine-port
repair PR (`pr-evidence.md` §6–7).

The CST already tokenises comments as part of the command level. This gives them a consumer:
**a plan should surface comments adjacent to its edited spans.** When `Bump`
touches a `version` carrier, any comment within a few lines mentioning
`revision`, `update`, `bump`, `remove`, or a Trac URL belongs in the plan for
the human to read. dockhand never *interprets* the comment — that would be
owning semantics in the least-parseable corner of the file — but a tool that
silently discards them re-creates the dav1d miss with machinery. This is a
parser-level feature with no evaluation cost, and it composes with the dated
conventions dockhand may itself write (`Obsolete`, stealth `dist_subdir`).

### The hazard insertion creates, and its resolution

Replacement inherits scope; insertion chooses it. Inserting `revision` at top
level in a subport-bearing file silently moves every subport, and checking only
the target subport stays green precisely when the edit is wrong.

This is settled by **D13**: fidelity enumerates every subport, always. See the
decision log for what follows — `Metadata` keyed by *(subport, variant set)*,
`Diff` handling membership changes, and `Plan` predicting its blast radius.

---

## Recommended order

1. Build the four-level CST with spans and the two lenses.
2. Use search-based correspondence.
3. **Decline the computed tail on day one.**
4. Run `classify` across the tree and measure the actual decline rate.
5. Only then choose between declining permanently, the evaluator, and probing.

Every argument for the evaluator currently on record — including the
`cmake-devel` one above — rests on that port being representative, which is an
anecdote. One sweep replaces it with a number. The design's own principle about
not guessing applies to its own construction.

---

## Open questions

1. ~~Does the lexer need all of Tcl's lexical rules?~~ *Answered by the
   four-level resolution:* all of them — the CST is full-fidelity to the
   dodekalogue by definition, and the fixtures (plus ad-hoc full-tree runs)
   test whether any rule is dead in practice rather than whether to implement
   it.

2. **What is the decline rate?** Unknown, and it decides item 5 above.

3. ~~**Does insertion have a home?**~~ *Answered above.* Insertion is anchored
   replacement with a measured placement rule; obsoleting turned out not to be
   insertion at all. What remains open is confirmation of the two consequences
   marked *pending* — that minimality is intent-relative, and that placement is
   observed rather than owned.

4. **Does the subport-scoped `obsolete` case share the placement machinery?**
   Inserting a guarded `if {${name} eq ${subport}} { ... }` block is a larger
   insertion than a single line, and the tree offers 52 examples to measure
   against rather than 16,188.
