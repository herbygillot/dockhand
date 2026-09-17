# `-t` and `-p` for the tree and prefix flags

Asked for on 2026-09-17: the global `--tree` and `--prefix` shorthands become lowercase, `-t` and `-p`, in place of `-T` and `-P`.

Nothing else used either letter as a shorthand, so the change is the two definitions in the root command, the two warnings that name the prefix flag (the MacPorts Base skew warning in the Tart provider and in `setup`), the CLI path tests, and the current documentation. Activity reports and reviews from before today keep the spelling they were written with; they record what the flags were then.
