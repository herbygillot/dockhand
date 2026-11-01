package macports

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The programs evaluation may run are kept in one place, as the options it
// reads are: the evaluator's dispatcher is given HostPrograms and refuses
// every other program a Portfile or Base runs while a port is evaluated
// (docs/oracle.md, phase 2), and the pre-fetch hook grammar admits a hook's
// exec of them by the same rules (CommandLineRefusal). The registry
// aliases Base gives a worker are kept beside them (WorkerAliases), given
// to the dispatcher and the hook grammar alike.

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

// AliasAnswer says how the evaluator's dispatcher answers one of the
// registry and installation aliases Base gives a port's worker, and so
// whether the pre-fetch hook grammar admits it.
type AliasAnswer string

const (
	// AliasWrites changes the registry: the dispatcher refuses it, and a
	// hook that calls it is refused.
	AliasWrites AliasAnswer = "writes"
	// AliasRegistry asks the registry what is installed: the dispatcher
	// answers it as the installation a port is evaluated against holds it,
	// an empty registry in Base's words for the fresh one.
	AliasRegistry AliasAnswer = "registry"
	// AliasSearch looks for a file along a list of directories, as a
	// path:, bin:, or lib: dependency is tested: the dispatcher answers it
	// over the installation's files, as it does findBinary.
	AliasSearch AliasAnswer = "search"
	// AliasPort reads another port Base has open, by its handle, from that
	// port's worker, which the dispatcher guards in its own right: the
	// dispatcher passes it to Base and counts it.
	AliasPort AliasAnswer = "port"
)

// WorkerAlias is a command Base aliases into a port's worker interpreter
// to reach the registry or what is installed, and how it is answered.
type WorkerAlias struct {
	Name   string
	Answer AliasAnswer
}

// WorkerAliases are the registry_ and _ aliases Base gives a port's
// worker, the 25 2.12.7's worker_init makes (macports.tcl), in one place:
// the evaluator's dispatcher guards each and answers it as Answer says,
// and the pre-fetch hook grammar admits a hook's call of each that
// doesn't write. A test of a recording session holds the list to the
// aliases the Base it runs against gives.
var WorkerAliases = []WorkerAlias{
	{"registry_new", AliasWrites}, {"registry_write", AliasWrites}, {"registry_prop_store", AliasWrites},
	{"registry_activate", AliasWrites}, {"registry_deactivate", AliasWrites}, {"registry_deactivate_composite", AliasWrites},
	{"registry_install", AliasWrites}, {"registry_uninstall", AliasWrites}, {"registry_register_deps", AliasWrites},
	{"registry_bulk_register_files", AliasWrites},
	{"registry_open", AliasRegistry}, {"registry_prop_retr", AliasRegistry}, {"registry_exists", AliasRegistry},
	{"registry_exists_for_name", AliasRegistry}, {"registry_fileinfo_for_index", AliasRegistry}, {"registry_fileinfo_for_file", AliasRegistry},
	{"registry_active", AliasRegistry}, {"registry_file_registered", AliasRegistry}, {"registry_port_registered", AliasRegistry},
	{"registry_list_depends", AliasRegistry}, {"_portnameactive", AliasRegistry},
	{"_mportsearchpath", AliasSearch},
	{"_mportkey", AliasPort}, {"_mport_archs", AliasPort}, {"_mport_supports_archs", AliasPort},
}

// WorkerAliasReads are the names of the worker aliases that read and
// change nothing, which a pre-fetch hook may call.
func WorkerAliasReads() []string {
	var names []string
	for _, alias := range WorkerAliases {
		if alias.Answer != AliasWrites {
			names = append(names, alias.Name)
		}
	}
	return names
}

