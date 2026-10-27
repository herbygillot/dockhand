# The survey rules as notices

2026-10-27, Then's item 11: three rules from the review survey the person
kept on 2026-10-02. Each is a commit rule's warning with an `explain`
entry. Each holds nothing and reads only the lines a change touched.
`tidy`, `submit`'s preview, and `review` say them with the other Portfile
findings (engine `portfileFindings`).

- **`restated`** (survey §1, 35 PRs): a line the change adds at a
  Portfile's top that sets a restatable option:
  - the options are name, version, homepage, master_sites, distname,
    use_configure, use_parallel_build, and python.pep517;
  - or the line is a dependency of one `port:` spec.

  It is said where the Portfile evaluates the same without the line,
  which is the counterfactual: the option's value is unchanged, or the
  dependency is still declared in its phase.
  - commitrules `CheckRestated` takes an `Evaluate`.
  - The engine's `evaluateText` evaluates the tree with that Portfile
    replaced (git `EditTree`), on this Mac.
  - portfile `AddedLines` gives each added one-line command with the
    Portfile without it.
- **`path-style`** (survey §5, 8 PRs): a `port:X` dependency the change
  adds where another port can satisfy it. It offers
  `path:<file>:X`, naming the file for pkgconfig (bin/pkg-config) and
  OpenBLAS (lib/libopenblas.dylib).
  - "Can satisfy" is the counterfactual: the candidate, X-devel or the
    survey's pkgconf for pkgconfig, evaluates with a `conflicts` that
    names X (engine `providers`).
  - `conflicts`, `use_parallel_build`, and `python.pep517` joined
    `macports.ReadOptions`.
  - portfile `AddedPortDependencies` gives the dependencies added.
- **`ticket-trailer`** (survey §3, 6 PRs, #31382's "The Trac ticket should
  not be in the short summary"): a Trac ticket's URL in the subject, or in
  a body sentence rather than a trailer such as Closes: or See:, is
  offered as "Closes: https://trac.macports.org/ticket/N (update's
  --closes N writes it)". A bare #N stays `ticket-url`'s.
- Not yet: the measure over the Mac's tree, of these and I7's scan, with
  counts and false positives, last in this item.
- Tests: TestALineAPortGroupAlreadySetsIsSaid,
  TestADependencyAnotherPortProvidesIsOfferedPathStyle,
  TestATicketOutsideATrailerIsOfferedAsOne,
  TestAProviderIsAPortWhoseConflictsNameIt,
  TestTheLinesAChangeAddsAreEachWithoutIt, and TestEveryRuleIsExplained.
