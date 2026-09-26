# 2026-09-25: the oracle's design, settled

The [oracle scope](../oracle.md) left five decisions for the Mac session.
They were settled in conversation with the person the same day, and are
now written into the scope as its design, replacing the open decisions.

## What changed in `docs/oracle.md`

- **A design section:**
  - facts by domain and source;
  - a clean context as CI's two evaluations;
  - how registry questions are handled at the checksum bump;
  - `with-deps` derived in two passes rather than installed;
  - where derived answers are cached and why they don't go stale;
  - what the registry is asked, with numbers;
  - isolation as best effort, with no `sandbox-exec` and no custom
    `tclsh`.
- **The phase table** marks phase 1 done. Phase 2 gains a first
  comparison with a fresh Tart guest, and phase 4 records registry
  subjects and sizes `with-deps`. A conditional phase 4b builds it.
- **Decisions** replace the open decisions: 1–4 settled, 5 deferred.
  Two questions stay open: whose variants `with-deps` installs, and
  whether a Portfile whose fetch depends on installed state is raised
  with its maintainer.

## The registry's subjects

Read from the 2026-09-23 host inventory
(`~/.dockhand/surveys/2026-09-23-host-inventory/`): 25,040 records of
parse-time `registry_*` and `_portnameactive` calls. One more matched
only because R-registry's path contains the word, and was left out.

- **922 asking subports in 231 Portfiles.** `qt5_version_info` accounts
  for 836 of the subports, in 186 Portfiles. After it come Portfiles
  directly (57 in 14), `active_variants` (40 in 16), and `elisp` (17 in
  17).
- **60 subjects recorded, a lower bound.** The inventory kept one record
  per worker for each command, not for each argument. `qt5_version_info`
  recorded only qt56-qtbase, though its PortGroup fixes nine subjects,
  and `elisp` recorded only the first of the three Emacs binaries it asks
  about.
- **The port index lists the variants a port offers, not its defaults.**
  Checked on hdf5's entry in a staged index. So `with-deps` resolves
  default variants by evaluating the subject port.

Phase 4's ledger records each question's subject, so later surveys
don't depend on this extraction.
