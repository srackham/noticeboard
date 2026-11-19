package admintable

import (
	"sort"
	"strings"
)

// Group is one optgroup of a select field.
type Group struct {
	Name    string
	Options []Option
}

// GroupByRegion returns select options for zone names, grouped by region.
// "Europe/London" falls under "Europe". A name with no segment, such as "UTC",
// falls under "Other", which is listed last. current is always included, so a
// stored value that the list does not offer is still shown and marked
// selected. The options are ordered by group, then by value.
func GroupByRegion(names []string, current string) []Option {
	seen := make(map[string]bool, len(names)+1)
	var all []string
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			all = append(all, n)
		}
	}
	if current != "" && !seen[current] {
		all = append(all, current)
	}
	sort.Strings(all)

	byGroup := make(map[string][]Option, len(all))
	var groupNames []string
	for _, n := range all {
		group := "Other"
		if i := strings.IndexByte(n, '/'); i >= 0 {
			group = n[:i]
		}
		if _, ok := byGroup[group]; !ok {
			groupNames = append(groupNames, group)
		}
		byGroup[group] = append(byGroup[group], Option{Group: group, Value: n, Selected: n == current})
	}

	sort.Slice(groupNames, func(i, j int) bool {
		switch {
		case groupNames[i] == "Other":
			return false
		case groupNames[j] == "Other":
			return true
		}
		return groupNames[i] < groupNames[j]
	})

	opts := make([]Option, 0, len(all))
	for _, name := range groupNames {
		opts = append(opts, byGroup[name]...)
	}
	return opts
}

// GroupOptions collects consecutive options with the same Group into Groups,
// which is the form the templates range over. A flat option list keeps the
// order GroupByRegion set.
func GroupOptions(opts []Option) []Group {
	var groups []Group
	for _, o := range opts {
		if len(groups) == 0 || groups[len(groups)-1].Name != o.Group {
			groups = append(groups, Group{Name: o.Group})
		}
		last := &groups[len(groups)-1]
		last.Options = append(last.Options, o)
	}
	return groups
}
