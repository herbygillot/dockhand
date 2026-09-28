# 2026-09-28: the description merge says what it did

The fourth step of the private-helper review's item ([review](../reviews/2026-09-28-private-helper-ownership.md), finding 10; [reconciliation](2026-09-28-private-helper-review-reconciled.md)).

Submitting to a pull request already open merges the fresh description into the existing one. It refreshes the parts dockhand writes while they're still as it wrote them: the Type(s), and everything from Tested on down. The merge knew what it did to each part, but returned only the text, and a boolean about the Tested on part. `refreshedParts` then found both parts again in the text before and after, compared them, and returned English phrases for the preview. That was a second reading of what the merge had just decided.

Now `mergeBody` returns each part's outcome as it decides it (`DescriptionSections`):
- **refreshed:** rewritten, and different;
- **current:** dockhand's, and already as it would write it;
- **kept:** someone's own, edited since dockhand wrote it, or never dockhand's;
- **absent:** left out of the description, and staying out.

`SubmitPlan.Sections` carries them in place of the phrases, and the command words them. `refreshedParts` is gone. `BodyKept` follows from them: the Tested on part is kept, or absent.

Someone else's pull request, whose description submit never rewrites, has both parts kept. So its `BodyKept` is true, and a terminal no longer asks the template's tested questions for a description that won't be written.

`TestTheMergedDescriptionKeepsOnlyWhatDockhandWrote` and `TestTheMergedDescriptionsTypesAreDockhandsWhileUnchanged` check each outcome, current and absent included. The submit tests check the words and the plan. Eight mutations each fail a test.
