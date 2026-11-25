package textedit

import (
	"slices"
	"strings"
	"testing"
)

// Match pairs two sequences by a longest common subsequence after their
// common ends, and past its budget calls the middle all changed rather
// than build a table that grows with the square of their lengths (Codex's
// review of 8e314b96, finding 5).
func TestMatchPairsWithinItsBudget(t *testing.T) {
	a := strings.Fields("a b c d e")
	b := strings.Fields("a x c d y e")
	from, within := Match(len(a), len(b), func(i, j int) bool { return a[i] == b[j] })
	if !within || !slices.Equal(from, []int{0, -1, 2, 3, -1, 4}) {
		t.Fatalf("Match = %v, %v", from, within)
	}
	if gone := Unmatched(len(a), from); !slices.Equal(gone, []int{1}) {
		t.Fatalf("Unmatched = %v", gone)
	}

	// Past the budget, the middle is changed, its common ends kept.
	n := 2_100
	long := make([]string, n)
	other := make([]string, n)
	for i := range long {
		long[i], other[i] = "a", "b"
	}
	long[0], other[0] = "same", "same"
	from, within = Match(len(long), len(other), func(i, j int) bool { return long[i] == other[j] })
	if within || from[0] != 0 || slices.ContainsFunc(from[1:], func(i int) bool { return i >= 0 }) {
		t.Fatalf("past the budget: within %v, from[0] %d", within, from[0])
	}
}

// Changed are the elements of each side a Match leaves unpaired: those
// gone from a and those come in b, by index, as the line comparisons of
// Portfiles and CMake files both read them (#150).
func TestChangedSaysWhatEachSideLeavesUnpaired(t *testing.T) {
	t.Parallel()
	a := []string{"one", "two", "three", "four"}
	b := []string{"one", "2", "three", "four", "five"}
	gone, come := Changed(len(a), len(b), func(i, j int) bool { return a[i] == b[j] })
	if !slices.Equal(gone, []int{1}) || !slices.Equal(come, []int{1, 4}) {
		t.Fatalf("Changed = %v, %v; want [1], [1 4]", gone, come)
	}
	gone, come = Changed(len(a), len(a), func(i, j int) bool { return a[i] == a[j] })
	if len(gone) != 0 || len(come) != 0 {
		t.Fatalf("Changed of a side with itself = %v, %v; want none", gone, come)
	}
}
