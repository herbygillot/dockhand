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
- **The measure over the Mac's tree** (macports-ports at 324108c8883,
  20,231 Portfiles). `TestTheNoticesOverATree` in tools/survey runs only
  where `MACPORTS_TREE` names a tree, and prints each match. Every
  Portfile is read as a new port's, so every line counts as one a change
  adds: the most each rule could say of the tree.
  - **I7's scan**: 49 Portfiles. One false positive: cpmtools' "New
    versions of this software do not update the version number". The
    phrase reads right everywhere else (osxfuse, llvm-14, dav1d, ffmpeg,
    curl, py-spaCy's five, doxygen's two), so it stands.
  - **restated**, the evaluated rule, over 600 Portfiles spaced through
    the tree with port-tclsh: 19 lines. They are use_configure 4, name 4,
    distname 4, version 2, use_parallel_build 2, homepage 2, and
    depends_build 1. Each read true: a name or version github.setup sets,
    `distname ${name}-${version}` (base's default), `use_configure yes`,
    and aqbanking6's port:pkgconfig, which a PortGroup adds. No false
    positive was found.
  - **path-style**, first run: 1,889, and libffi's 136 and libheif's 19
    read wrong. libffi-devel conflicts with libffi, but the tree writes
    all 1,562 of its libffi dependencies as port:libffi, and libheif the
    same. So the rule was narrowed: it is said only where the tree itself
    writes the port path-style, and it offers the form the tree writes
    most (the engine's `treeSpec`, from git grep). That leaves 1,689,
    which are pkgconfig 1,462, bash 67, jemalloc 17, zmq 10, libplist 10,
    cmake 8, and a long tail; none read wrong. These are whole-tree
    counts. A change says only the dependencies it adds.
  - **ticket-trailer**, first run over the last 20,000 commits: 4 of
    2,000, then 11 of 20,000. Two narrowings followed:
    - a line written as a trailer, in a list or without its colon,
      "* Fixes: …", "- See …", or "Fixes …", is one;
    - a ticket a trailer cites too, as commons-lang3's sentence beside its
      See: lines, isn't said.

    That leaves 10 of 20,000, each a ticket in a body sentence or alone
    on a line, which the notice offers as Closes: or See:.
- Tests: TestALineAPortGroupAlreadySetsIsSaid,
  TestADependencyAnotherPortProvidesIsOfferedPathStyle,
  TestATicketOutsideATrailerIsOfferedAsOne,
  TestAProviderIsAPortWhoseConflictsNameIt,
  TestTheLinesAChangeAddsAreEachWithoutIt, and TestEveryRuleIsExplained.
