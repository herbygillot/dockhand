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

## A saved plan's messages read as plain text (finding 1)

`tidy --plan --out <file>` saved the plan as JSON, as it had since it was added, so a message to edit was an escaped string: `"sshuttle: update to 2.0.0\n\nBuild with Python 3.14…"`. The guide has always named the file `plan.toml`.

A plan is now TOML, and says so in a comment at its top:
- each message is a multi-line literal string, which reads as the commit will say it, and is edited as plain text;
- a message TOML can't hold that way, one with three single quotes or a control character, is an escaped string instead;
- the rest is written by the TOML encoder already vendored, whose own strings escape newlines, so the message writes itself (`planMessage`, a `toml.Marshaler`);
- reading a plan refuses a key a plan doesn't have, as reading JSON refused an unknown field;
- a plan saved as JSON before, version 1, is still read, and a version newer than this dockhand's is refused as before. A plan whose version doesn't match its format is not a saved plan.

The engine's allowed imports name the TOML package, for a saved plan.

Tests:
- `TestASavedPlansMessagesReadAsWritten`: a message with a body, one with quotes, a backslash, and a tab, and three that need escapes, each read back as it was, and literal only where it can be;
- `TestASavedPlanAppliesUntilTheBranchMoves`, now over TOML, with a JSON plan from before still applying, and the refusals;
- `TestTidyRegroupsAndAppliesASavedPlan`: a message edited in the file as plain text reaches its commit.

Eight mutations each fail a test.

## Applying a saved plan shows what it writes (finding 2)

`tidy --apply` reprinted the saved plan as the proposal had been printed: each commit's subject, files, and the notes on how the proposal was made. It never showed a message's body, so the body the person had added to sshuttle's commit showed only in `git log` afterwards. A note can also go stale once a message is edited, such as "subject from your commit e15ced6". And that note said "your commit" of any commit it took a subject from, though e15ced6 was dockhand's own, carrying its Generated-By line.

Now:
- **Applying shows each commit as it will be written:** its subject, whether it takes edits not yet committed, its files, its author, and its whole body, blank lines kept.
- **The saved notes aren't read back.** They said how the proposal was made. The plan as saved, edited or not, is what applies. The file keeps them for the person editing it.
- **A subject's source says whose commit it was:** "subject from dockhand's commit …" of one carrying dockhand's attribution line, and "your commit" otherwise. Whether a message is dockhand's is `commitmsg`'s to say (`Attributed`), beside the attribution line itself.

Tests:
- `TestTidyRegroupsAndAppliesASavedPlan`: the apply shows the edited, two-paragraph body, and no notes;
- `TestASubjectSaysWhoseCommitItCameFrom`: a person's commit, and one of dockhand's;
- `TestACommitIsDockhandsByItsAttribution`;
- the saved-plan test: a loaded plan carries no notes.

Seven mutations each fail a test.
