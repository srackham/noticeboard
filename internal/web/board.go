package web

import (
	"context"
	"log"
	"net/http"
	"time"

	"noticeboard/internal/clock"
	"noticeboard/internal/settings"
	"noticeboard/internal/zones"
)

// unknownTime is shown for a clock whose zone cannot be loaded.
const unknownTime = "--:--"

// boardView is the data for the board templates.
type boardView struct {
	Title            string
	RefreshSeconds   int
	ColourBackground string
	ColourLabel      string
	ColourClock      string
	ColourUTC        string
	Clocks           []clockView
}

// clockView is one tile.
type clockView struct {
	Label string
	Zone  string
	Class string // clock.ClassUTC or clock.ClassClock
	UTC   bool
	Time  string // HH:MM
}

// handleRootRedirect sends the root path to the board's canonical URL.
func (s *Server) handleRootRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/clocks", http.StatusFound)
}

// handleClocks serves the clocks page. A normal request gets the full page with
// the board embedded; an HTMX poll gets just the board fragment, which it swaps
// in place.
func (s *Server) handleClocks(w http.ResponseWriter, r *http.Request) {
	if isHTMX(r) {
		s.render(w, r, "board")
		return
	}
	s.render(w, r, "index.html")
}

// render builds the board data and executes the named template. The output
// is buffered, so a failure gives a clean 500 rather than a partial page.
func (s *Server) render(w http.ResponseWriter, r *http.Request, name string) {
	view, err := s.loadBoard(r.Context())
	if err != nil {
		log.Printf("%s: %v", r.URL.Path, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	s.execute(w, r, name, view, http.StatusOK)
}

// loadBoard reads the settings and enabled clocks and formats each time for now.
func (s *Server) loadBoard(ctx context.Context) (boardView, error) {
	st, err := settings.Get(ctx, s.db)
	if err != nil {
		return boardView{}, err
	}
	zs, err := zones.ListEnabled(ctx, s.db)
	if err != nil {
		return boardView{}, err
	}

	now := s.now()
	view := boardView{
		Title:            st.Title,
		RefreshSeconds:   st.RefreshSeconds,
		ColourBackground: st.ColourBackground,
		ColourLabel:      st.ColourLabel,
		ColourClock:      st.ColourClock,
		ColourUTC:        st.ColourUTC,
		Clocks:           make([]clockView, 0, len(zs)),
	}
	for _, z := range zs {
		c := clockView{
			Label: z.Label,
			Zone:  z.Zone,
			Class: clock.ColourClass(z.Zone),
			Time:  unknownTime,
		}
		c.UTC = c.Class == clock.ClassUTC
		if loc, err := time.LoadLocation(z.Zone); err != nil {
			log.Printf("clock %q: zone %q: %v", z.Label, z.Zone, err)
		} else {
			c.Time = clock.Format(now, loc)
		}
		view.Clocks = append(view.Clocks, c)
	}
	return view, nil
}
