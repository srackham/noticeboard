package web

import (
	"errors"
	"net/http"
	"strconv"

	"noticeboard/internal/zones"
)

// clocksView is the data for the clock list. The same data renders the
// dashboard and the out-of-band refresh after each change.
type clocksView struct {
	Rows    []zoneRow
	Enabled int // clocks currently enabled
	Size    int // board size, for "N of 9 enabled"
}

// zoneRow is one line of the clock list. First and Last disable the Up and
// Down buttons at the ends of the list.
type zoneRow struct {
	ID      int64
	Label   string
	Zone    string
	Enabled bool
	First   bool
	Last    bool
}

// zoneFormView is the data for the new and edit clock form.
type zoneFormView struct {
	Heading string
	Action  string // the URL the form posts to
	Label   string
	Zone    string
	Errors  zones.Errors // field messages, keyed by "label" or "zone"
	Error   string       // a message for the whole form, such as a full board
}

// pathClockID parses the {id} path value. A malformed ID is treated as unknown.
func pathClockID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// clockAction is "/admin/zones/{id}" plus an optional suffix, such as "/toggle".
func clockAction(id int64, suffix string) string {
	return "/admin/zones/" + strconv.FormatInt(id, 10) + suffix
}

// loadClocks reads every clock and the enabled count.
func (s *Server) loadClocks(r *http.Request) (clocksView, error) {
	ctx := r.Context()
	all, err := zones.List(ctx, s.db)
	if err != nil {
		return clocksView{}, err
	}
	enabled, err := zones.CountEnabled(ctx, s.db)
	if err != nil {
		return clocksView{}, err
	}
	view := clocksView{Enabled: enabled, Size: zones.BoardSize, Rows: make([]zoneRow, len(all))}
	for i, z := range all {
		view.Rows[i] = zoneRow{
			ID: z.ID, Label: z.Label, Zone: z.Zone, Enabled: z.Enabled,
			First: i == 0, Last: i == len(all)-1,
		}
	}
	return view, nil
}

// handleZoneNew serves the empty new-clock form.
func (s *Server) handleZoneNew(w http.ResponseWriter, r *http.Request) {
	s.execute(w, r, "zone-form", zoneFormView{Heading: "New clock", Action: "/admin/zones"}, http.StatusOK)
}

// handleZoneEdit serves the form for an existing clock.
func (s *Server) handleZoneEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathClockID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	z, err := zones.Get(r.Context(), s.db, id)
	if errors.Is(err, zones.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.execute(w, r, "zone-form", zoneFormView{
		Heading: "Edit clock",
		Action:  clockAction(id, ""),
		Label:   z.Label,
		Zone:    z.Zone,
	}, http.StatusOK)
}

// handleZoneCreate adds a clock at the end of the list.
func (s *Server) handleZoneCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		formError(w, err)
		return
	}
	label, zone := r.PostForm.Get("label"), r.PostForm.Get("zone")
	if _, err := zones.Create(r.Context(), s.db, label, zone); err != nil {
		s.zoneFormFailure(w, r, err, zoneFormView{
			Heading: "New clock", Action: "/admin/zones", Label: label, Zone: zone,
		})
		return
	}
	s.renderClockChange(w, r)
}

// handleZoneUpdate changes the label and zone of a clock.
func (s *Server) handleZoneUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathClockID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		formError(w, err)
		return
	}
	label, zone := r.PostForm.Get("label"), r.PostForm.Get("zone")
	if _, err := zones.Update(r.Context(), s.db, id, label, zone); err != nil {
		if errors.Is(err, zones.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.zoneFormFailure(w, r, err, zoneFormView{
			Heading: "Edit clock", Action: clockAction(id, ""), Label: label, Zone: zone,
		})
		return
	}
	s.renderClockChange(w, r)
}

// handleZoneToggle flips a clock between enabled and disabled.
func (s *Server) handleZoneToggle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathClockID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.clockActionResult(w, r, zones.Toggle(r.Context(), s.db, id))
}

// handleZoneMove swaps a clock with its neighbour. The dir query value must be "up" or "down".
func (s *Server) handleZoneMove(w http.ResponseWriter, r *http.Request) {
	id, ok := pathClockID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	dir, ok := zones.ParseDirection(r.URL.Query().Get("dir"))
	if !ok {
		http.Error(w, "dir must be up or down", http.StatusBadRequest)
		return
	}
	s.clockActionResult(w, r, zones.Move(r.Context(), s.db, id, dir))
}

// handleZoneDelete removes a clock.
func (s *Server) handleZoneDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathClockID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.clockActionResult(w, r, zones.Delete(r.Context(), s.db, id))
}

// renderClockChange answers a successful change with the refreshed clock list and an empty form region.
func (s *Server) renderClockChange(w http.ResponseWriter, r *http.Request) {
	view, err := s.loadClocks(r)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.execute(w, r, "clocks-oob", view, http.StatusOK)
}

// clockActionResult turns the outcome of a toggle, move or delete into a response.
// ErrBoardFull is shown in the form region with 422. The list is left alone, because nothing changed.
func (s *Server) clockActionResult(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case err == nil:
		s.renderClockChange(w, r)
	case errors.Is(err, zones.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, zones.ErrBoardFull):
		s.execute(w, r, "form-message-oob", err.Error(), http.StatusUnprocessableEntity)
	default:
		s.serverError(w, r, err)
	}
}

// zoneFormFailure re-renders the clock form with the reason for a failed save. Field
// errors and a full board both give 422 and keep the form in its own region.
func (s *Server) zoneFormFailure(w http.ResponseWriter, r *http.Request, err error, v zoneFormView) {
	var fieldErrs zones.Errors
	switch {
	case errors.As(err, &fieldErrs):
		v.Errors = fieldErrs
	case errors.Is(err, zones.ErrBoardFull):
		v.Error = err.Error()
	default:
		s.serverError(w, r, err)
		return
	}
	s.execute(w, r, "zone-form", v, http.StatusUnprocessableEntity)
}
