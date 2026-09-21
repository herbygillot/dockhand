# The baseline survey

The second half of queue item 1: one run over the whole tree with the tools as they stand after the switch and setup-proc inputs, the expression parser, the family on demand, and the survey's own stub fix, journaled with the new `--journal`, so every later item is measured against it. The journal stays under `~/.dockhand/surveys/`, outside the checkout.

**The run.** 41,730 ports at commit `842164fd61d`, the same tree the 2026-09-20 survey assessed, so the difference is dockhand's alone.

| | 2026-09-20 | 2026-09-21 |
|---|---|---|
| wall time | 4 h 35 min | 34 min 27 s |
| CPU | 7.1 cores | 10.7 cores |
| input-found | 81.1% as reported, about 92% after the stub fix | 94.1%, 39,280 ports |
| unsupported | 16.9% | 3.5%, 1,479 ports |
| unknown | 2.1% | 2.3%, 971 ports |
| Portfiles fully covered | about 92% | 93.5%, 18,789 of 20,096 |
| ports not covered | about 3,200 after the stub fix | 2,450 |

The evaluator's family on demand is where the eight times comes from: the survey's evaluations were quadratic in family size, and php alone was 60 percent of the old run.

**What remains, by the first check that stopped each port.** A port stops at its first failing check, so a bucket downstream grows when the one upstream shrinks: 844 of the 1,154 ports that had no editable version input now have one and are counted wherever they stop next.

| bucket | ports | notes |
|---|---|---|
| Nothing to fetch: metaports and `_select` ports | 441 | 196 `_select` ports; 299 Portfiles |
| A pre-fetch hook the guard grammar refuses | 427 | 311 Portfiles |
| Host state read during evaluation | 674 | 366 in the six frozen qt5x Portfiles, 33 php, 275 others, led by the pyqt5 family |
| No editable version input established | 310 | 71 in the llvm family across 15 Portfiles; was 1,154 |
| A checksum declaration outside the observed contexts | 107 | 61 of them php's |
| Dependency manifests and their sources | 119 | cargo.crates wanting one declaration; patches that edit a manifest |
| A calculated checksum algorithm | 86 | the cross binutils ports |
| Fetch customization or vendored source | 78 |  |
| Unknown tag convention | 63 |  |
| Patchfiles dockhand cannot place | 79 | remote or ambiguous patchfiles, a missing patch directory |
| Smaller tails | 66 | custom fetch procedures, post-fetch hooks, a git clone mixed with an archive, credentials, platform operands, two Portfiles the index lacks |

The 130 index disagreements of the last survey are gone; `SuiteSparse`, the example, is ready. php stands at 483 of 605 subports ready: 61 stop at a checksum declaration outside the observed contexts, 33 at a host read, 27 have nothing to fetch, and 1 has no version input, so the note-only host reads of queue item 5 are worth 33 php ports, not 122.

**What this sizes.** The pre-fetch guard grammar, 427 ports across 311 Portfiles, all behind a hook the grammar refuses. An outcome for ports with nothing to fetch, 441, of which 196 are `_select` ports. Literal segments of a composed version, the llvm family's 71 ports across 15 Portfiles. The note-only host reads, php's 33 and then whatever the PortGroup-local reads explain of the 275 other host readers, where the pyqt5 family leads. The six frozen qt5x Portfiles, 366 ports, stay not worth modeling and are 15 percent of everything not covered.
