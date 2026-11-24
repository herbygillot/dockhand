package macports_test

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/stretchr/testify/require"
)

// A distfile's :tag is cut as Base's getdistname and getdisttag cut it
// (port1.0/portutil.tcl): after the last colon, where what follows is
// letters, digits, '_' or '-' and something precedes it. Three of
// dockhand's readers cut at the first colon and one at the last (#150).
func TestADistfilesTagIsCutAsBaseCutsIt(t *testing.T) {
	t.Parallel()
	for file, want := range map[string][2]string{
		"jq-1.8.tar.gz":        {"jq-1.8.tar.gz", ""},
		"jq-1.8.tar.gz:github": {"jq-1.8.tar.gz", "github"},
		"a:b:c":                {"a:b", "c"},
		"file.tar.gz:my.site":  {"file.tar.gz:my.site", ""},
		":tag":                 {":tag", ""},
		"x.zip:site_2-b":       {"x.zip", "site_2-b"},
	} {
		name, tag := macports.DistfileTag(file)
		require.Equal(t, want, [2]string{name, tag}, file)
	}
}
