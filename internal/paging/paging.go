// Package paging holds the page arithmetic shared by the table repository and
// the administration handlers (DESIGN.md section 5.2). It has no dependencies
// and no I/O, so the clamping rule is written once.
package paging

// LastPage is a sentinel page number. Clamp turns it into the last page, which
// is how a create shows the page holding the new record.
const LastPage = int(^uint(0) >> 1) // the largest int

// Clamp returns the page to show and the number of pages for a table of total
// rows. The page count is at least 1, so an empty table is page 1 of 1. A
// requested page outside the range is moved to the nearest end.
func Clamp(requested, total, size int) (page, pages int) {
	if size < 1 {
		size = 1
	}
	if total < 0 {
		total = 0
	}
	pages = (total + size - 1) / size
	if pages < 1 {
		pages = 1
	}
	page = requested
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	return page, pages
}
