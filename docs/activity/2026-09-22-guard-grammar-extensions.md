# 2026-09-22: three extensions of the pre-fetch guard grammar

Next item 1. The grammar that decides whether a pre-fetch hook can fail
the fetch but never change what is fetched refused three shapes the
survey found behind ports whose hooks do exactly that. Each is an
alternative added beside what the grammar already read, so the change
can only turn refusals into acceptances, and every acceptance it makes
on the tree was read.

## The extensions

- **Tcl's `error` where `return -code error` stood.** A hook, or a
  branch of one, may end in `error` with exactly one plain message,
  text and simple variable substitutions. An `error` with a computed
  message, with an info or code argument, or with an expanded word stays
  refused, and so does `ui_error` or `ui_msg` in that position, which
  reject nothing. Unconditional, it preserves the platform restriction
  as `return -code error` does.
- **A branch that is itself a conditional rejection.** llvm-10's hook is
  a platform check around a runtime check; ld64's goes three deep. A
  branch that opens with `if` is read as a conditional rejection in its
  own right, with the same rules at every depth; a branch that does
  anything before its nested `if`, or after it, stays refused. A nested
  conditional claims no platform restriction, as a flat one never did.
- **A host read in a condition.** `file exists`, `file isdirectory`, and
  `file isfile` with one argument, `vercmp` with two or three, and `info
  exists` with one, each a predicate with no effect on the host or the
  interpreter. Their arguments may substitute variables but not
  commands, judged on the words' text with the substitutions left in
  place, so the subcommand cannot itself be computed. Any other `file`
  or `info` subcommand is refused by name: "calls `file delete
  ${prefix}/lib` in its condition". `mpi_variant_name` joined the pure
  variant queries beside `fortran_variant_name`, being the same read of
  the interpreter's variant state.

The refusal wordings did not change; the placement test's fixture,
whose hook the grammar now reads, uses `ui_msg` instead.

## On the tree

The survey's rerun held 466 ports refused for a hook. 237 of them were
refused for one of the three shapes; assessed again with the new
grammar, at the survey's commit:

| | ports |
|---|---|
| Now guarded, a conditional rejection | 68 |
| Now guarded, an unconditional rejection | 32 |
| Outcome moved from unsupported to input-found | 23 |
| Outcome moved from unsupported to unknown, the fetch no longer the reason | 57 |
| Still unsupported | 155 |

The 100 now guarded span 42 Portfiles, and every hook body was read:
the clang and llvm runtime checks, ld64's, the Xcode version checks of
mas, xcbeautify, and whereamip, the SDK-presence rejections of opencv3,
ruby26, libaacs, fsevents-tools, and game-porting-toolkit, the platform
rejections of FScript, jubatus, pficommon, mediatomb, ncarg, libopenraw,
and the xorg servers, the variant requirements of clojure-lsp, jet,
reinteract, gtkextra3, and avahi, hadoop's Java check, and the composed
darwin rejections of the mpich and openmpi compiler subports. Of the
155 still unsupported, 141 are the MPI family: the PortGroup's hook
passes its condition now and stops at `mpi.action_enforce_variants`
after the `if`, a procedure the grammar cannot see into. The other
refused shapes stay refused, checked on a sample: the Java PortGroup's
180 ports, whose branch runs `java::java_set_env` before rejecting; the
SDK ports' `foreach`; clang-12's `supported_archs` before rejecting;
gyoto's registry read; the Go toolchain check off github.com.

## What this sizes next

The two buckets that remain are procedure calls, not grammar: the Java
PortGroup's `java::java_set_env`, which sets build options from the
host's Java and so is not a rejection, and the MPI PortGroup's
`mpi.action_enforce_variants`, which raises on a dependency's variants
and changes nothing, but whose body the grammar would have to know the
way it knows the Go toolchain check. Together they are 321 ports, more
than the three extensions reached, and each is one PortGroup.
