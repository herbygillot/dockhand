package macos

import (
	"encoding/xml"
	"io"
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
