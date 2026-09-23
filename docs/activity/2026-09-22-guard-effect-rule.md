# 2026-09-22: the guard grammar judges commands by their effect on the fetch

Next item 1's remainder. After the three grammar extensions, 376 ports
were still refused for a pre-fetch hook, 321 of them behind two
PortGroup procedures the grammar could not see into: the Java
PortGroup's `java::java_set_env` before its rejection, and the MPI
PortGroup's `mpi.action_enforce_variants` after its `if`. The decision,
recorded on the roadmap that evening, was not to match the two hooks by
shape, as the Go toolchain check matches its one hook, but to enforce
the property the grammar protects: a hook may fail the fetch and must
never change what is fetched, judged by what each command does.

## What landed

**The effect table.** `macports.AffectsFetch` names the fetch phase's
inputs: the archive names and sites, the checksums, the patches, the
source's name and layout, the `fetch.`, `extract.`, `use_`, and
version-control families, and the forge sources, with an option
command's suffix stripped by `OptionName`. A test cross-checks the
editor's download policy against it, so the two lists cannot drift.

**The worker ships definitions.** The observation worker, which already
runs the Portfile in a real interpreter to place each hook, now reads
the first word of every command in the hook bodies, resolves it in the
namespace of the procedure it appears in as Tcl would, and ships what it
is: an option command with its option, from the `handle_option` alias
Base installs; a procedure with its arguments and body, from `info
args` and `info body`; or a built-in command. The bodies of the
procedures a hook reaches are shipped too, five levels deep and 128
definitions at most. Nothing is executed, and no file is opened.

**The effect rule.** Where the grammar's own rules refuse a command, a
diagnostic before a rejection being the only thing they allowed, the
recognizer now judges the command by effect: a diagnostic, a plain
variable, an option outside the table, a read of the host or the
records, a control structure whose bodies are harmless, or a procedure
whose resolved body is, changes nothing the fetch reads and is
admitted. A write to a fetch option, a computed command or variable
name, an `exec`, `system`, `eval`, or the like, and any command the
worker could not define, are refused, and the refusal names the command
and where it is: "runs `java::java_set_env` before rejecting, which runs
`set java_home [find_java_home]` inside java::java_set_env, which runs …
`exec /usr/libexec/java_home -V`, which is not followed by the grammar".
Base's `option` reads with one argument and writes with two, and is
judged by that rather than by following its body. A hook that rejects
nothing is a guard that "changes nothing the fetch reads".

**The specific checks stay first.** The Go PortGroup's toolchain check
matches exactly, as before, before anything else is consulted.

## On the tree

The 376 ports the survey still refused, assessed at the survey's commit
with the new rule, every Portfile-level hook it newly accepts read:

| | ports |
|---|---|
| Now guarded | 182, across 116 Portfiles |
| of which conditional rejections | 155 |
| harmless hooks that reject nothing | 19 |
| unconditional rejections | 3 |
| Outcome moved to input-found | 153 |
| Outcome moved to unknown, the fetch no longer the reason | 21 |
| Still refused | 194 |

The MPI family is in: its hook's query in the condition, its procedure
after the `if`, and the Base helpers that procedure reaches, `_get_dep_
port` with its `switch`, `_portnameactive`, `_mportsearchpath`, and the
registry queries, all read as what they are. So are the R ports' `catch`
around a registry read, Gyoto's version check against the registry,
MacOSX.sdk's walk over its distfiles that only reads them, and the llvm
ports that set `supported_archs` before their runtime check.

Of the 194 still refused, 180 are the Java PortGroup: `java_set_env`
reaches `find_jvm_versions`, which runs `/usr/libexec/java_home -V`
through `exec` to discover the installed JVMs, and the rule refuses
`exec` on sight, since what a program does cannot be read off a hook.
That is a policy question now, not a grammar gap: whether an `exec` of
a literal program path whose output is only captured is a host read the
grammar may admit. The refusal names it, and the count is the argument
either way. The rest: eight Go ports whose PortGroup check is
recognized only for github.com, five R ports with an unbraced
`!{$result}` condition, and one port whose PortGroup runs the `bun`
binary to check its version, another `exec`.

Nothing recognized before is recognized differently: the rule is
consulted only where the grammar refused, and the refusal wordings that
survive gained a clause saying what the command does.

## A bug the suite caught

The worker's first version of the walk ran at the port interpreter's
global level, and its `set name …` over the command names it found
rewrote the port's own `name`; a preparation test fetched an archive
called `uplevel-2.0.tar.gz`, after the last command in its hook. The
walk runs under `apply` now, so its variables are its own, and the 376
ports were assessed again with the fixed worker: the numbers above are
that run's, and they match the earlier run's exactly, since the hook
classification never read the clobbered variable and the assessment's
other reads came before it.
