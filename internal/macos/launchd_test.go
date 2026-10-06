package macos

import (
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLaunchdPlistPreservesArgumentAndEnvironmentValues(t *testing.T) {
	special := `/path with spaces/<literal>&"value"`
	data := LaunchdPlist("label", []string{special, "one argument"}, special, map[string]string{"Z": special, "A": "first"})
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	var values []string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "string" {
			var value string
			require.NoError(t, decoder.DecodeElement(&value, &start))
			values = append(values, value)
		}
	}
	require.Equal(t, []string{"label", special, "one argument", special, special, "first", special}, values)
	require.Contains(t, string(data), "<key>KeepAlive</key><false/>")
	require.Contains(t, string(data), "<key>RunAtLoad</key><true/>")
}

// A job kept alive in the background, as serve's agent is, says so, and
// escapes its environment's keys as well as its values, which serve's
// own writer didn't (the library survey's finding 3).
func TestALaunchdJobKeptAliveEscapesEveryKey(t *testing.T) {
	data := string(LaunchdJob{Label: "serve", Arguments: []string{"dockhand", "serve"}, Log: "/tmp/serve.log", Environment: map[string]string{"ODD<KEY>": "v&w"}, KeepAlive: true, ProcessType: "Background"}.Plist())
	require.Contains(t, data, "<key>KeepAlive</key><true/>")
	require.Contains(t, data, "<key>ProcessType</key><string>Background</string>")
	require.Contains(t, data, "<key>ODD&lt;KEY&gt;</key><string>v&amp;w</string>")
	require.NotContains(t, string(LaunchdPlist("one-shot", nil, "/tmp/log", nil)), "ProcessType", "a one-shot job's plist is as it was")
}

// A job kept alive while a path exists says so as launchd.plist's
// PathState, and stays a property list plutil reads (the rc6 full stage,
// A8).
func TestALaunchdJobKeptAliveWhileItsProgramIsThere(t *testing.T) {
	data := LaunchdJob{Label: "serve", Arguments: []string{"/opt/local/bin/dockhand", "serve"}, Log: "/tmp/serve.log", KeepAlive: true, KeepAliveWhile: "/opt/local/bin/dockhand&"}.Plist()
	require.Contains(t, string(data), "<key>KeepAlive</key><dict><key>PathState</key><dict><key>/opt/local/bin/dockhand&amp;</key><true/></dict></dict>")
	if plutil, err := exec.LookPath("plutil"); err == nil {
		file := filepath.Join(t.TempDir(), "serve.plist")
		require.NoError(t, os.WriteFile(file, data, 0o644))
		out, err := exec.Command(plutil, "-lint", file).CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
}
