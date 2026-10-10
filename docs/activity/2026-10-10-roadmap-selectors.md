# Roadmap: bump's tidy-plan refusal to v0.4.0, two selector bugs to v0.3.1

At the person's "Go ahead" (2026-10-10, from the prioritization thread):

- bump stopping on a tidy plan that isn't unambiguous moves from v0.3.1 to
  v0.4.0, where bump becomes `ship <port> --yes` on serve's preparation path.
- Two bugs from the command-line UX second look's §1 join v0.3.1's fixes,
  since `ship -p` and `-b` build on them: a branch's intended ports are
  recorded, so `start --port` and an authoring verb's auto-start agree with
  `-p`; and `-b` takes only an existing branch, with `--new=<name>` starting
  one.
- The majors hold stays built in serve's submit hold list, for v0.4.0 to reuse.
