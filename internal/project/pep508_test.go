package project

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A requirement's environment marker is read as a MacPorts build on macOS
// would see it: what that environment settles is yes or no, and what it
// doesn't, a machine, an extra, or a Python version not known, is unknown,
// through and and or as three values (the helper-ownership review's
// finding 1).
func TestAMarkerIsReadForMacOS(t *testing.T) {
	t.Parallel()
	py313 := MacOS("3.13")
	for marker, want := range map[string]Applies{
		`sys_platform == 'win32'`:                                 No,
		`sys_platform == "darwin"`:                                Yes,
		`platform_system == 'Darwin' and os_name == 'posix'`:      Yes,
		`platform_system != 'Windows'`:                            Yes,
		`python_version < '3.10'`:                                 No,
		`python_version >= "3.9"`:                                 Yes,
		`python_full_version >= '3.13.0'`:                         Yes,
		`sys_platform == 'win32' or python_version >= '3.12'`:     Yes,
		`(sys_platform == 'linux' or sys_platform == 'win32')`:    No,
		`'darwin' in sys_platform`:                                Yes,
		`sys_platform not in 'win32 cygwin'`:                      Yes,
		`platform_machine == 'arm64'`:                             Unknown,
		`platform_machine == 'arm64' and sys_platform == 'win32'`: No,
		`platform_machine == 'arm64' or sys_platform == 'darwin'`: Yes,
		`extra == 'socks'`:                                        Unknown,
		`implementation_name == "cpython" and extra == "test"`:    Unknown,
	} {
		got, err := Evaluate(marker, py313)
		require.NoError(t, err, marker)
		require.Equal(t, want, got, marker)
	}
	got, err := Evaluate(`python_version < '3.10'`, MacOS(""))
	require.NoError(t, err)
	require.Equal(t, Unknown, got, "a Python version not known")
	for _, marker := range []string{`sys_platform ==`, `sys_platform == 'win32`, `(sys_platform == 'win32'`, `platform == 'x'`, `sys_platform = 'x'`, `sys_platform == 'x' ;`} {
		_, err := Evaluate(marker, py313)
		require.Error(t, err, marker)
	}
}