// TclWorkerAliases renders aliases as the Tcl dictionary the evaluator's
// dispatcher reads: each name and how it is answered.
func TclWorkerAliases(aliases []WorkerAlias) string {
	var entries []string
	for _, alias := range aliases {
		entries = append(entries, alias.Name, string(alias.Answer))
	}
	return tclList(entries...)
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

// Argument is one word of a command line as source text shows it: its
// text where it is literal, or a substitution whose value can't be known
// until it runs.
type Argument struct {
	Text    string
	Literal bool
}

// CommandLineRefusal judges a command line in exec's syntax against
// HostPrograms, as the evaluator's dispatcher does when it runs, for code
// that is judged before it runs, such as a pre-fetch hook: every program
// of the pipeline admitted, output only to /dev/null or a channel, and
// nothing left in the background. It is empty for an admitted line, and
// otherwise says why not. A substituted word is admitted only as the value
// an option takes, never as a program, an option, or a redirection.
func CommandLineRefusal(words []Argument) string {
	i := 0
	for i < len(words) && words[i].Literal && slices.Contains([]string{"-ignorestderr", "-keepnewline", "--"}, words[i].Text) {
		i++
		if words[i-1].Text == "--" {
			break
		}
	}
	words = words[i:]
	if len(words) > 0 && words[len(words)-1].Literal && words[len(words)-1].Text == "&" {
		return "leaves a process running"
	}
	var stage []Argument
	judge := func() string {
		if len(stage) == 0 {
			return ""
		}
		return programRefusal(stage)
	}
	for i := 0; i < len(words); i++ {
		word := words[i]
		if word.Literal && (word.Text == "|" || word.Text == "|&") {
			if reason := judge(); reason != "" {
				return reason
			}
			stage = nil
			continue
		}
		operator := ""
		if word.Literal {
			for _, candidate := range []string{"2>@1", ">&@", "2>@", ">@", "<@", "<<", "<", ">>&", "2>>", ">>", ">&", "2>", ">"} {
				if strings.HasPrefix(word.Text, candidate) {
					operator = candidate
					break
				}
			}
		}
		if operator == "" {
			stage = append(stage, word)
			continue
		}
		if operator == "2>@1" {
			continue
		}
		target := Argument{Text: strings.TrimPrefix(word.Text, operator), Literal: true}
		if target.Text == "" {
			i++
			if i >= len(words) {
				return "redirects to nothing"
			}
			target = words[i]
		}
		switch operator {
		case ">", "2>", ">&", ">>", "2>>", ">>&":
			if !target.Literal {
				return "writes to a computed file"
			}
			if target.Text != "/dev/null" {
				return "writes " + target.Text
			}
		}
	}
	return judge()
}

func programRefusal(command []Argument) string {
	if !command[0].Literal {
		return "runs a computed program"
	}
	name := command[0].Text[strings.LastIndexByte(command[0].Text, '/')+1:]
	arguments := command[1:]
	for _, program := range HostPrograms {
		if !matches(program.Name, name) {
			continue
		}
		for _, pattern := range program.Refused {
			for _, argument := range arguments {
				if argument.Literal && matches(pattern, argument.Text) {
					return "runs " + name + " with " + argument.Text
				}
			}
		}
		rest := arguments[admitted(program.Options, arguments):]
		switch program.Form {
		case FormAny:
			for _, argument := range rest {
				if !argument.Literal {
					return "runs " + name + " with a computed argument"
				}
			}
			return ""
		case FormWrapper:
			if len(rest) > 0 {
				return programRefusal(rest)
			}
			return ""
		case FormSubcommand:
			if len(rest) > 0 && rest[0].Literal && slices.Contains(program.Subcommands, rest[0].Text) {
				// After the subcommand, a computed word could be one the
				// program refuses.
				for _, argument := range rest[1:] {
					if !argument.Literal && len(program.Refused) > 0 {
						return "runs " + name + " with a computed argument"
					}
				}
				return ""
			}
			if len(rest) > 0 && rest[0].Literal {
				return "runs " + name + " " + rest[0].Text
			}
			return "runs " + name + " without a subcommand it admits"
		}
		if len(rest) > 0 {
			if !rest[0].Literal {
				return "runs " + name + " with a computed argument"
			}
			return "runs " + name + " with " + rest[0].Text
		}
		return ""
	}
	return "runs " + name + ", which is not known to only report"
}

// admitted is how many of arguments, from the first, options admit with
// the values each takes, whatever they are; a computed word where an
// option would be stops it.
func admitted(options []ProgramOption, arguments []Argument) int {
	i := 0
	for i < len(arguments) && arguments[i].Literal {
		next := i
		for _, option := range options {
			if matches(option.Pattern, arguments[i].Text) {
				next = i + 1 + option.Values
				break
			}
		}
		if next == i {
			break
		}
		i = min(next, len(arguments))
	}
	return i
}

func matches(pattern, text string) bool {
	matched, err := regexp.MatchString("^(?:"+pattern+")$", text)
	return err == nil && matched
}
