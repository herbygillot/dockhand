# 2026-09-29: the description begins "Submitted by dockhand"

The person asked for dockhand's line in a pull request's description to move. It had been the last line, "Submitted by **[dockhand](https://github.com/herbygillot/dockhand)** (ver. v3.1.0)". Now:
- the description begins "Submitted by [dockhand](https://github.com/herbygillot/dockhand)", with no version, before its Description heading (`submittedBy`);
- its last line lists dockhand's version: "- [dockhand](https://github.com/herbygillot/dockhand) ver. v3.1.0", or dockhand alone when the build doesn't know its version (`signature`).

**Submitting again.** A re-submit rewrites everything from Tested on down while it's still exactly what dockhand last wrote, and the last line is part of that. A description written before this change would lose the line naming dockhand, with no first line to take its place. So where that part is rewritten, a description dockhand wrote before its first line named dockhand gains the first line (`submittedFirst`). It's left alone where:
- a person edited Tested on, so the old last line stays;
- a person took the first line out of one written since;
- a person began the description otherwise.

**Rendering.** The last line is a Markdown list item right after the Verification checklist. Where nothing stood between them, `[skip notification]` being the only thing that can, CommonMark continued the checklist's list with it: the line became the checklist's last item, and the checklist took a loose list's spacing. The person agreed to a fix that keeps the line as they asked for it. A comment GitHub doesn't show, `<!-- dockhand -->`, now stands between them and ends the list. Rendering both with pandoc's CommonMark reader showed each: one loose list of three without it, and the checklist tight with the line a list of its own with it. Pandoc's GFM mode splits a task list from other items by itself, which CommonMark doesn't.

Tests: `TestTheSignatureNeedsNoVersion` and `TestAnOlderDescriptionGainsItsFirstLine`, in the engine, and the submit test's description, which begins and ends as it now does, with the comment before its last line. Eight mutations each fail a test.
