package cli

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestTicketReferencesBecomeTracURLsAndURLsPassThrough(t *testing.T) {
	t.Parallel()
	flags := referenceFlags{closes: []string{"74379", " #74422 "}, see: []string{"https://github.com/macports/macports-ports/pull/34756"}}
	references, err := flags.resolve()
	require.NoError(t, err)
	require.Equal(t, []record.Reference{
		{Relation: record.ReferenceCloses, URL: "https://trac.macports.org/ticket/74379"},
		{Relation: record.ReferenceCloses, URL: "https://trac.macports.org/ticket/74422"},
		{Relation: record.ReferenceSee, URL: "https://github.com/macports/macports-ports/pull/34756"},
	}, references)
	for _, value := range []string{"", "#", "ticket 74379", "trac.macports.org/ticket/74379", "ftp://example.invalid/1", "https://example.invalid/a b"} {
		_, err := (&referenceFlags{see: []string{value}}).resolve()
		require.ErrorContains(t, err, "--see takes a Trac ticket number or a URL", "%q", value)
	}
	empty, err := (&referenceFlags{}).resolve()
	require.NoError(t, err)
	require.Nil(t, empty)
}
