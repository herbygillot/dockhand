# 2026-09-30: the description's first line names dockhand in bold

The person asked for the pull request description's leader line to set the `dockhand` link in bold. It was "Submitted by [dockhand](https://github.com/herbygillot/dockhand)". It's now "Submitted by **[dockhand](https://github.com/herbygillot/dockhand)**" (`submittedBy`), which renders as "Submitted by **dockhand**", still a link. The last line, with dockhand's version, is unchanged.

## Pull requests already open

The leader line sits above the Description section. When submitting again refreshes a description, it leaves that part as it is, so a pull request opened before this change would have kept the plain line. `submittedFirst` gives such a description the bold line, but only where it still begins with the plain line and dockhand last wrote it that way. A description a person began otherwise keeps what they wrote, as before.

## Tests

- `TestTheSignatureNeedsNoVersion` pins the bold line.
- `TestAnOlderDescriptionGainsItsFirstLine` adds two cases: a plain first line dockhand wrote is given the bold one, and one a person moved stays theirs.
- `TestSubmitFollowsThePublicationRule` and the command's preview test read the bold line.
