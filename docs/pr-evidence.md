# PR evidence

**Status: evidence, not proposal.** Companion to `field-evidence.md`, which
recorded one maintainer's practice from the inside. This records the other
side: what `macports/macports-ports` reviewers actually say when work arrives
as a pull request. Gathered 2026-08-24 from merged and closed PRs; every claim
carries a PR number. The sample is small and skewed toward PRs that *drew*
comments — silent merges are invisible here by construction.

---

## 1. Multi-port PRs are routine when they are one logical change

The cohort question — is a PR spanning several maintainers' ports antisocial? —
is answered by observed practice: **no, when the ports move for one reason.**

- [#33883](https://github.com/macports/macports-ports/pull/33883) — "dav1d: Rev
  bump dependents" touches nine ports across at least four maintainers
  (`ffmpeg4/6/7/8`, `libavif`, `xine-lib`, `MPlayer`, `mplayer-devel`). Merged
  without granularity objections.
- [#33155](https://github.com/macports/macports-ports/pull/33155) — four
  `*-binutils` ports updated in one PR. Merged with zero comments.
- [#32636](https://github.com/macports/macports-ports/pull/32636) — reviewer
  *requires* the cross-port pairing: "The `revision` increase for the `toxic`
  port needs to be done at the same time, so please add a separate commit doing
  that to this PR."

The unit reviewers police is the **commit**, not the PR.

## 2. Commit granularity has observable rules

From [#33883](https://github.com/macports/macports-ports/pull/33883)
(mascguy): "For a group of related rev-bumps like this, it's worth grouping
them all in a single commit. And so I squashed everything into one." From
[#32636](https://github.com/macports/macports-ports/pull/32636) (reneeotten):
the dependent revbump should be "a separate commit" from the version bump
itself. Synthesis, as convention rather than law:

- One logical change per commit; a change and its dependent revbumps are
  *different* logical changes sharing a PR.
- A grouped revbump of N dependents is **one** commit, even at N=9 across
  several maintainers.
- Squash-at-merge on request is normal; two commits touching the same port for
  the same reason draw a squash request
  ([#33883](https://github.com/macports/macports-ports/pull/33883)).

## 3. Commit message and title formats are enforced, and tooling reads titles

- "The commit message doesn't follow the guidelines. It should be 'portname:
  short description'" — review verdict, sole blocking issue on
  [#33024](https://github.com/macports/macports-ports/pull/33024).
- Trac cross-references are requested as trailers in the commit body, in the
  literal form `Closes: https://trac.macports.org/ticket/NNNNN`
  ([#34087](https://github.com/macports/macports-ports/pull/34087), where the
  reviewer spelled out the exact syntax when the contributor guessed wrong).
- The PR template **auto-detects** the change type from the title — "update
  (title contains ': U(u)pdate to')". Title conventions are load-bearing for
  the project's own tooling, not just style.

## 4. The revision rule, as a reviewer states it

[#33925](https://github.com/macports/macports-ports/pull/33925): "this changes
the installed files and thus should increase the 'revision'."

That is the entire decision procedure in one sentence. The five observed rules
in `field-evidence.md` §9 are corollaries of it: *does the installed result
differ?* Version changed → reset (new files anyway); build fixed at same
version → increment (files differ); whitespace or gating annotation → nothing
(files identical); byte-identical distfile relocation → nothing.

Also enforced: "the 'revision' should be reset to zero when updating the
version" ([#32636](https://github.com/macports/macports-ports/pull/32636)) —
the cascade rule, stated in review. And on new ports, reviewers ask for an
explicit `revision 0` line
([#33865](https://github.com/macports/macports-ports/pull/33865), three times
in one PR).

## 5. The stealth-update recipe, from a real merged PR

[#29118](https://github.com/macports/macports-ports/pull/29118) (`k9s`,
0.50.9) is the complete idiom in one diff:

```diff
-revision            0
+revision            1
+
+# 0.50.9 had a stealth update, remove again with next update -- https://trac.macports.org/ticket/72837
+dist_subdir         ${name}/${version}_1
```

plus refreshed checksums. Four parts: **checksums + revision increment +
`dist_subdir` versioning + a dated removal comment**, with a Trac ticket as
provenance. The `dist_subdir` step exists so users holding the old distfile in
cache do not hit checksum mismatches — and it is *temporary*, to be removed at
the next real update, which the comment records.

## 6. Missed dependent revbumps are a real, recurring failure

[#33883](https://github.com/macports/macports-ports/pull/33883) exists because
nine dependents "were missed in the original dav1d update" — a follow-up PR
repairing an incomplete cascade. And the coordination mechanism that failed was
a **comment in the Portfile**: "Please increase the revision of libheif,
ffmpeg and ffmpeg-devel whenever dav1d's version is updated" — which the PR
discussion then flags as itself stale ("I am suspicious that this comment…").

## 7. Conventions keep living in comments

Third independent instance of machine-invisible convention carried in comments:
obsolete stubs carry removal dates (`pev`), stealth updates carry
remove-at-next-update notes (`k9s`), dependent-revbump instructions sit above
`version` lines (`dav1d`). Comments are where the tree keeps its TODO state.

## 8. Reviewers verify claims, and truthful minimal testing is accepted

- "did you verify this?" — inline, on an unsubstantiated year change
  ([#32636](https://github.com/macports/macports-ports/pull/32636)).
- "Tested on: CI only." — accepted without comment in a merged nine-port PR
  ([#33883](https://github.com/macports/macports-ports/pull/33883)).

Candour costs nothing; unverified assertions draw questions.

## 9. A maintainer's bot, on their own port, merges without friction

[#31661](https://github.com/macports/macports-ports/pull/31661) — "diffscribe:
update to 0.3.0", authored by `nickawilliams-bot`, body opening "Automated
update from [diffscribe] release v0.3.0", full template including Tested-on.
Merged. The tree already contains exactly the workflow D9/D10 describe, run by
a maintainer against their own port, and nobody blinked.

## 10. Placement conventions are socially enforced

[#32636](https://github.com/macports/macports-ports/pull/32636)
(barracuda156): "keep all important informative data like port name and
revision immediately next to github.setup line, while move non-informative
technicalities like tarball.from and extract.mkdir down to where they logically
belong." The measured 79.5% revision-follows-carrier regularity is not an
accident of history; reviewers actively push files toward it.

## 11. Review feedback is dominated by semantics the tree already encodes

Sampled blocking comments cluster on: missing revision handling (§4), redundant
defaults ("this is the default already set by the python PortGroup",
[#33865](https://github.com/macports/macports-ports/pull/33865)), preferred
distfile sources ("upstream provides 'releases' which are preferred",
[#32636](https://github.com/macports/macports-ports/pull/32636)), and commit
hygiene (§2–3). Almost none of it is about the *edit* being wrong; it is about
the edit being *incomplete against convention*. The labour reviewers spend is
exactly the labour a convention-aware tool would remove.

---

## What this tends to support or contradict

- **Contradicts** `cli.md`'s "one PR per port is the norm" and the cohort
  conflict claim in `intents.md` — the norm is one PR per logical change (§1).
- **Supports** the correction already recorded there, now with evidence rather
  than suspicion.
- **Supports** D9/D10 directly (§9), including the provenance-candour bet (§8).
- **Extends** `RefreshChecksums`: the stealth cascade is four parts, not two
  (§5), and `dist_subdir` was absent from every dockhand document until now.
- **Simplifies** `BumpRevision`: one criterion — do the installed files change —
  with the field-evidence rules as corollaries (§4). The criterion is
  *measurable* (destroot manifest diff), which moves the decision from pure
  judgement toward evidence.
- **Adds a requirement** nothing in the design currently owns: `promote` must
  produce a **commit plan** — grouping, ordering, messages, trailers — because
  the commit, not the diff, is the unit reviewers police (§2–3).
- **Adds a finding source**: comments near edited spans (§6–7). A lexer that
  already tokenises comments can surface them; a tool that ignores them will
  re-create the dav1d miss.
