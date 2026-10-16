# Reusing an archived branch's name

The roadmap's part 3 item (decided 2026-10-07), with the rc8 full stage's
B10.

- start's refusal says what fits the record (`engine.TakenError`): an
  open branch, `cd "$(dockhand path X)"` works in it; an archived one
  without a pull request, that start offers to rename it aside on a
  terminal; an archived one with a pull request, merged, or closed, that
  its record keeps the name.
- `engine.RenameArchived`: an archived branch renamed `<name>-archived`
  (`-2`, `-3` where that's taken), its Git branch and its record, with a
  rename event; `archive --undo <name>-archived` finds it.
- One with a pull request isn't renamed, a narrowing of the roadmap's
  "its fork branch if it has one": a branch reaches the fork only by
  submit, which opens its pull request, so its fork branch is that pull
  request's head, which a rename would leave behind, and with it the
  pull request. start says so and asks for another name.
- start, on a terminal, asks to rename it aside and then takes the name.
- Not in this slice: forgetting an archived branch for good, `clean
  --delete`, which deletes its Git branch and record, asking first. Next.
