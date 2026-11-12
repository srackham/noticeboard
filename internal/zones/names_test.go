package zones

import (
	"sort"
	"testing"
	"time"
)

func TestNamesLoadAsLocations(t *testing.T) {
	names := Names()
	if len(names) < 300 {
		t.Fatalf("Names returned %d zones, want the full list (about 346)", len(names))
	}
	for _, name := range names {
		if _, err := time.LoadLocation(name); err != nil {
			t.Errorf("listed zone %q does not load: %v", name, err)
		}
	}
}

func TestNamesSortedUniqueAndExcludeNonZones(t *testing.T) {
	names := Names()
	if !sort.StringsAreSorted(names) {
		t.Error("Names is not sorted")
	}
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			t.Errorf("duplicate %q", name)
		}
		seen[name] = true
	}
	for _, bad := range []string{"", "Local", "Factory"} {
		if seen[bad] {
			t.Errorf("Names includes %q, which is not a selectable zone", bad)
		}
	}
}

func TestNamesIncludeSeedClocksAndUTC(t *testing.T) {
	have := map[string]bool{}
	for _, name := range Names() {
		have[name] = true
	}
	for _, want := range []string{
		"UTC", "Pacific/Auckland", "Asia/Kolkata", "Australia/Sydney", "Asia/Shanghai",
		"America/Vancouver", "Asia/Manila", "Europe/Moscow", "Europe/Kyiv", "Europe/London",
	} {
		if !have[want] {
			t.Errorf("Names is missing %q", want)
		}
	}
}

func TestNamesOmitLegacyAliases(t *testing.T) {
	have := map[string]bool{}
	for _, name := range Names() {
		have[name] = true
	}
	// Each of these is a Link in the IANA backward file, so it is an alias of a canonical zone.
	for _, alias := range []string{"Asia/Calcutta", "US/Eastern", "GB", "Japan", "Europe/Kiev", "Africa/Asmera", "Brazil/East"} {
		if have[alias] {
			t.Errorf("Names includes the legacy alias %q", alias)
		}
	}
	// UTC is kept although the backward file links it, because the UTC clock needs it.
	if !have["UTC"] {
		t.Error("Names is missing UTC")
	}
}

func TestNamesAreTheSameSetValidationAccepts(t *testing.T) {
	// Every offered name must pass the same check the repository applies on save.
	for _, name := range Names() {
		if _, _, errs := validate("label", name); len(errs) != 0 {
			t.Errorf("validate rejects offered zone %q: %v", name, errs)
		}
	}
}
