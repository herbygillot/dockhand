package buildlog

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Base names the release as Apple did when it shipped, and a GitHub
// runner's header may lack a line, as macos-26's had no MacPorts line in
// macports/macports-ports run 42565046877 (#100).
func TestReadHeaderReadsEveryNameBaseGivesTheRelease(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		log    string
		want   Header
		wanted bool
	}{
		{"DEBUG: OS darwin/25.6.0 (macOS 26.7) arch i386\nDEBUG: macOS 26.7 (darwin/25.6.0) arch i386\nDEBUG: MacPorts 2.12.6\nDEBUG: Xcode 26.6, CLT 26.6.0.0.1781586589\nDEBUG: SDK 26\n",
			Header{MacOS: "26.7", Darwin: 25, Arch: "i386", MacPorts: "2.12.6", Xcode: "26.6", Tools: "26.6.0.0.1781586589", SDK: "26"}, true},
		{"DEBUG: OS X 10.11.6 (darwin/15.6.0) arch i386\r\nDEBUG: MacPorts 2.12.6\r\nDEBUG: Xcode 8.2.1, CLT 8.2.0.0.1.1480973914\r\nDEBUG: SDK 10.11\r\n",
			Header{MacOS: "10.11.6", Darwin: 15, Arch: "i386", MacPorts: "2.12.6", Xcode: "8.2.1", Tools: "8.2.0.0.1.1480973914", SDK: "10.11"}, true},
		{"DEBUG: Mac OS X 10.6.8 (darwin/10.8.0) arch i386\nDEBUG: MacPorts 2.12.6\nDEBUG: Xcode 3.2.6, CLT none\nDEBUG: SDK 10.6\n",
			Header{MacOS: "10.6.8", Darwin: 10, Arch: "i386", MacPorts: "2.12.6", Xcode: "3.2.6", Tools: "none", SDK: "10.6"}, true},
		{"DEBUG: macOS 26.7 (darwin/25.6.0) arch i386\nDEBUG: MacPorts 2.12.6\n", Header{MacOS: "26.7", Darwin: 25, Arch: "i386", MacPorts: "2.12.6"}, false},
		{"DEBUG: macOS 26.7.2 (darwin/25.6.0) arch arm\nDEBUG: Xcode 26.4.1, CLT 26.6.0.0.1781586589\nDEBUG: SDK 26\n",
			Header{MacOS: "26.7.2", Darwin: 25, Arch: "arm", Xcode: "26.4.1", Tools: "26.6.0.0.1781586589", SDK: "26"}, false},
	} {
		got, ok := ReadHeader(strings.NewReader(c.log))
		require.Equal(t, c.wanted, ok, c.log)
		require.Equal(t, c.want, got, c.log)
	}
	require.Equal(t, "arm64", Header{Arch: "arm"}.Architecture())
	require.Equal(t, "x86_64", Header{Arch: "i386"}.Architecture())
	require.Empty(t, Header{}.Architecture())
	xcode, tools := Header{Xcode: "3.2.6", Tools: "none"}.DeveloperTools()
	require.Equal(t, []string{"3.2.6", ""}, []string{xcode, tools})
}
