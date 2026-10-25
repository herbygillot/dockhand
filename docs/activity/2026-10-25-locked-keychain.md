# setup tells a locked Keychain from no login

2026-10-25. F5 on rc5, placed at roadmap 59553f59 as a fix for main riding
v0.6.0. Over SSH with the login Keychain locked, `dockhand setup`'s
Publishing line said the Keychain was locked, but its github line said a
login was needed, it offered one with yes the default, and its Next: line
named `dockhand setup github`. `github.LoginSource` read any saved-login
error as no login.

- **The owner's word.** github `LoginSource` returns `LoginLocked` where
  the store keeps a login it can't give (`credential.ErrLocked`), and
  `LockedLogin` is the refusal for it, of the Auth kind, wrapping the
  store's error: "your GitHub login is saved, but the login Keychain is
  locked; unlock it with security unlock-keychain in this shell, or set
  GH_TOKEN for this command", Next `security unlock-keychain`.
- **Refused up front.** github `Login` asks the store first, and refuses a
  locked one before the device flow, where GitHub's page was gone through
  only for the login not to be saved.
- **setup.** Its offer is made only for no login, as before, so a locked
  store is offered none. Providers' github line says the login is saved
  and the Keychain locked, and `missingSteps` names unlocking it, or
  GH_TOKEN, in Next. A missing, old, or expired login is offered one as
  before.
- **A browser that won't open** (setup/auth.go): the login says to open
  the address it printed and keeps waiting for the code, where it failed
  and named `--no-browser`.
- **Held:** v0.5.0's README known-issues row waits for the person's answer
  on the prioritization thread's card.
- **Tests:** TestALockedKeychainIsNotNoLogin and
  TestABrowserThatWontOpenLeavesTheLoginWaiting.
