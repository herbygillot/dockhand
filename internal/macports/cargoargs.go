package macports

import (
	"strings"

	"github.com/herbygillot/dockhand/internal/project"
)

// CargoBuildArgs are the features and packages a cargo port's build asks
// for, read from its evaluated build.cmd, build.pre_args, and build.args:
// --features (or -F) a,b, --features=a,b, a quoted "a b", each repeated;
// --all-features; --no-default-features; and -p or --package, each with
// its name. 22 of the tree's cargo ports pass --features or
// --all-features: gitui's {--features "vendor-openssl"}, taplo's
// --features=lsp, stylua's five (the omnidiff session's note 1). A build
// that names none builds with default features.
func CargoBuildArgs(options map[string]string) project.CargoFeatures {
	var found project.CargoFeatures
	var args []string
	for _, option := range []string{"build.cmd", "build.pre_args", "build.args"} {
		args = append(args, shellWords(options[option])...)
	}
	features := func(value string) {
		found.Features = append(found.Features, strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })...)
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		next := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch {
		case arg == "--all-features":
			found.All = true
		case arg == "--no-default-features":
			found.NoDefault = true
		case arg == "--features" || arg == "-F":
			features(next())
		case strings.HasPrefix(arg, "--features="):
			features(strings.TrimPrefix(arg, "--features="))
		case arg == "-p" || arg == "--package":
			if name := next(); name != "" {
				found.Packages = append(found.Packages, name)
			}
		case strings.HasPrefix(arg, "--package="):
			found.Packages = append(found.Packages, strings.TrimPrefix(arg, "--package="))
		}
	}
	return found
}

// shellWords splits an evaluated option's value into the words the shell
// passes cargo: a Tcl list's braces dropped, as the list's string form
// keeps them, and a quoted run one word, its quotes dropped.
func shellWords(value string) []string {
	var words []string
	var word strings.Builder
	started := false
	var quote rune
	for _, r := range value {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			word.WriteRune(r)
		case r == '"' || r == '\'':
			quote, started = r, true
		case r == '{' || r == '}' || r == ' ' || r == '\t' || r == '\n':
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if started {
		words = append(words, word.String())
	}
	return words
}
