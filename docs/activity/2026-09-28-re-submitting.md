# 2026-09-28: re-submitting

The hugo exercise's re-submitting of sshuttle, after its move to Python 3.14 ([review](../reviews/2026-09-28-hugo-bump-exercise.md#moving-sshuttle-to-python-314-and-re-submitting); verified in the roadmap's Reviews). Each finding is fixed in its own commit.

## A re-submit refreshes the Description (finding 3)

A pull request's description opens with a Description section, which submit writes from the commit's body, or as a table of the commits when there are several. A later submit refreshed only the Type(s) and everything from Tested on down, while each was still as dockhand last wrote it. So a body written after the pull request opened never reached it: sshuttle's commit gained "Build with Python 3.14, the python PortGroup's default.", and #35011's Description stayed empty.

The Description is now dockhand's as the Type(s) are:
- it's rewritten while it is still exactly as dockhand last wrote it, apart from GitHub's line endings;
- one a person edited is kept, and a description that leaves it out stays without it.

The merge says what it did to it, as to the other parts (`DescriptionSections.Description`). The preview names it, "refreshes its Description section", and counts a Description a person wrote as theirs: "its description is yours, and stays as it is", or "the rest of its description is yours". Someone else's pull request, whose description submit never rewrites, has it kept too.

Tests:
- `TestTheMergedDescriptionsDescriptionIsDockhandsWhileUnchanged`: refreshed, line endings aside, kept when a person edited it, and absent when left out;
- `TestAResubmitRefreshesTheDescriptionItWrote`: a pull request opened, its commit amended with a body, the preview, the re-submit, and a person's edit that stays;
- `TestTheRefreshedPartsAreNamed`: the words when all three parts are refreshed.

Seven mutations each fail a test.
