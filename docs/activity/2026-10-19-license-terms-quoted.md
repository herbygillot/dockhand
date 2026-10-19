# A license hold names its licenses and quotes the added terms

The rest of the roadmap's v0.5.0 part 1 item 1, libxkbcommon's license
hold, after prioritization's ruling (cbe03130): the hold stands, since
1.13.2's LICENSE adds Anton Zinoviev's terms for its test data, which no
known license has, and a LICENSE's headings don't say what a port
installs. Its wording was wrong: "now reads as no license that's known",
where the licenses found are the ones 1.7.0 had and only their coverage
fell.

- `project.LicenseText.Terms` are the first line of each paragraph no
  license match covers that comes after a copyright line or a label: the
  terms a file grants beside its licenses, apart from the prose a file of
  several opens with (1.13.2 reworded that preamble, so "the first new
  uncovered line" quoted it).
- `coveredWords` finds them in the same pass that counts the words.
- `LicenseText.String` names three or more licenses with commas: "MIT,
  HPND and HPND-sell-variant".
- sourcecompare's `licenseWords` has a case for licenses that are the same
  but no longer known: "is still MIT, HPND and HPND-sell-variant by what
  its text matches, and adds terms no license that's known has: "This
  software may be used, modified, copied, distributed, and sold"", through
  `addedTerms`, the first of the new text's terms the old text didn't
  have. Without one, it says how much of the text they cover.
- Tests: `TestALicenseThatAddsTermsQuotesThem` (sourcecompare, from the two
  real LICENSE files) and the Terms assertions in
  `TestLabelsAndCopyrightLinesAreNoLicensesWords`.
