package prose

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlural(t *testing.T) {
	t.Parallel()
	for want, got := range map[string]string{
		"1 port": Plural(1, "port"), "0 ports": Plural(0, "port"), "2 ports": Plural(2, "port"),
		"3 patches": Plural(3, "patch"), "2 boxes": Plural(2, "box"), "4 branches": Plural(4, "branch"),
	} {
		require.Equal(t, want, got)
	}
}

// Sizes are SI, as Finder and df -H count them (the library survey's
// finding, the person's choice of 2026-10-02).
func TestBytes(t *testing.T) {
	t.Parallel()
	for n, want := range map[int64]string{
		0: "0 bytes", 1: "1 byte", 999: "999 bytes", 1000: "1 KB", 1500: "1.5 KB", 12_000: "12 KB",
		820_000_000: "820 MB", 1_400_000_000: "1.4 GB", 30_000_000_000: "30 GB", 1 << 30: "1.1 GB",
		9_950_000: "9.9 MB", 2_000_000_000_000: "2 TB", 1024: "1 KB", 5_000_000_000_000_000: "5000 TB",
	} {
		require.Equal(t, want, Bytes(n), n)
	}
}
