# status shows the releases its checks planned

Field testing's exiv2 session, via the person (2026-10-20 18:37Z): `dockhand
status <branch>` showed a "macOS 26 · not run" column for a branch only
ever checked on 12 and 27, from a Mac on 26.

The evidence a branch's status reads (`evidenceNow`) adds the
environments build.default's profile requires that no check planned
(`evidence.Plan`), so submit can say which releases no check covered.
Their cells are all not run. status's branch table showed them as a
result. `Evidence.Checked` is `Shown` without them: the environments some
check of the files planned. status shows those. check's report and the
pull request keep `Shown`.

The report's other two oddities follow from the same reading. The
columns ran 27, then 26, because the required release is added after the
planned ones. macOS 12 was missing because the table is the evidence of
the files' finished checks, and the check running on 12 and 27 hadn't
finished. Its environments show once it does.
