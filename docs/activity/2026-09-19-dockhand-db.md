# DOCKHAND_DB

`--db` gains an environment variable, `DOCKHAND_DB`, read the way `MACPORTS_TREE`, `MACPORTS_PREFIX`, `GIT_BIN`, and `TART_BIN` are: it supplies the flag's default when set, the flag overrides it, and the built-in default of `~/.dockhand/state.db` applies when neither is given. The flag's help names it. The root test checks the three cases, including that the help footer's "State database" line follows the environment and that an explicit flag wins. The usage note that said `--no-publish` has no shorthand, stale since `-P` arrived, was removed on the way.
