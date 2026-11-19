package settings

import (
	"strconv"
	"strings"
	"testing"
)

// validInput is a form submission that passes validation.
func validInput() Input {
	return Input{
		Title:            " Harbour ",
		RefreshSeconds:   " 12 ",
		ColourBackground: "#AABBCC",
		ColourLabel:      "#010203",
		ColourClock:      "#000000",
		ColourUTC:        "#ffffff",
		TablePageSize:    " 25 ",
	}
}

func TestParseInputAcceptsAndNormalises(t *testing.T) {
	s, errs := ParseInput(validInput())
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if s.Title != "Harbour" || s.RefreshSeconds != 12 || s.ColourBackground != "#aabbcc" {
		t.Errorf("ParseInput = %+v, want trimmed title, parsed refresh and lowercase colour", s)
	}
	if s.TablePageSize != 25 {
		t.Errorf("TablePageSize = %d, want 25", s.TablePageSize)
	}
}

func TestParseInputReportsField(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*Input)
		wantField string
	}{
		{name: "refresh empty", mutate: func(in *Input) { in.RefreshSeconds = "" }, wantField: FieldRefreshSeconds},
		{name: "refresh not a number", mutate: func(in *Input) { in.RefreshSeconds = "soon" }, wantField: FieldRefreshSeconds},
		{name: "refresh decimal", mutate: func(in *Input) { in.RefreshSeconds = "5.5" }, wantField: FieldRefreshSeconds},
		{name: "refresh zero", mutate: func(in *Input) { in.RefreshSeconds = "0" }, wantField: FieldRefreshSeconds},
		{name: "refresh too large", mutate: func(in *Input) { in.RefreshSeconds = "61" }, wantField: FieldRefreshSeconds},
		{name: "title too long", mutate: func(in *Input) { in.Title = strings.Repeat("x", 81) }, wantField: FieldTitle},
		{name: "background not a colour", mutate: func(in *Input) { in.ColourBackground = "grey" }, wantField: FieldColourBackground},
		{name: "label short form", mutate: func(in *Input) { in.ColourLabel = "#abc" }, wantField: FieldColourLabel},
		{name: "clock missing hash", mutate: func(in *Input) { in.ColourClock = "228b22" }, wantField: FieldColourClock},
		{name: "utc not hex", mutate: func(in *Input) { in.ColourUTC = "#zz0000" }, wantField: FieldColourUTC},
		{name: "page size empty", mutate: func(in *Input) { in.TablePageSize = "" }, wantField: FieldTablePageSize},
		{name: "page size not a number", mutate: func(in *Input) { in.TablePageSize = "many" }, wantField: FieldTablePageSize},
		{name: "page size too small", mutate: func(in *Input) { in.TablePageSize = strconv.Itoa(MinTablePageSize - 1) }, wantField: FieldTablePageSize},
		{name: "page size too large", mutate: func(in *Input) { in.TablePageSize = strconv.Itoa(MaxTablePageSize + 1) }, wantField: FieldTablePageSize},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := validInput()
			tt.mutate(&in)
			_, errs := ParseInput(in)
			if _, ok := errs[tt.wantField]; !ok {
				t.Errorf("errors = %v, want an error for %s", errs, tt.wantField)
			}
			if len(errs) != 1 {
				t.Errorf("errors = %v, want only %s", errs, tt.wantField)
			}
		})
	}
}

// TestParseInputAcceptsPageSizeBounds checks the ends of the accepted range.
func TestParseInputAcceptsPageSizeBounds(t *testing.T) {
	for _, in := range []string{strconv.Itoa(MinTablePageSize), strconv.Itoa(MaxTablePageSize), " 20 "} {
		form := validInput()
		form.TablePageSize = in
		if _, errs := ParseInput(form); len(errs) != 0 {
			t.Errorf("TablePageSize %q: unexpected errors: %v", in, errs)
		}
	}
}

func TestParseInputRefreshMessageIsSingle(t *testing.T) {
	in := validInput()
	in.RefreshSeconds = "soon"
	_, errs := ParseInput(in)
	if got := errs[FieldRefreshSeconds]; got != refreshMessage() {
		t.Errorf("message = %q, want %q", got, refreshMessage())
	}
}
