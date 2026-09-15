package macos

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

// Command executes on an explicitly chosen host or guest, preserving exit errors.
type Command func(context.Context, io.Reader, ...string) ([]byte, error)

type DeveloperTools struct {
	Directory    string
	XcodeVersion string
	Problems     []string
}

func (t DeveloperTools) CommandLineTools() bool {
	return t.Directory == "/Library/Developer/CommandLineTools"
}
func (t DeveloperTools) Xcode() bool {
	return filepath.IsAbs(t.Directory) && strings.HasSuffix(t.Directory, ".app/Contents/Developer")
}

// InspectDeveloperTools only observes the target. Failed probes are reported as
// problems; transport failures and cancellation are returned as errors.
func InspectDeveloperTools(ctx context.Context, command Command) (DeveloperTools, error) {
	result := DeveloperTools{}
	problems := []string{}
	run := func(input io.Reader, label string, args ...string) ([]byte, error) {
		output, err := command(ctx, input, args...)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil {
			return output, nil
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return nil, err
		}
		problems = append(problems, label+": "+err.Error())
		return nil, nil
	}
	selected, err := run(nil, "developer tools are unavailable", "/usr/bin/xcode-select", "-p")
	if err != nil {
		return result, err
	}
	developerDirectory := strings.TrimSpace(string(selected))
	result.Directory = developerDirectory
	if result.Xcode() {
		xcode, err := run(nil, "Xcode is unavailable", "/usr/bin/xcodebuild", "-version")
		if err != nil {
			return result, err
		}
		first, _, _ := strings.Cut(strings.TrimSpace(string(xcode)), "\n")
		if version, ok := strings.CutPrefix(first, "Xcode "); ok && version != "" {
			result.XcodeVersion = version
		} else if xcode != nil {
			problems = append(problems, "xcodebuild returned an unrecognized version: "+strings.TrimSpace(string(xcode)))
		}
	} else if !result.CommandLineTools() {
		if selected != nil {
			problems = append(problems, "unexpected developer directory: "+developerDirectory)
		}
	}
	if _, err = run(nil, "compiler is unavailable", "/usr/bin/xcrun", "--find", "clang"); err != nil {
		return result, err
	}

	result.Problems = problems
	return result, nil
}
