# 2026-09-21: a refused pre-fetch hook says why and where

## Why

The fetch recognizer's predicates returned booleans, so a refusal reached
the user as "pre-fetch hook 2 has unrecognized behavior": a hook number,
with no word on whether the hook came from the Portfile or a PortGroup, and
nothing about which command the grammar stopped at. The reason was known at
the point it was lost. Item 1 on the roadmap sizes grammar extensions by
hook shape, and the assessment could not name the shape.

## What changed

- Each recognizer returns a refusal: the first command or condition it
  stopped at, with a one-line snippet cut at sixty characters, and the
  offset of that command in the body. The hook's first command chooses the
  recognizer: `if` reads as a conditional rejection, the Go PortGroup's
  first line as its toolchain check, anything else as an unconditional
  rejection. The wordings: "ends with `error ...` rather than return -code
  error", "runs `catch ...` before rejecting", "returns a computed message",
  "has a branch that <reason>", "runs `<command>` outside an if", "has a
  condition that is not braced", "calls `file` in its condition: `<cond>`",
  "calls `variant_isset` with a computed argument in its condition", "is
  the Go PortGroup's toolchain check, recognized only when go.domain is
  github.com; this port's is gitlab.com", "differs from the Go PortGroup's
  toolchain check as dockhand knows it", "is not wrapped as a Base hook",
  "does not parse as Tcl", "is empty".
- The worker reports where each hook was written. It searches the PortGroup
  files the port loaded, from `PortInfo(portgroups)`, then the Portfile,
  for the trimmed body text, and returns the label and the line the body
  starts on as a fourth field of `fetch_details`. The evaluator accepts
  three or four fields. The message adds ", at Portfile line 302" or ", in
  the mpi-1.0 PortGroup at line 393", the line of the offending command
  itself, computed from the body offset.
- The four boolean predicates remain for the tests as wrappers over the
  reason functions.

## Seen on the tree

- boost169 and GASNet: "pre-fetch hook 2 calls `mpi_variant_name` in its
  condition: `${mpi.require} && [mpi_variant_name] eq ""`, in the mpi-1.0
  PortGroup at line 393". The earlier attribution to the procedure call
  after the `if` was wrong; the grammar stops at the query in the condition.
- Okapi: "pre-fetch hook 1 has a branch that runs `java::java_set_env`
  before rejecting, in the java-1.0 PortGroup at line 44".
- gitlab-runner: the Go toolchain check refused for go.domain gitlab.com.
- llvm-10: a branch that ends with a nested `if` on a host read, at Portfile
  line 302; terminal-notifier: `vercmp` in the condition, at Portfile line 22.

## Evidence

- `TestRefusalNamesTheCommandTheGrammarStoppedAt` covers each wording and
  the line arithmetic for a Portfile and a PortGroup origin.
- `TestRefusedHookIsPlacedInItsFile` evaluates a fixture tree under
  port-tclsh with the hook in the Portfile and in a PortGroup, and checks
  the full option error including the line.
