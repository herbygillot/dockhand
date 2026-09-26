# 2026-09-26: the oracle's phase 5b, the Java PortGroup's hook admitted

The second half of phase 5 in the [oracle](../oracle.md) scope, decision
18 of the [contracts direction](../reviews/2026-09-23-contracts-direction.md).
The Java PortGroup's pre-fetch hook made 177 ports unsupported. The hook
grammar refused it, not the evaluator: the hook calls
`java::java_set_env`, whose `find_java_home` runs `/usr/libexec/java_home`
and writes `depends_${deptype}-append`. The grammar refused every `exec`
on sight and every computed command name. Hooks stay judged statically
(decision 17); decision 18 gives the grammar the two rules it lacked.

## What changed

- **An `exec` of a program that only reports is admitted** in a hook, by
  the same rules the evaluator's dispatcher runs programs by since phase
  2: `macports.HostPrograms`, now judged in Go too
  (`macports.CommandLineRefusal`):
  - every stage of the pipeline must be a listed program in a form that
    only reports;
  - output may go only to `/dev/null` or a channel;
  - nothing may be left running in the background.

  Source text is judged before it runs, so a substituted word is
  admitted only as the value an option takes, as `-v ${java.version}`
  is. It is never admitted as a program, an option, or a redirection.
  What the program prints lands in the word around the `exec`, which the
  grammar already judges: a plain variable, or a rejection's condition.
  A condition only decides whether the fetch fails, never what it
  fetches.
- **A computed command name is judged by its fixed prefix's family**,
  for the one family whose every member is harmless: `depends_`, as in
  `depends_${deptype}-append`. No fetch reads a dependency. Any other
  computed name is still refused.
- The table's comment no longer says the grammar refuses `exec`
  outright; it shares the table now, as phase 2 kept it in Go to allow.

## Tests

- `TestCommandLineRefusalAdmitsOnlyProgramsThatReport`: the rules the
  dispatcher applies in Tcl, applied to source text, with substituted
  words.
- The grammar's tests: the Java PortGroup's hook is admitted, as is a
  dependency named by a variable. A procedure that `exec`s `touch`, and
  a condition that does, are refused, as is another computed command. A
  rejection whose condition runs `uname -m` is admitted.

## The survey

The whole tree at `abd9fff84df`, compared with phase 5's survey
(`~/.dockhand/surveys/2026-09-26-phase5b-abd9fff.jsonl`):

- **176 ports moved, all Java PortGroup users, and none back.** 156
  load the PortGroup themselves, and 20 through `maven`, which loads it.
  - **173** went from unsupported to input-found. That is the scope's
    "180 Java ports", confirmed.
  - **objectweb-asm and saxon** get past the hook to the next check, an
    unknown tag convention.
  - **lp_solve_java** gets past the hook to the version probe, where its
    parent port lp_solve already stops: a modelled context's compiler
    read at `Portfile:20`.
- **182 fetches refused before are accepted**, and none the other way.
  Unsupported as the first stop fell from 830 to 655.
- **Three ports gained refused contexts**: bazel and bazel-6 on 10.8–10.11,
  and turbovnc-viewer on 10.6. Their Java hook's guard is read now, and
  it rejects where `java_home` finds no JDK. No outcome moved.
- **Cost:** 29,416 CPU seconds, as phase 5's.

## What decision 18 still asks

The hook's guard rejects where `java_version_not_found` is set, and that
is decided when the port is evaluated. The Java PortGroup's callback runs
`java_home` there, which asks this Mac: the callback runs after the
Portfile, where the observation doesn't see it. JDKs also live outside
the prefix, in `/Library/Java/JavaVirtualMachines`, where MacPorts'
openjdk ports install them too, so the fresh installation doesn't cover
them.

Decision 18 answers `java_home` from the facts table instead, "Unable to
locate a Java Runtime" on a fresh Mac, and tries both outcomes where a JDK
may be installed. Which contexts a Java port refuses depends on that, but
what it fetches doesn't. That answer is the next step for Java, and it
is not in this phase.
