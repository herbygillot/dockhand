# A patch the Portfile names and Git doesn't track

The roadmap's item found by field testing at 5897a828. On net-snmp
5.9.5.2's branch, files/patch-swinst_darwin-declaration.diff was written
and named by `patchfiles-append`, but never `git add`ed. `tidy` committed
the Portfile without it and then said nothing was uncommitted; `ship`'s
check, check-469, fetched the patch from the mirrors and 404'd on both
releases.

- `portfile.PatchfileNames` reads the literal names of `patchfiles`,
  `-append`, and `-prepend` in any body, a platform's or a variant's too.
- `tidy` takes in the untracked files a changed port's `patchfiles` names,
  as `git add` would, in the commit for that port's directory
  (`tidyWorkingFiles`, engine/tidy.go). The plan and its application each
  compute it, so a patch added, removed, or edited between them makes the
  plan stale. Any other untracked file in a changed port's directory is
  listed as left out, and a named file Git ignores is said, not taken.
  The plan carries them as `Taken`, `LeftOut`, and `Ignored`, and
  `--json` as `taken`, `left_out`, and `ignored`. A kept plan with a file
  left out says "every tracked change is committed", never "nothing is
  uncommitted".
- The check's plan refuses a changed port whose `patchfiles` names an
  untracked file the capture doesn't hold, before any VM starts
  (`patchesHeld`, engine/plan.go): "net-snmp's patchfiles names
  files/patch-swinst_darwin-declaration.diff, which isn't tracked: git add
  it, or check --include it". `ProposeCheck` is check's, ship's, and
  serve's, so each meets it; serve and an unattended ship hold on it.
- A named file Git ignores is a file the capture doesn't hold too, and is
  refused the same way, with other advice, since `--include` refuses an
  ignored file: "jq's patchfiles names files/ignored.diff, which Git
  ignores, so neither a commit nor a check carries it; git check-ignore -v
  files/ignored.diff names the rule" (`ignoredPatches`, engine/capture.go,
  which tidy's report shares). Prioritization's call, 2026-10-20: it
  finishes this item rather than making a new one.
- `check -b <branch> --include <file>` on a branch with nothing
  uncommitted captured the head, which refused the include; files to
  include now make the automatic capture the working files.
- README and the v0.4.0 release notes list it as a known issue: `git add`
  a new patch before `tidy`.

Replayed in scratch on a copy of the branch as it was (19e17a35 with the
patch untracked, master at dd12c848): `tidy --yes` made one commit whose
tree is 74c68a50's, the fixed branch's, patch included. Undone, `check`
and `check --plan` refused with the wording above; `ship` held for the
tidy plan's review, which takes the patch in, so its check never runs
without it.
