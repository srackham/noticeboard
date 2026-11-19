package admintable

import (
	"strings"
	"testing"
)

func TestGroupByRegionOrdersGroups(t *testing.T) {
	opts := GroupByRegion([]string{"Asia/Tokyo", "Europe/Kyiv", "UTC"}, "Asia/Tokyo")
	groups := GroupOptions(opts)
	names := make([]string, len(groups))
	for i, g := range groups {
		names[i] = g.Name
	}
	if strings.Join(names, ",") != "Asia,Europe,Other" {
		t.Errorf("groups = %v, want Asia, Europe, then Other last", names)
	}
	if !groups[0].Options[0].Selected {
		t.Error("current zone not marked selected")
	}
	if n := countSelected(opts); n != 1 {
		t.Errorf("selected options = %d, want 1", n)
	}
}

func TestGroupByRegionKeepsCurrentMissingFromList(t *testing.T) {
	groups := GroupOptions(GroupByRegion([]string{"Asia/Tokyo"}, "Mars/Olympus"))
	if len(groups) != 2 || groups[1].Name != "Mars" {
		t.Fatalf("extra zone not added as its own group: %v", groups)
	}
	if !groups[1].Options[0].Selected || groups[1].Options[0].Value != "Mars/Olympus" {
		t.Error("current zone not shown and selected")
	}
}

func TestGroupByRegionDeduplicates(t *testing.T) {
	opts := GroupByRegion([]string{"UTC", "UTC", "Asia/Tokyo"}, "")
	if len(opts) != 2 {
		t.Errorf("options = %v, want the duplicate removed", opts)
	}
}

func TestGroupOptionsKeepsOrder(t *testing.T) {
	groups := GroupOptions([]Option{
		{Group: "A", Value: "a1"},
		{Group: "A", Value: "a2"},
		{Group: "B", Value: "b1"},
	})
	if len(groups) != 2 || len(groups[0].Options) != 2 || groups[1].Name != "B" {
		t.Errorf("GroupOptions = %+v, want A with two options, then B", groups)
	}
	if got := GroupOptions(nil); got != nil {
		t.Errorf("GroupOptions(nil) = %v, want nil", got)
	}
}

func countSelected(opts []Option) int {
	n := 0
	for _, o := range opts {
		if o.Selected {
			n++
		}
	}
	return n
}
