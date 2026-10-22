# check -d starts a drain

Roadmap part 1, item 7's long check without a terminal (the gnome-voice
session's, which needed --replace to run its check here once -d had
queued it; an agent's foreground check outlived its tool's timeout, and a
session's end killed one).

- `check -d`, and `retry -d` and a baseline's, with no serve leading,
  start `dockhand serve --drain --no-notify` on the same checkout and
  database in a session of its own, logging to drain.log beside the
  database, as automatic cleanup is started (`host.startDrain`, in
  cleanup_unix.go), and say "check-N queued; a background drain (pid N)
  runs it. dockhand wait check-N follows it." Where it can't start, they
  say so and to start serve. Both background starts now run in the
  invocation's environment (`engine.Options.Env`), not the process's.
- `Engine.Serve` with Drain, having found the queue empty and let go of
  the lead, reads the queue once more and drains again where a check was
  queued meanwhile, once per run, so one it can't run doesn't loop.
- runQueued and runBaseline take the settings, for the host and the
  options the engine opened with.

Item 7's last step, status listing dockhand's processes, is next.
