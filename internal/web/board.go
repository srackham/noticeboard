package web

import (
	"bytes"
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

// handleIndex serves the full page with the board embedded.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "index.html")
}

// handleBoard serves the board fragment that HTMX polls.
func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "board")
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

	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, name, view); err != nil {
		log.Printf("%s: render %s: %v", r.URL.Path, name, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
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
