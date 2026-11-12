package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNewClockFormOffersZoneDropDown(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	body := serve(h, withCookie(httptest.NewRequest(http.MethodGet, "/admin/zones/new", nil), c)).Body.String()
	for _, want := range []string{
		`<select id="zone" name="zone" required>`,
		`<option value="" selected>Choose a time zone</option>`,
		`<optgroup label="Australia">`,
		`<option value="Australia/Sydney">Australia/Sydney</option>`,
		`<optgroup label="Other">`,
		`<option value="UTC">UTC</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("new clock form missing %q", want)
		}
	}
	if strings.Contains(body, `type="text" value="" placeholder="Australia/Sydney"`) {
		t.Error("the free-text zone field is still present")
	}
	if strings.Contains(body, `value="Local"`) {
		t.Error("Local should not be offered")
	}
}

func TestEditClockFormSelectsCurrentZone(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	id := clockID(t, sqlDB, "Australia/Sydney")

	body := serve(h, withCookie(httptest.NewRequest(http.MethodGet, "/admin/zones/"+id+"/edit", nil), c)).Body.String()
	if !strings.Contains(body, `<option value="Australia/Sydney" selected>Australia/Sydney</option>`) {
		t.Errorf("current zone not selected:\n%s", body)
	}
	if strings.Contains(body, `<option value="" selected>`) {
		t.Error("placeholder should not be selected for an existing clock")
	}
	if n := strings.Count(body, " selected>"); n != 1 {
		t.Errorf("selected options = %d, want 1 (the placeholder is not selected)", n)
	}
}

func TestEditClockFormKeepsZoneMissingFromList(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	// A zone the list does not offer, as if the database were older than the list.
	exec(t, sqlDB, `UPDATE timezones SET zone = 'Not/AZone' WHERE zone = 'Asia/Kolkata'`)
	id := clockID(t, sqlDB, "Not/AZone")

	body := serve(h, withCookie(httptest.NewRequest(http.MethodGet, "/admin/zones/"+id+"/edit", nil), c)).Body.String()
	if !strings.Contains(body, `<option value="Not/AZone" selected>Not/AZone</option>`) {
		t.Errorf("stored zone not shown and selected:\n%s", body)
	}
}

func TestInvalidZoneSubmissionKeepsSelection(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	rec := serve(h, adminRequest(http.MethodPost, "/admin/zones",
		url.Values{"label": {"Harbour"}, "zone": {"Not/AZone"}}, c))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<option value="Not/AZone" selected>`) {
		t.Errorf("submitted zone not kept selected:\n%s", body)
	}
	if !strings.Contains(body, "Unknown time zone.") {
		t.Errorf("zone message missing:\n%s", body)
	}
}

func TestZoneGroupsSplitByRegionAndKeepCurrent(t *testing.T) {
	groups := zoneGroups([]string{"Asia/Tokyo", "Europe/Kyiv", "UTC"}, "Asia/Tokyo")
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

	withExtra := zoneGroups([]string{"Asia/Tokyo"}, "Mars/Olympus")
	if len(withExtra) != 2 || withExtra[1].Name != "Mars" {
		t.Errorf("extra zone not added as its own group: %v", withExtra)
	}
}
