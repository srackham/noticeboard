package paging

import (
	"math"
	"testing"
)

func TestClamp(t *testing.T) {
	tests := []struct {
		name                string
		requested, total    int
		size                int
		wantPage, wantPages int
	}{
		{name: "empty table is page 1 of 1", requested: 1, total: 0, size: 20, wantPage: 1, wantPages: 1},
		{name: "empty table, requested page 5", requested: 5, total: 0, size: 20, wantPage: 1, wantPages: 1},
		{name: "first page", requested: 1, total: 45, size: 20, wantPage: 1, wantPages: 3},
		{name: "middle page", requested: 2, total: 45, size: 20, wantPage: 2, wantPages: 3},
		{name: "last page", requested: 3, total: 45, size: 20, wantPage: 3, wantPages: 3},
		{name: "beyond the last page", requested: 9, total: 45, size: 20, wantPage: 3, wantPages: 3},
		{name: "LastPage sentinel", requested: LastPage, total: 45, size: 20, wantPage: 3, wantPages: 3},
		{name: "LastPage on an empty table", requested: LastPage, total: 0, size: 20, wantPage: 1, wantPages: 1},
		{name: "page zero", requested: 0, total: 45, size: 20, wantPage: 1, wantPages: 3},
		{name: "negative page", requested: -3, total: 45, size: 20, wantPage: 1, wantPages: 3},
		{name: "exact multiple", requested: 2, total: 40, size: 20, wantPage: 2, wantPages: 2},
		{name: "one row", requested: 1, total: 1, size: 20, wantPage: 1, wantPages: 1},
		{name: "size of one", requested: 3, total: 3, size: 1, wantPage: 3, wantPages: 3},
		{name: "negative total", requested: 1, total: -5, size: 20, wantPage: 1, wantPages: 1},
		{name: "zero size does not divide by zero", requested: 2, total: 10, size: 0, wantPage: 2, wantPages: 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, pages := Clamp(tt.requested, tt.total, tt.size)
			if page != tt.wantPage || pages != tt.wantPages {
				t.Errorf("Clamp(%d, %d, %d) = %d, %d; want %d, %d",
					tt.requested, tt.total, tt.size, page, pages, tt.wantPage, tt.wantPages)
			}
		})
	}
}

func TestLastPageIsMaxInt(t *testing.T) {
	if LastPage != math.MaxInt {
		t.Errorf("LastPage = %d, want math.MaxInt", LastPage)
	}
}
