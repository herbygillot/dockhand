# serve's daily look notifies when what it found changed (v0.4.0 part 1, c)

The rc10 full stage's B7: the day's look's count, "116 ports of yours
have newer releases", went to serve.log and status alone. serve notified
for a pull request's change, a branch it prepared, and a submission, so
with `serve.for_outdated = "list"`, the default, serve said nothing a
person saw.

- `engine.lookNotice` words the look as one notification, "Your ports: 2
  ports have newer releases (dockhand outdated --mine)", where the ports
  found with newer releases aren't the last look's, compared as sets;
  nothing where they're the same or there are none. A rate-limited look's
  retry that adds ports changes the set, and notifies again.
- It names `dockhand outdated --mine`, which lists them, rather than
  status's `update --outdated --mine`, which part 2 deletes with `ship`.
- usage.md's serve section says it.

Test: TestTheDaysLookNotifiesWhenWhatItFoundChanged.
