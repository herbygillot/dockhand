package outdated

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// Subports of one Portfile that share its version and source are looked
// up once, each with its own copy of the release found; one that differs
// in either is looked up apart (field testing at 6dd7745c: php and p5
// subports made most of ryandesign's 2,133).
func TestSubportsSharingASourceAreLookedUpOnce(t *testing.T) {
	port := func(version, project string) macports.PortInfo {
		return macports.PortInfo{Version: version, Options: map[string]string{"github.author": "php", "github.project": project}}
	}
	same := lookupKey("lang/php/Portfile", port("8.3.1", "php-src"))
	require.Equal(t, same, lookupKey("lang/php/Portfile", port("8.3.1", "php-src")))
	require.NotEqual(t, same, lookupKey("lang/php/Portfile", port("8.2.9", "php-src")), "another version")
	require.NotEqual(t, same, lookupKey("lang/php/Portfile", port("8.3.1", "pecl")), "another source")
	require.Empty(t, lookupKey("", port("8.3.1", "php-src")))

	shared := &sharedLookups{lookups: map[string]*sharedLookup{}}
	var asked atomic.Int32
	release := func(context.Context) (upstream.Result, error) {
		asked.Add(1)
		return upstream.Result{Assessment: upstream.UpdateAvailable, Release: &model.Release{Version: "8.3.2"}}, nil
	}
	var wg sync.WaitGroup
	results := make([]upstream.Result, 5)
	for i := range results {
		wg.Go(func() {
			var err error
			results[i], err = shared.do(t.Context(), same, release)
			require.NoError(t, err)
		})
	}
	wg.Wait()
	require.EqualValues(t, 1, asked.Load())
	for _, result := range results {
		require.Equal(t, "8.3.2", result.Release.Version)
	}
	results[1].Release.Version = "changed"
	require.Equal(t, "8.3.2", results[2].Release.Version, "each its own copy")

	// One cut short by its own context isn't shared.
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := shared.do(cancelled, "other", func(ctx context.Context) (upstream.Result, error) { return upstream.Result{}, ctx.Err() })
	require.ErrorIs(t, err, context.Canceled)
	result, err := shared.do(t.Context(), "other", release)
	require.NoError(t, err)
	require.Equal(t, "8.3.2", result.Release.Version, "asked again")
}
