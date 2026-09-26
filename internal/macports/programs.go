package macports

import (
	"regexp"
	"strconv"
	"strings"
)

// The programs evaluation may run are kept in one place, as the options it
// reads are: the evaluator's dispatcher is given HostPrograms and refuses
// every other program a Portfile or Base runs while a port is evaluated
// (docs/oracle.md, phase 2). The hook grammar refuses exec outright today;
// if it comes to admit programs, this is the table it shares.

// ProgramForm says how a host program's arguments are judged.
type ProgramForm string

const (
	// FormAny admits any arguments: the program only reports.
	FormAny ProgramForm = "any"
	// FormOnly admits arguments that are each one of the program's
	// Options; with no Options, it admits the program alone.
	FormOnly ProgramForm = "only"
	// FormWrapper admits leading Options, then a command line judged in
	// its own right, as env runs one.
	FormWrapper ProgramForm = "wrapper"
	// FormSubcommand admits leading Options, then one of Subcommands with
	// any arguments, as git's are.
	FormSubcommand ProgramForm = "subcommand"
)

// ProgramOption is an argument a host program admits: Pattern matches the
// whole argument, as a regular expression, and Values is how many
// arguments after it it takes as its values, whatever they are.
type ProgramOption struct {
	Pattern string
	Values  int
}

// HostProgram is a program evaluation may run on the host, one that only
// reports on it, in the forms its arguments are admitted in.
type HostProgram struct {
	// Name matches the program's base name, as a regular expression.
	Name        string
	Form        ProgramForm
	Options     []ProgramOption
	Subcommands []string
	// Refused match arguments no form admits, as regular expressions.
	Refused []string
}

func flags(patterns ...string) []ProgramOption {
	options := make([]ProgramOption, len(patterns))
	for i, pattern := range patterns {
		options[i] = ProgramOption{Pattern: pattern}
	}
	return options
}

func valued(pattern string) ProgramOption { return ProgramOption{Pattern: pattern, Values: 1} }

