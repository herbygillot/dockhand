# 2026-09-26: serve's notifications, kept, and turned off at will

A side investigation looked for a well-kept macOS notification library to
replace the AppleScript `serve` posts notifications through, and found no
suitable one. The person settled it:

- **Keep AppleScript,** with a bound, and a word about how it behaves.
- **Add a flag that turns notifications off.**

## What changed

- **`serve --no-notify`** posts none for that run, whatever
  `serve.notify` says. `serve.notify = false` in the configuration file
  turns them off for good, as before.
  - It is `serve`'s flag, beside `--no-submit-passing`, rather than a
    global one, since `serve` is the only command that posts
    notifications. A global flag would sit on every command and be read
    by one.
- **`serve --install` carries its flags to the agent.** It wrote the
  launchd agent as `serve --tree … --db …` alone, so a flag given with
  `--install` was silently dropped, `--submit-passing` included. The
  agent now runs with the flags given beside `--install`:
  `--no-notify`, `--submit-passing`, and `--no-submit-passing`.
- **Posting a notification is bounded.** An `osascript` that doesn't
  return within ten seconds is stopped, not left running.
- **`serve --help` says how they behave:** macOS credits AppleScript's
  notifications to Script Editor, so clicking one opens it.

## Tests

- **Notifications are off** with `serve.notify = false`, and with
  `--no-notify`.
- **The agent** gets `--no-notify` and `--submit-passing` when installed
  with them, and none without; `--uninstall` only boots it out.
