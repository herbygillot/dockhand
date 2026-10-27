# An undeclared link holds an unattended submission

2026-10-27, at the person's "Hold it" on the prioritization thread's card
(12:53Z). Field testing's rustup linked liblzma with no depends_lib on xz.

- `link-undeclared` (engine `linkHolds`) makes each of the check's
  undeclared links (`UndeclaredLinks`) a concern for the port that links
  it, with the linked port as its subject. The concern reads "jq links
  liblzma.5.dylib from xz, which it doesn't declare in depends_lib; review
  is required before automatic submission".
- It is on the submit plan (`SubmitPlan.Undeclared`, among `Concerns`), so
  submit's preview says it, and serve's and bump's submissions hold on it.
- It is one of `HoldRules`, so `holds.allow` may name it. When allowed, it
  is still said.
- A declared link, a build whose links weren't read, and a clean scan hold
  nothing.
- docs/usage.md's `holds.allow` lists it.
- Test: TestAnUndeclaredLinkHoldsAnUnattendedSubmission.
