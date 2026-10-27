# Port selectors, and serve's selection

2026-10-27, the docs dig's I38a (roadmap's "Port selectors, and serve's
selection"), at the person's "Dockhand should be able to accept port
selectors - it should not automatically only work on just *my* ports." and
"Dockhand should behave under the same policy either way."

- **outdated, update, and ship take `--maintainer`, `--category`, and
  `--not-maintainer`** (command/outdated's `Selectors`, which each
  registers), repeatable or comma-separated, combined as MacPorts combines
  them: alternatives within a flag, all of the flags together, through
  `portindex.Filter` on master's index (engine.OutdatedRequest's new
  Categories and NotMaintainers, passed to `outdated.Selection`). `--mine`
  stays, as `--maintainer` with the configured maintainer, and may sit
  beside the others; each maintainer is asked once. `--not-maintainer`
  alone is refused: it leaves ports out of a selection. A selection goes
  the way `--mine` did: update's one branch a port, asked once; ship's
  plan, holds, and stages. A batch's "Of the rest" line names outdated with
  the selection's own flags.
- **serve's selection is configurable**: `serve.maintainers`,
  `serve.categories`, and `serve.not_maintainers`, your ports, the
  maintainer line's, when neither of the first two is set
  (`engine.ServeSelection`, `PortSelection`, on `Engine.Serves`, which
  replaces `Engine.Maintainers`). The day's look asks for that selection,
  records it in outdated.json, and status shows a look only under the
  selection configured now. Its lines say "the selected ports" and its
  notice and status name outdated or ship over the selection's flags; the
  same holds, `serve.submit_limit`, and `serve.ship_to` stages apply.
  `dockhand config` prints the three keys.
- Tests: TestShipMineAndPassing (the selection's request, `--mine` beside
  `--maintainer`), TestServesSelectionCarriesAnothersPortAsItsOwn (to
  commit), TestTheDaysLookNotifiesWhenWhatItFoundChanged, and the index's
  "no maintainer in a category" cases, which select as `port echo
  nomaintainer and category:sysutils` does.
