# 2026-09-21: the intermittent CLI test panic, captured

## What it was

The CLI test package had stopped three times this week with a runtime
fatal error that was never captured. Today's full-suite run caught it:

```
fatal error: concurrent map writes
github.com/spf13/cobra.AddTemplateFunc(...)
github.com/herbygillot/dockhand/internal/cli.newRoot(...)  root.go:176
```

The help-sections change of 2026-09-21 registered the `flagSections`
template function with `cobra.AddTemplateFunc` every time a root command
was built. Cobra's template table is a plain package-level map, a root is
built per command and per test, and the CLI tests run in parallel, so two
constructions wrote the map at once and the runtime stopped the binary. It
surfaced only under the whole suite's load, which is why it never repeated
on demand.

## The fix

The function is registered once, under a `sync.Once`. Four consecutive
runs of the package under the race detector pass.
