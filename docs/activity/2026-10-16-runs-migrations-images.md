# A migration under a running dockhand, a stopped run, and MacPorts that moves

Three items part 1's fixes gained on 2026-10-15 and 16, from updating the
dockhand port to 0.3.1 and field testing's rust check that night.

- **A migration waits for the processes on the older schema.** Opening a
  database whose schema this build would migrate, the store first counts
  the serve and foreground sessions with a heartbeat in the last two
  minutes, coord's, after which another dockhand may take over: each reads
  the older schema, and couldn't record its work after the migration. With
  any, it refuses, `store.LiveUnderMigrationError`, "2 dockhand processes
  are working on schema 31 (v0.3.1): migrating the database to schema 32
  would leave them unable to record what they do; wait for them to finish,
  or stop them, and run this again". On a terminal, command asks first,
  and a yes opens it again with `engine.Options.MigrateUnderLive`;
  observers, which only read, and ended or stale sessions don't count. A
  script is refused, and runs again later. No schema changed: sessions
  have kept the same columns since schema 1.
- **A build that finds the schema newer says so and stops.** Its heartbeat
  failing with `store.ErrSchema` is said as what it is, "… newer than this
  dockhand supports (31); use a newer build. This dockhand can't record
  its work any more, so it stops; a newer one resumes a stopped check, with
  dockhand wait", and ends, where it said another dockhand might take over
  after two minutes. Builds before this one keep the old words.
- **A stopped run is said as stopped.** `logs` says "check-1 · stopped: the
  process running it ended; dockhand wait check-1 resumes it" where it said
  running; `check` of the same files refuses it as stopped, offering
  `wait` to resume it or `--replace` to check again, where it said
  "already running … dockhand wait follows it"; and status's row says wait
  resumes it, beside cancel ending it.
- **An image whose MacPorts is older than this Mac's is said.** A Tart
  image keeps the MacPorts it was made with: #35553 was checked on the
  Tahoe image's 2.12.6 while this Mac had 2.12.7, unsaid. The Tart
  provider reads an image's version from setup's record and this Mac's
  from `port version` once (`MacPortsBehind`, behind
  `buildenv.MacPortsProvider`; `tart.Status.Behind` for setup).
  `engine.PlanNotes`, which took `PolicyNotes`' place, adds a line under a
  check's Provider, "tart macOS 26 (Tahoe) arm64 with Xcode builds with
  MacPorts 2.12.6, older than this Mac's 2.12.7, which reads its ports;
  dockhand setup tart tahoe --rebuild --xcode <Xcode> makes it again", and
  setup's Providers row says the same of each image. Said by check and
  setup rather than status, which would list Tart's VMs on every run.
  `macports.BaseAtLeast` is the comparison, moved from the provisioner.
- **A newer MacPorts family is admitted with a warning.** `admitBase`
  refused every release past 2.12, so the day 2.13.0 ships every command
  that reads a Portfile would stop until a dockhand release named it. A
  release of a newer family is admitted, with one line, "MacPorts Base
  2.13.0 is newer than this dockhand was built against (2.11 and 2.12);
  what it reads of Portfiles may be wrong where Base changed, until a
  dockhand release names it", said once a command; development builds and
  older families are refused as before. docs/macports-compatibility.md
  says so.
- **The setup protocol's pin** is updated, not raised: `baseAtLeast` moving to `macports.BaseAtLeast` changed the provisioner's code, not what an image holds.
