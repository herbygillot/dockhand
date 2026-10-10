# A locked Keychain said plainly (rc10's F5, for rc11)

Over SSH with nobody at the screen, dhtest's login Keychain was locked, and
`dockhand update go-reflex --new` exited 1 with "github: reading saved
credential: keychain: reading credential: security find-generic-password:
exit status 36:". `security` exits with the low byte of the Security
framework's status, and the Keychain store read only 44 (errSecItemNotFound).

- `credential.ErrLocked`: a store that keeps the credential but can't give it
  now. Never a reason to try another source.
- The Keychain store reads exit 36 (errSecInteractionNotAllowed) from
  find-generic-password and delete-generic-password as locked: "the login
  Keychain is locked, as it is over SSH with nobody at the screen; security
  unlock-keychain unlocks it".
- github's saved login adds its own way out: "github: the saved GitHub login
  can't be read: …, or GH_TOKEN set for the command stands in for it". The
  GitHub CLI's login isn't tried in its place (tested).
- Every reader goes through that path: commands, `auth status`
  (AuthenticatedUser), and serve's minute re-read, which says the problem once
  as a login it couldn't read and keeps it in status, not as a rejection.
