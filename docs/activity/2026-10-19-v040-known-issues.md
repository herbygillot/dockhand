# v0.4.0's known issues, back in the README

4a33af6d, the wording review, dropped the README's "Known issues in
v0.4.0" section, and the v0.3.1 banner, without its note saying so. The
list is back, each of its fourteen rows checked against rc5's 5897a828,
the commit v0.4.0 is tagged on.

Taken out, fixed before rc5:
- A Git-fetched port whose `git.url` changes while its tag stays read as
  the same source: 73b47517 (CX-2) compares a port's source with how and
  from where it fetches (assess.Policy 20).
- Dropping a patch upstream merged checked only the patch list, version,
  and revision: 489d1a64 (CX-3) holds every correction to the whole
  family.

Kept, as true at 5897a828: the other twelve. The 404 row now says that
mirrors not yet carrying a release are tried again (c3c12d28), and the
toolchain and serve rows drop their dated examples.

Added, each fixed on main after rc5 and so for v0.5.0: libxkbcommon's
kind of license hold (5b3a949d, 0680b7f8), a baseline taken for the
branch's check (2ec02f27), a configure.ac library named through a
variable said removed (0d4f4fe1), and `--baseline` offered for tests
master doesn't run (8939afe3). The banner stays out: the README on main
describes main, and the release notes, docs/releases/v0.4.0.md, say what
v0.4.0 is.
