# 2026-09-25: stealth updates, doing it by hand, and update --submit

This covers what was left of Design v3's `update` journeys (§6.2, §6.3, §6.5).

## What changed

- **Stealth updates (§6.5).**
  - `checksums` calls it a stealth update when an archive's contents changed under the same name, in a Portfile the branch hasn't changed since its base. It names that case, and shows each archive's checksums before and after, as a shortened sha256 and the size.
  - It then sets `dist_subdir ${name}/${version}_1`, the MacPorts guide's recipe, so mirrors keep both archives. A second stealth update counts up to `_2`.
  - `portfile.StealthDistSubdir` writes the line after the checksums, aligned with them, or counts up an existing one written that way.
  - A `dist_subdir` set any other way (`dist_subdir go`, say) is left alone. Dockhand says it wasn't set, and why, with the recipe.
  - The prepared tree is rewritten to match, so the plan's diff, the working files, and the edit's record all include the line.
  - It ends with "Inspect the source change before deciding whether it needs a revision bump": that decision stays yours. `--json` carries it all as `stealth`.
- **By hand (§6.3).**
  - When preparation can't edit a port by itself (`preparation.ErrUnsupported`), `update` says so and why, and what it kept. It then gives the hand path: edit the version with `dockhand edit <port>`, and `dockhand checksums <port>` fills in the rest.
  - `checksums` says the same for checksums it can't write.
- **`update --submit` (§6.2).**
  - After the edit, it tidies the branch and then runs `submit --check`: it checks the committed head and submits exactly that commit once the check passes. Each step previews itself the way the command does on its own.
  - The tidy uses tidy's own review. On a terminal it asks. Without a terminal, it applies only a plan made of dockhand's own edits, and never resolves an ambiguous one. That review is now shared, as `decideTidy`.
  - `--submit` refuses `--plan` and `--outdated`, and does nothing further when there was nothing to update.

## Decisions

- **What counts as a stealth update.** A different sha256, rmd160, or size under a distfile name the Portfile already declares, with the Portfile unchanged from the base. A version edited by hand first, the §6.3 path, also changes the checksums of a file whose name the Portfile doesn't spell out. Counting that would call every hand-edited update a stealth one. The branch-unchanged condition is cheap, needs no second evaluation of the base, and errs toward saying nothing.
- **Not yet: what changed inside the archive.** The design's "inside the archive: 1 file differs" needs the old archive and `diff --archive`, which isn't built. The old archive is, by definition, gone from upstream, and the MacPorts mirrors are where it survives.
- **No codes on the by-hand message.** The design shows one (`[hook-exec]`). Preparation's refusals carry reasons, not codes, and `explain` covers commit rules, not preparation. Inventing codes for the message alone would promise an explanation that doesn't exist.
