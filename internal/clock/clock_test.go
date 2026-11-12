package clock

import (
	"testing"
	"time"
)

func TestFormat(t *testing.T) {
	// 2024-06-01 14:05:09 UTC, a fixed instant so the output does not depend on the host clock.
	now := time.Date(2024, time.June, 1, 14, 5, 9, 0, time.UTC)

	tests := []struct {
		name string
		zone string
		want string
	}{
		{name: "UTC", zone: "UTC", want: "14:05"},
		{name: "half-hour offset", zone: "Asia/Kolkata", want: "19:35"},
		{name: "wraps past midnight", zone: "Pacific/Auckland", want: "02:05"},
		{name: "daylight saving applies", zone: "America/Vancouver", want: "07:05"},
		{name: "standard time in June", zone: "Australia/Sydney", want: "00:05"},
		{name: "previous day", zone: "Asia/Manila", want: "22:05"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc, err := time.LoadLocation(tt.zone)
			if err != nil {
				t.Fatalf("LoadLocation(%q): %v", tt.zone, err)
			}
			if got := Format(now, loc); got != tt.want {
				t.Errorf("Format(%s) = %q, want %q", tt.zone, got, tt.want)
			}
		})
	}
}

func TestFormatIsTwentyFourHour(t *testing.T) {
	tests := []struct {
		name   string
		hour   int
		minute int
		want   string
	}{
		{name: "midnight", hour: 0, minute: 0, want: "00:00"},
		{name: "morning", hour: 9, minute: 7, want: "09:07"},
		{name: "noon", hour: 12, minute: 0, want: "12:00"},
		{name: "afternoon", hour: 13, minute: 30, want: "13:30"},
		{name: "last minute", hour: 23, minute: 59, want: "23:59"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2024, time.January, 1, tt.hour, tt.minute, 0, 0, time.UTC)
			if got := Format(now, time.UTC); got != tt.want {
				t.Errorf("Format = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestColourClass(t *testing.T) {
	tests := []struct {
		zone string
		want string
	}{
		{zone: "UTC", want: ClassUTC},
		{zone: "Etc/UTC", want: ClassClock},
		{zone: "GMT", want: ClassClock},
		{zone: "Europe/London", want: ClassClock},
		{zone: "utc", want: ClassClock},
		{zone: "Pacific/Auckland", want: ClassClock},
		{zone: "", want: ClassClock},
	}
	for _, tt := range tests {
		t.Run(tt.zone, func(t *testing.T) {
			if got := ColourClass(tt.zone); got != tt.want {
				t.Errorf("ColourClass(%q) = %q, want %q", tt.zone, got, tt.want)
			}
		})
	}
}
