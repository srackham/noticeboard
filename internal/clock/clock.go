// Package clock holds the pure time logic for the board: formatting a time
// for a zone and choosing a tile's colour class. It does no I/O.
package clock

import "time"

// Colour classes select the colour of a clock tile. They are also used as CSS class names.
const (
	// ClassUTC is used only for the zone "UTC".
	ClassUTC = "utc"
	// ClassClock is used for every other zone.
	ClassClock = "clock"
)

// Format returns now as a 24-hour HH:MM time in loc.
func Format(now time.Time, loc *time.Location) string {
	return now.In(loc).Format("15:04")
}

// ColourClass returns the colour class for a clock in the IANA zone name zone.
// Only the exact name "UTC" gets ClassUTC. Other names, including "Etc/UTC",
// "GMT" and "Europe/London", get ClassClock.
func ColourClass(zone string) string {
	if zone == "UTC" {
		return ClassUTC
	}
	return ClassClock
}
