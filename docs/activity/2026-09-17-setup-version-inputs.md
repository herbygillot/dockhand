# Versions carried as setup arguments

The first item of the evening's reconciled queue, moved to the top because of its share: 17,040 survey entries, 41% of the index, had no editable version input because the perl5, R, and ruby PortGroups carry the version inside `perl5.setup`, `R.setup`, or `ruby.setup`.

## What it took

Recognizing the argument was the small part: `portfile.Candidates` now reads `perl5.setup module vers ?cpandir?`, `R.setup domain author package version ?prefix? ?suffix?`, and `ruby.setup module vers ?type? ?docs? ?source? ?implementation?` the way it reads `github.setup` and `go.setup`. Four things followed from the PortGroups, found by assessing eleven ports across the three families before and after:

- **The perl module version is a spelling, not the port version.** `perl5.setup JSON 4.11` evaluates to version `4.110.0`; the module version is what CPAN lists and what `livecheck.version` holds. An archive source's version is now spelled as `livecheck.version` spells it when the Portfile evaluates one, the same string for almost every port and the module version for perl. Discovery compares the listing against that spelling, as Base does, and the release records the evaluated port version with `SourceVersion` beside it when the two differ; the edit writes the spelling, the evaluation still decides the version, and an explicit `--version` is the spelling too, so `bump p5-archive-tar-wrapper 0.42` means the module version. The stored-release validator, which required an explicit request to equal the version, learned the same distinction after the first real perl bump was refused at the job store.
- **CPAN's livecheck is `regexm`.** One match against the whole listing, first capture; the native extractor gained that mode beside the line-oriented one. MetaCPAN also answers 402 to Go's default User-Agent, so listing requests now send dockhand's own, the one archive downloads already sent.
- **Every R port loads the compilers PortGroup**, whose pre-fetch guard `if {${compilers.require_fortran} && [fortran_variant_name] eq ""} { return -code error ... }` was an unrecognized hook because its condition calls a command. Conditions may now call `variant_isset`, `variant_exists`, and `fortran_variant_name` with literal arguments, which read the selected variants and change nothing; any other substitution still refuses the hook.
- **The ruby stub is the python shape reversed.** `rb-foo` returns before declaring a livecheck and each `rb33-foo` subport declares the RubyGems check, so the newest subport keeps its own regex livecheck instead of borrowing the stub's absent one; `py-` stubs still lend theirs.

The `p5-` and `rb-` stubs bump as shared releases through their newest subport exactly as `py-` stubs do; nothing there needed to change.

## Counts

Every `p5-`, `R-`, and `rb-` port in the tree, 7,557 Portfiles, assessed in one pooled run of about ten minutes:

| Family | Portfiles | input-found | unsupported |
|---|---:|---:|---:|
| perl | 2,005 | 2,003 | 2 |
| R | 5,165 | 5,158 | 7 |
| ruby | 387 | 385 | 2 |

The eleven left: five R ports with a second, port-specific pre-fetch hook; one R port fetched from a repository and one with no archive; two perl ports and one ruby port whose version is not a literal; one ruby gem with no HTTP fetch location. The pinned corpus moved from 119 to 129 input-found with no regressions, the ten being its perl, R, and ruby members. Automatic discovery has a perl tail the counts do not show: 32 modules spelled `v1.2.3`, which the stability classifier calls unknown, need an explicit version, and a few livechecks whose CPAN author directory redirects from https to http are refused by the fetch guard; both are on the survey-tail item.

## Exercised

Live discovery: 130 random perl ports through MetaCPAN, all current except those tails, since the tree's perl maintainers keep up; R-jsonlite 1.8.9 to 2.0.0 from CRAN; rb-actionmailer, rb-nokogiri, rb-json and others from RubyGems. Previews: `bump R-jsonlite --diff` rewrote `R.setup cran jeroen jsonlite 2.0.0` with fresh checksums; `bump rb-mustache --diff` moved the stub and its eight subports to 1.1.3; `bump p5-inline-python --diff` wrote `perl5.setup Inline-Python 0.58` and evaluated 0.560.0 to 0.580.0 across the stub and four subports; `bump p5-dbd-pg --diff` moved 3.20.2 to 3.21.2. Real bumps with `--no-publish`: rb-mustache 1.1.3 built and passed in Tart on macOS 26 arm64; R-jsonlite prepared its branch and failed in the VM building its dependency R-Matrix, and p5-inline-python prepared its branch and failed at configure because Inline::Python 0.58 wants Python headers the port's python27 dependency does not give it; both are the ports' own failures, recorded as needs-attention with their logs. An explicit `bump p5-list-uniq 0.23`, a pure-perl module spelled `v0.21.0` in its Portfile, first refused the change because its metacpan homepage spells the version, which the fidelity check counted as an unexpected move; a homepage that follows the version now moves with it, since nothing is fetched from it. Run again, it resolved 0.230.0 and stopped at a 404: List-Uniq-0.23 was released by a different CPAN author, so the Portfile's author directory needs a person. `bump p5-dbd-pg` discovered 3.21.2 from CPAN, prepared its branch with fresh checksums, and failed to configure in the VM, DBD::Pg's Makefile.PL finding no libpq at the port's `POSTGRES_LIB`; the three failed contributions are left in the ports checkout with their logs, since each is the port's own problem to fix. The first job-store refusal of a derived spelling was found by these runs and fixed before they could get that far.

## Tests

`portfile`: the three commands. `source`: the livecheck spelling as the source version, the fallback when it is unevaluated, `regexm` accepted, `none` refused. `upstream`: a derived spelling discovered, checked, resolved explicitly, and reported current. `eval`: the compilers guard accepted, variable and nested substitutions refused; the extractor's page mode. `portedit`: a Portfile deriving its version from a spelling is edited to the spelling and fetched by it, and a release naming the wrong version is refused; a stub whose members own their livecheck keeps it. `sqlite`: the release validator's spellings. The whole suite passes.
