package sourcecompare

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A specifier admits a version as Python's packaging 26.3 says,
// SpecifierSet(specifier).contains(version, prereleases=True), which
// produced every answer here.
func TestASpecifierAdmitsAsPackagingSays(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		specifier, version string
		admits             bool
	}{
		{"==0.19.0", "0.17.1", false},
		{"==0.19.0", "0.19.0", true},
		{"==0.19", "0.19.0", true},
		{"==0.19.0", "0.19", true},
		{"==0.19.*", "0.19.3", true},
		{"==0.19.*", "0.20", false},
		{"!=0.19.*", "0.20", true},
		{"!=0.19.0", "0.19.0", false},
		{">=0.18,<0.20", "0.19.3", true},
		{">=0.18,<0.20", "0.20.0", false},
		{">=1.0", "1.0rc1", false},
		{"<2.0", "2.0rc1", false},
		{"<2.0rc2", "2.0rc1", true},
		{">1.0", "1.0.post1", false},
		{">1.0.post1", "1.0.post2", true},
		{">1.0", "1.0.1", true},
		{"~=1.4", "1.9", true},
		{"~=1.4", "2.0", false},
		{"~=1.4.2", "1.4.9", true},
		{"~=1.4.2", "1.5.0", false},
		{"<=1.0", "1.0", true},
		{">=1.0", "1.0.dev1", false},
		{"<1.0", "1.0.dev1", false},
		{">=1.0a1", "1.0.dev3", false},
		{"==1.0", "1.0+local", true},
		{"==1.0+local", "1.0+local", true},
		{"", "3.2", true},
		{"==2024.1", "2024.01", true},
		{">=1!0.5", "2.0", false},
		{"===1.0", "1.0", true},
		{"===1.0", "1.0.0", false},
		{">=3.10", "3.9.18", false},
		{">=3.10", "3.14.0", true},
		{"~=1!1.4", "1.5", false},
		{"~=1!1.4", "1!1.5", true},
		{"~=1.4", "1!1.5", false},
		{"==1!1.*", "1.2", false},
	} {
		admits, err := Admits(test.specifier, test.version)
		require.NoError(t, err, "%s %s", test.specifier, test.version)
		require.Equal(t, test.admits, admits, "%q admits %q", test.specifier, test.version)
	}
	for _, test := range [][2]string{{"^1.2", "1.3"}, {"@ https://example.org/x.tar.gz", "1.0"}, {">=1.0", "not-a-version"}, {">=1.*", "1.0"}, {"~=1", "1.0"}} {
		_, err := Admits(test[0], test[1])
		require.Error(t, err, "%s %s: not read, and so neither yes nor no", test[0], test[1])
	}
	require.Equal(t, "textual-fastdatatable", NormalizeName("Textual_FastDataTable"))
	require.Equal(t, "zope-interface", NormalizeName("zope.interface"))
}
