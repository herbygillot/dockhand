package prose

import (
	"testing"
	"time"

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

func TestFewSaysALongListByItsCount(t *testing.T) {
	ports := []string{"terraform-1.16", "terraform-1.17", "terraform-1.18", "terraform-1.19"}
	require.Equal(t, "terraform-1.16, terraform-1.17, terraform-1.18, terraform-1.19", Few(ports, 4, "port"))
	require.Equal(t, "4 ports: terraform-1.16, terraform-1.17, terraform-1.18 and 1 more", Few(ports, 3, "port"))
	require.Empty(t, Few(nil, 3, "port"))
}

func TestAndListsWordsAsASentenceDoes(t *testing.T) {
	require.Equal(t, "14", And([]string{"14"}))
	require.Equal(t, "14 and 15", And([]string{"14", "15"}))
	require.Equal(t, "13, 14, and 15", And([]string{"13", "14", "15"}))
	require.Empty(t, And(nil))
}

func TestCountSetsThousandsApart(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 2200: "2,200", 1234567: "1,234,567", -5000: "-5,000"} {
		require.Equal(t, want, Count(n))
	}
}

func TestTextHelpers(t *testing.T) {
	require.Equal(t, "One rule.", FirstSentence("One rule. Then more."))
	require.Equal(t, "no stop", FirstSentence("no stop"))
	require.Equal(t, "first", FirstLine("first\nsecond"))
	require.Equal(t, "a b\nc d", Wrap("a b c d", 3))
	require.Equal(t, []string{"two", "three"}, LastLines("one\r\n two \n\nthree\n", 2))
}

func TestTookIsShortEnoughForACell(t *testing.T) {
	t.Parallel()
	require.Equal(t, "38s", Took(38*time.Second))
	require.Equal(t, "4m 12s", Took(4*time.Minute+12*time.Second+400*time.Millisecond))
	require.Equal(t, "1h 3m", Took(time.Hour+3*time.Minute+20*time.Second))
}