// HostPrograms are the programs evaluation may run. Each is one the
// 2026-09-23 host inventory found Portfiles, PortGroups, or Base running
// while ports were evaluated, or a query of the toolchain beside them, in
// the forms that only report: xcrun finding a tool but not running one,
// java_home without --exec, an interpreter asked its version but not
// given code.
var HostPrograms = []HostProgram{
	{Name: `java_home`, Form: FormOnly, Options: append(flags(`-V`, `--verbose`, `-f`, `-F`, `--failfast`, `-X`, `--xml`),
		valued(`-v`), valued(`--version`), valued(`-a`), valued(`--arch`), valued(`-d`), valued(`--datamodel`))},
	{Name: `xcrun`, Form: FormOnly, Options: append(flags(`--show-sdk-path`, `--show-sdk-version`, `--show-sdk-build-version`, `--show-sdk-platform-path`, `--show-sdk-platform-version`, `--version`, `-v`, `--verbose`, `-n`, `--no-cache`),
		valued(`--sdk`), valued(`-sdk`), valued(`--toolchain`), valued(`--find`), valued(`-f`))},
	{Name: `xcode-select`, Form: FormOnly, Options: flags(`-p`, `--print-path`, `-v`, `--version`)},
	{Name: `xcodebuild`, Form: FormOnly, Options: append(flags(`-version`, `-showsdks`, `ProductBuildVersion`, `ProductVersion`, `Path`, `PlatformPath`, `SDKVersion`), valued(`-sdk`))},
	{Name: `sw_vers`, Form: FormAny},
	{Name: `uname`, Form: FormAny},
	{Name: `getconf`, Form: FormAny},
	{Name: `echo|printf|true|false`, Form: FormAny},
	{Name: `machine|arch|hostname`, Form: FormOnly},
	{Name: `sysctl`, Form: FormOnly, Options: flags(`-n`, `-i`, `-in`, `-ni`, `-N`, `-e`, `-h`, `-b`, `-a`, `-o`, `-x`, `[A-Za-z0-9_.]+`)},
	{Name: `env`, Form: FormWrapper, Options: append(flags(`-i`, `[A-Za-z_][A-Za-z0-9_]*=.*`), valued(`-u`))},
	{Name: `git`, Form: FormSubcommand, Options: append(flags(`--no-pager`, `--git-dir=.*`, `--work-tree=.*`), valued(`-c`), valued(`-C`)),
		Subcommands: []string{"rev-parse", "status", "log", "describe"}, Refused: []string{`--output.*`}},
	{Name: `(clang\+\+|clang|gcc|g\+\+|cc|c\+\+)(-mp-[0-9.]+|-[0-9.]+)?`, Form: FormOnly, Options: flags(`--version`, `-v`, `-dumpversion`, `-dumpmachine`, `-print-.*`, `--print-.*`)},
	{Name: `rustc`, Form: FormOnly, Options: append(flags(`--version`, `-V`, `-vV`, `--print=.*`), valued(`--print`))},
	{Name: `perl(5(\.[0-9]+)*)?`, Form: FormOnly, Options: flags(`-v`, `-V`, `-V:.*`)},
	{Name: `ruby([0-9.]+)?`, Form: FormOnly, Options: flags(`--version`)},
	{Name: `python([0-9.]+)?`, Form: FormOnly, Options: flags(`--version`, `-V`)},
	{Name: `pkg-config`, Form: FormOnly, Options: flags(`--version`, `--modversion`, `--cflags`, `--cflags-only-I`, `--libs`, `--libs-only-l`, `--libs-only-L`, `--static`, `--exists`, `--print-variables`,
		`--atleast-version=.*`, `--exact-version=.*`, `--max-version=.*`, `--variable=.*`, `[^-].*`)},
	{Name: `llvm-config(-mp-[0-9.]+)?`, Form: FormOnly, Options: flags(`--version`, `--prefix`, `--bindir`, `--libdir`, `--includedir`, `--cflags`, `--cppflags`, `--cxxflags`, `--ldflags`, `--libs`, `--system-libs`, `--libfiles`,
		`--components`, `--targets-built`, `--host-target`, `--shared-mode`, `--link-shared`, `--link-static`, `--has-rtti`, `--assertion-mode`, `--build-mode`, `--cmakedir`, `--obj-root`, `--src-root`, `[a-z0-9]+`)},
}

// ValidHostPrograms reports the first entry of programs whose patterns do
// not compile, or whose form is unknown, as its index and a reason; -1 if
// every entry is sound.
func ValidHostPrograms(programs []HostProgram) (int, string) {
	for i, program := range programs {
		patterns := []string{program.Name}
		for _, option := range program.Options {
			patterns = append(patterns, option.Pattern)
		}
		patterns = append(patterns, program.Refused...)
		for _, pattern := range patterns {
			if _, err := regexp.Compile("^(?:" + pattern + ")$"); err != nil {
				return i, err.Error()
			}
		}
		switch program.Form {
		case FormAny, FormOnly, FormWrapper:
			if len(program.Subcommands) > 0 {
				return i, "subcommands outside the subcommand form"
			}
		case FormSubcommand:
			if len(program.Subcommands) == 0 {
				return i, "the subcommand form without subcommands"
			}
		default:
			return i, "unknown form " + strconv.Quote(string(program.Form))
		}
	}
	return -1, ""
}

// TclHostPrograms renders programs as the Tcl list the evaluator's
// dispatcher reads: one dictionary per program.
func TclHostPrograms(programs []HostProgram) string {
	var entries []string
	for _, program := range programs {
		var options []string
		for _, option := range program.Options {
			options = append(options, tclList(option.Pattern, strconv.Itoa(option.Values)))
		}
		entries = append(entries, tclList("name", program.Name, "form", string(program.Form),
			"options", tclList(options...), "subcommands", tclList(program.Subcommands...), "refused", tclList(program.Refused...)))
	}
	return tclList(entries...)
}

// tclList quotes each element in braces, which the patterns above never
// unbalance, so the list reads back element for element.
func tclList(elements ...string) string {
	quoted := make([]string, len(elements))
	for i, element := range elements {
		quoted[i] = "{" + element + "}"
	}
	return strings.Join(quoted, " ")
}
