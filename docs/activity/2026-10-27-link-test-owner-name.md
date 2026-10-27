# An undeclared link's regression test, and a move's old owner name

2026-10-27, the next two of field testing's outdated-ports block
(roadmap c21ac8aa).

- **An undeclared link's regression test** (rustup linked liblzma with no
  depends_lib on xz). TestAnUndeclaredLinkIsFoundFromTheGuestsReading runs
  the guest program over a Rust build whose binary loads
  ${prefix}/lib/liblzma.5.dylib, which xz provides, records its links as
  the host does (follow.go), and finds xz through
  `evidence.UndeclaredLinks`; with xz declared it finds nothing. The hold,
  `link-undeclared`, waits on the person's answer on the card, so the
  finding is still a notice.
- **A repository move says when the old owner's name belongs to another
  account now** (yabai, whose old owner name, koekeishiya, now names a
  new, unrelated account). Where an update's repository moved to another
  owner and the move isn't followed, `oldOwnerNow` reads the old name's
  account, one request (`GET /users/{old}`, the forge's new `Account`),
  and the repository's creation date (forge.Description's `CreatedAt`):
  where none holds the name, the move's words add that it's free for
  anyone to take; where an account made after the repository holds it,
  that it now belongs to another account, created <date>, which could
  publish a repository of that name in this one's place, so
  `--follow-moves` matters. An account older than the repository, and a
  rename within one owner, read as before.

TestAMovesOldOwnerNameIsReadNow.
