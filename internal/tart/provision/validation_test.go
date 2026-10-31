package provision

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// guestAnswers is a guest that answers each command line from answers, by
// its first words, finds clang as every image does, and fails any other.
func guestAnswers(answers map[string]string) func(context.Context, io.Reader, ...string) ([]byte, error) {
	return func(_ context.Context, _ io.Reader, args ...string) ([]byte, error) {
		line := strings.Join(args, " ")
		for prefix, answer := range answers {
			if strings.HasPrefix(line, prefix) {
				if failure, ok := strings.CutPrefix(answer, "fail:"); ok {
					return nil, &exec.ExitError{Stderr: []byte(failure)}
				}
				return []byte(answer), nil
			}
		}
		if strings.HasPrefix(line, "/usr/bin/xcrun --find clang") {
			return []byte("/usr/bin/clang\n"), nil
		}
		return nil, errors.New("not answered: " + line)
	}
}

// A prepared image is refused for what would make its checks unlike
// MacPorts' builders: no passwordless sudo, another package manager, and
// developer tools other than the image's, each said, off a Mac too, where
// it ran only against a real guest (the test analysis's step b).
func TestValidationRefusesAnImageUnlikeTheBuilders(t *testing.T) {
	t.Parallel()
	_, err := validate(t.Context(), guestAnswers(map[string]string{"sudo -n": "fail:a password is required"}), Config{})
	require.ErrorContains(t, err, "passwordless sudo is unavailable")

	_, err = validate(t.Context(), guestAnswers(map[string]string{"sudo -n": "", "/bin/sh -c for path": "/opt/homebrew\n/sw\n"}), Config{})
	require.EqualError(t, err, "foreign package manager found: /opt/homebrew, /sw")

	_, err = validateXcode(t.Context(), guestAnswers(map[string]string{"/usr/bin/xcode-select -p": "/Applications/Xcode.app/Contents/Developer\n", "/usr/bin/xcodebuild -version": "Xcode 26.6\nBuild version 17A1\n"}), Config{})
	require.EqualError(t, err, "base image selects unexpected developer directory /Applications/Xcode.app/Contents/Developer")
	_, err = validateXcode(t.Context(), guestAnswers(map[string]string{"/usr/bin/xcode-select -p": "/Library/Developer/CommandLineTools\n"}), Config{XcodeVersion: "26.6"})
	require.EqualError(t, err, "Xcode image selects unexpected developer directory /Library/Developer/CommandLineTools")
	_, err = validateXcode(t.Context(), guestAnswers(map[string]string{"/usr/bin/xcode-select -p": "/Applications/Xcode.app/Contents/Developer\n", "/usr/bin/xcodebuild -version": "Xcode 26.5\nBuild version 17A1\n"}), Config{XcodeVersion: "26.6"})
	require.EqualError(t, err, "image has Xcode 26.5; expected 26.6")
}
