package zones

import (
	_ "embed"
	"strings"
	"sync"
)

// zoneNamesText is the list of zone names offered by the clock form. Its
// header explains how to regenerate it.
//
//go:embed zonenames.txt
var zoneNamesText string

var (
	namesOnce sync.Once
	zoneNames []string
)

// Names returns the zone names the clock form offers, sorted. Each name loads
// with time.LoadLocation, so a choice from the list always passes validation.
// The caller may modify the returned slice.
func Names() []string {
	namesOnce.Do(func() {
		for _, line := range strings.Split(zoneNamesText, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			zoneNames = append(zoneNames, line)
		}
	})
	return append([]string(nil), zoneNames...)
}
