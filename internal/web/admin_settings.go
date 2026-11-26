package web

import (
	"net/http"
	"strconv"

	"noticeboard/internal/settings"
)

// dashboardView is the data for the dashboard: the clock list and the settings form.
type dashboardView struct {
	Clocks      clockListView
	Settings    settingsFormView
	IdleTimeout int // seconds without user activity before the admin pages sign out
}

// settingsFormView is the data for the settings form. The values are kept as
// typed, so a rejected submission is shown again unchanged.
type settingsFormView struct {
	Title            string
	RefreshSeconds   string
	ColourBackground string
	ColourLabel      string
	ColourClock      string
	ColourUTC        string
	TablePageSize    string
	LoginTimeout     string
	Errors           settings.Errors // field messages, keyed by form field name
}

// newSettingsView builds the form data from the typed values.
func newSettingsView(in settings.Input, errs settings.Errors) settingsFormView {
	return settingsFormView{
		Title:            in.Title,
		RefreshSeconds:   in.RefreshSeconds,
		ColourBackground: in.ColourBackground,
		ColourLabel:      in.ColourLabel,
		ColourClock:      in.ColourClock,
		ColourUTC:        in.ColourUTC,
		TablePageSize:    in.TablePageSize,
		LoginTimeout:     in.LoginTimeout,
		Errors:           errs,
	}
}

// inputFrom converts stored settings to the form's string values.
func inputFrom(st settings.Settings) settings.Input {
	return settings.Input{
		Title:            st.Title,
		RefreshSeconds:   strconv.Itoa(st.RefreshSeconds),
		ColourBackground: st.ColourBackground,
		ColourLabel:      st.ColourLabel,
		ColourClock:      st.ColourClock,
		ColourUTC:        st.ColourUTC,
		TablePageSize:    strconv.Itoa(st.TablePageSize),
		LoginTimeout:     strconv.FormatFloat(st.LoginTimeout, 'g', -1, 64),
	}
}

// renderDashboard serves the dashboard with the clock list and the settings form.
func (s *Server) renderDashboard(w http.ResponseWriter, r *http.Request) {
	clocks, err := s.loadClocks(r)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	st, err := settings.Get(r.Context(), s.db)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	view := dashboardView{
		Clocks:      clocks,
		Settings:    newSettingsView(inputFrom(st), nil),
		IdleTimeout: st.LoginTimeoutSeconds(),
	}
	s.execute(w, r, "admin-page", view, http.StatusOK)
}

// handleSettings saves the clock display settings. Invalid input gives 422 with the form
// re-rendered, showing the messages and the values as typed. Valid input is saved,
// and the form comes back with the stored values.
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		formError(w, err)
		return
	}
	in := settings.Input{
		Title:            r.PostForm.Get("title"),
		RefreshSeconds:   r.PostForm.Get("refresh_seconds"),
		ColourBackground: r.PostForm.Get("colour_background"),
		ColourLabel:      r.PostForm.Get("colour_label"),
		ColourClock:      r.PostForm.Get("colour_clock"),
		ColourUTC:        r.PostForm.Get("colour_utc"),
		TablePageSize:    r.PostForm.Get("table_page_size"),
		LoginTimeout:     r.PostForm.Get("login_timeout"),
	}

	clean, errs := settings.ParseInput(in)
	if len(errs) > 0 {
		s.execute(w, r, "settings-form", newSettingsView(in, errs), http.StatusUnprocessableEntity)
		return
	}
	stored, err := settings.Update(r.Context(), s.db, clean)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.execute(w, r, "settings-form", newSettingsView(inputFrom(stored), nil), http.StatusOK)
}
