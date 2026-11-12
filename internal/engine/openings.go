package engine

import (
	"slices"
	"time"

	"github.com/herbygillot/dockhand/internal/model"
)

// ServeAdmits reports whether serve may take a place today under its
// limit: the rows that count today are fewer than it. The check and the
// place are one transaction where the store makes them.
func ServeAdmits(rows []model.ServeOpening, day time.Time, limit int) bool {
	counted := 0
	for _, row := range rows {
		if row.Counts(day) {
			counted++
		}
	}
	return counted < limit
}

// ServeMaySend reports whether a create may be sent for the row at index:
// it is among the first rows that count today, oldest first, as many as
// the limit, which matters where the limit was lowered (case AM).
func ServeMaySend(rows []model.ServeOpening, index int, day time.Time, limit int) bool {
	var counting []int
	for i, row := range rows {
		if row.Counts(day) {
			counting = append(counting, i)
		}
	}
	slices.SortStableFunc(counting, func(a, b int) int { return rows[a].Since().Compare(rows[b].Since()) })
	at := slices.Index(counting, index)
	return at >= 0 && at < limit
}
