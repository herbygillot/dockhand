# Codex's wording review applied

The person asked (2026-10-17) that Codex's review of dockhand's wording,
docs/reviews/dockhand-wording-review.xlsx (248 messages, 251 proposals, at
88a7c9fb), be applied wherever a message has one suggestion. 245 do; three
have a recommended and an alternative wording and are left as they were
for the person to choose: CLI-020/021 (check's "queued; nothing is
running it"), WF-035/036 (status's "passed; held for a look"), and
SV-010/011 (serve's "login can't renew itself").

- **How it was applied:** each code row's old message is the decoded Go
  string literal at its file and line, so a small tool parsed each file,
  found the literal whose value is the old message nearest its line, and
  replaced it with the proposal quoted the same way; WF-039's two
  occurrences in refreshStatus both changed, as its note asked. Each of the
  39 doc rows replaced its excerpt in place. All 245 were found; none was
  edited by hand.
- **Checked before applying:** every proposal keeps its message's format
  verbs in order, and none drops a command, a flag, or a number; the ones
  that drop the word "dockhand" do so in prose, not in a command offered.
  CLI-057 names $VISUAL beside $EDITOR, which `editor()` reads first.
- **Tests:** about 110 tests pinned the old wording. Each was updated to
  the new text, the expected text taken from the message as it now reads;
  a first pass that replaced shared fragments blindly reached messages that
  hadn't changed and was reverted. The help-reference test reads "use
  dockhand at" as a sentence ("at" joins sentenceWords), for WF-003's
  "if you use dockhand at least once every six months".

## The three with two suggestions (2026-10-18)

The person picked "A, B, B": check's queued line takes CLI-020's "%s
queued. Start dockhand serve to run it."; status's passed-and-held line
takes WF-036's "passed; submission paused for review: ", in both its Held
and Moved branches; serve's login notification takes SV-011's "GitHub
login expired or cannot be renewed. Sign in again: dockhand setup github".
Their three tests follow.
