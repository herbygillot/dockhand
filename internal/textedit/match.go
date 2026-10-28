package textedit

// MatchBudget is the most cells Match's table may take between two
// sequences' common ends: 4,000,000 int32s, 16 MB. Past it, the middle is
// all changed, which callers read as a change to hold for, never as
// sameness.
const MatchBudget = 4_000_000

// Match pairs two sequences' elements, a's n and b's m, by a longest
// common subsequence after their common start and end, same saying
// whether a[i] and b[j] are equal: from[j] is the index in a that b[j]
// keeps, or -1 for one b adds or alters. Where the middle's table would
// pass MatchBudget, every element of it is unmatched and within is
// false: the comparison's memory stays bounded, its answer "changed"
// (Codex's review of 8e314b96, finding 5, where a license comparison
// took 51 MB for a 25 KB pair). It's the one way the comparisons of
// lines and words match, each mapping the pairs to its own answer.
func Match(n, m int, same func(i, j int) bool) (from []int, within bool) {
	from = make([]int, m)
	for j := range from {
		from[j] = -1
	}
	start := 0
	for start < n && start < m && same(start, start) {
		from[start] = start
		start++
	}
	endA, endB := n, m
	for endA > start && endB > start && same(endA-1, endB-1) {
		endA--
		endB--
		from[endB] = endA
	}
	rows, columns := endA-start, endB-start
	if rows*columns > MatchBudget {
		return from, false
	}
	table := make([][]int32, rows+1)
	for i := range table {
		table[i] = make([]int32, columns+1)
	}
	for i := rows - 1; i >= 0; i-- {
		for j := columns - 1; j >= 0; j-- {
			if same(start+i, start+j) {
				table[i][j] = table[i+1][j+1] + 1
			} else {
				table[i][j] = max(table[i+1][j], table[i][j+1])
			}
		}
	}
	for i, j := 0, 0; i < rows && j < columns; {
		switch {
		case same(start+i, start+j):
			from[start+j] = start + i
			i, j = i+1, j+1
		case table[i+1][j] >= table[i][j+1]:
			i++
		default:
			j++
		}
	}
	return from, true
}

// Unmatched are the indices of a that from, Match's answer for b, keeps
// none of.
func Unmatched(n int, from []int) []int {
	kept := make([]bool, n)
	for _, i := range from {
		if i >= 0 {
			kept[i] = true
		}
	}
	var gone []int
	for i, k := range kept {
		if !k {
			gone = append(gone, i)
		}
	}
	return gone
}
