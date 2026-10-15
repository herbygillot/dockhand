# Ports listed in MacPorts' name order (v0.4.0 part 1, c)

The Status and outdated group's sort item: outdated's PORT column sorted
by bytes, so terraform-1.10 came before terraform-1.2. `macports.ComparePortNames`
reads a run of digits by its number, as a person does, and three lists a
person reads now sort by it: outdated's report (engine/outdated.go), the
dependents revbump and `--revbump-dependents` list (engine/ports.go), and
the assessed holds status and serve say of a branch (engine/servesubmit.go).

Test: TestOutdatedPortsArePreparedOneBranchEach (terraform-1.2 before
terraform-1.10).
