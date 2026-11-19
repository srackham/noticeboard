package web

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"noticeboard/internal/admintable"
	"noticeboard/internal/paging"
	"noticeboard/internal/settings"
)

// errBadPage reports a page query value that is not a positive whole number.
var errBadPage = errors.New("page must be a positive whole number")

// tablePageView is the data for the full table page.
type tablePageView struct {
	Slug  string
	Title string
	Grid  tableView
}

// tableView is the data for the grid region and its out-of-band refresh.
type tableView struct {
	Slug     string
	Columns  []string
	Rows     []tableRow
	Search   string // the active search text; empty means no filter
	Number   int    // page shown
	Pages    int
	Total    int
	First    int // position of the first row on the page, from 1
	Last     int // position of the last row on the page
	PrevURL  string
	NextURL  string
	Previous bool // there is a previous page
	Next     bool // there is a next page
}

// tableRow is one record in the grid. The URLs are built here so the template
// needs no query-string assembly.
type tableRow struct {
	ID        int64
	Cells     []string // one raw value per column
	EditURL   string
	DeleteURL string
	Confirm   string // the hx-confirm text for Delete
}

// tableFormView is the data for the record form.
type tableFormView struct {
	Slug    string
	Heading string
	Action  string // the URL the form posts to
	Fields  []tableFormField
	Error   string // a message for the whole form, such as a full board
}

// tableFormField is one input in the record form.
type tableFormField struct {
	Name      string
	Label     string
	Value     string
	MaxLength int
	Groups    []admintable.Group // set for select fields
	Error     string
}

// tableMissingView is the data for the 404 answer to a stale mutation.
type tableMissingView struct {
	GridView tableView
}

// tableFor finds the table named by the {table} path value. An unknown slug
// gives false, and the caller answers 404. The slug is only ever compared with
// a registered adapter's Slug, never used to build SQL.
func (s *Server) tableFor(r *http.Request) (admintable.Table, bool) {
	slug := r.PathValue("table")
	for _, t := range s.tables {
		if t.Slug() == slug {
			return t, true
		}
	}
	return nil, false
}

// tablePageSize returns the configured rows per page.
func (s *Server) tablePageSize(r *http.Request) (int, error) {
	st, err := settings.Get(r.Context(), s.db)
	if err != nil {
		return 0, err
	}
	return st.TablePageSize, nil
}

// pagingFor parses the page query value. A missing value is page 1. A value
// that is not a positive whole number gives errBadPage.
func (s *Server) pagingFor(r *http.Request) (admintable.Paging, error) {
	page := 1
	if v := r.URL.Query().Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return admintable.Paging{}, errBadPage
		}
		page = n
	}
	size, err := s.tablePageSize(r)
	if err != nil {
		return admintable.Paging{}, err
	}
	return admintable.Paging{Page: page, Size: size}, nil
}

// searchFor returns the search text from the q query value, trimmed. An empty
// value means no filter.
func searchFor(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("q"))
}

// pageValues builds the query values shared by the page URLs: the page number
// and, when present, the search text.
func pageValues(page int, search string) url.Values {
	v := url.Values{"page": {strconv.Itoa(page)}}
	if search != "" {
		v.Set("q", search)
	}
	return v
}

// tableURL is the page URL for a table, page number and search text.
func tableURL(slug string, page int, search string) string {
	return "/admin/tables/" + slug + "?" + pageValues(page, search).Encode()
}

// recordURL is the URL of a record's update or delete route, with the page and
// search that the response should re-render.
func recordURL(slug string, id int64, page int, search string) string {
	return "/admin/tables/" + slug + "/" + strconv.FormatInt(id, 10) + "?" + pageValues(page, search).Encode()
}

// tableAction is the create route's URL, with the search so the refreshed grid
// keeps the filter.
func tableAction(slug, search string) string {
	if search == "" {
		return "/admin/tables/" + slug
	}
	return "/admin/tables/" + slug + "?q=" + url.QueryEscape(search)
}

// newTableView builds the grid data from a page of records and the active search.
func newTableView(slug string, p admintable.Page, search string) tableView {
	view := tableView{
		Slug:     slug,
		Search:   search,
		Rows:     make([]tableRow, len(p.Rows)),
		Number:   p.Number,
		Pages:    p.Pages,
		Total:    p.Total,
		First:    p.Offset + 1,
		Last:     p.Offset + len(p.Rows),
		PrevURL:  tableURL(slug, p.Number-1, search),
		NextURL:  tableURL(slug, p.Number+1, search),
		Previous: p.Number > 1,
		Next:     p.Number < p.Pages,
	}
	for i, row := range p.Rows {
		view.Rows[i] = tableRow{
			ID:        row.ID,
			Cells:     row.Cells,
			EditURL:   "/admin/tables/" + slug + "/" + strconv.FormatInt(row.ID, 10) + "/edit?" + pageValues(p.Number, search).Encode(),
			DeleteURL: recordURL(slug, row.ID, p.Number, search),
			Confirm:   "Delete record " + strconv.FormatInt(row.ID, 10) + "?",
		}
	}
	return view
}

// loadGrid lists one page of a table and builds the grid data, for the search
// text in the request.
func (s *Server) loadGrid(r *http.Request, t admintable.Table, pg admintable.Paging) (tableView, error) {
	search := searchFor(r)
	p, err := t.List(r.Context(), pg, search)
	if err != nil {
		return tableView{}, err
	}
	view := newTableView(t.Slug(), p, search)
	view.Columns = t.Columns()
	return view, nil
}

// newTableFormView builds the form data, with the values as typed and any field
// errors. values may be nil for an empty form.
func newTableFormView(t admintable.Table, heading, action string, values admintable.Values, errs admintable.FieldErrors, message string) tableFormView {
	fields := t.Fields()
	view := tableFormView{Slug: t.Slug(), Heading: heading, Action: action, Error: message}
	for _, f := range fields {
		value := values[f.Name]
		field := tableFormField{
			Name:      f.Name,
			Label:     f.Label,
			Value:     value,
			MaxLength: f.MaxLength,
			Error:     errs[f.Name],
		}
		if f.Options != nil {
			field.Groups = admintable.GroupOptions(f.Options(value))
		}
		view.Fields = append(view.Fields, field)
	}
	return view
}

// handleTablePage serves the full table page, or the grid fragment when HTMX
// asks for it. The page and fragment share this route, so the response says
// which one it is with Vary.
func (s *Server) handleTablePage(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tableFor(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	pg, err := s.pagingFor(r)
	if err != nil {
		s.pagingError(w, r, err)
		return
	}
	grid, err := s.loadGrid(r, t, pg)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	w.Header().Set("Vary", "HX-Request")
	if isHTMX(r) {
		s.execute(w, r, "table-grid", grid, http.StatusOK)
		return
	}
	s.execute(w, r, "table-page", tablePageView{Slug: t.Slug(), Title: t.Title(), Grid: grid}, http.StatusOK)
}

// handleTableNew serves the new-record form, filled with the adapter's defaults.
func (s *Server) handleTableNew(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tableFor(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	defaults, err := t.Defaults(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	view := newTableFormView(t, "New record", tableAction(t.Slug(), searchFor(r)), defaults, nil, "")
	s.execute(w, r, "table-form", view, http.StatusOK)
}

// handleTableEdit serves the form for an existing record.
func (s *Server) handleTableEdit(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tableFor(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	id, ok := pathRecordID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	pg, err := s.pagingFor(r)
	if err != nil {
		s.pagingError(w, r, err)
		return
	}
	values, err := t.Load(r.Context(), id)
	if errors.Is(err, admintable.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	action := recordURL(t.Slug(), id, pg.Page, searchFor(r))
	s.execute(w, r, "table-form", newTableFormView(t, "Edit record", action, values, nil, ""), http.StatusOK)
}

// handleTableCreate adds a record at the end of the table and shows the last
// page, where the new record appears.
func (s *Server) handleTableCreate(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tableFor(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		formError(w, err)
		return
	}
	values := formValues(r, t)
	if err := t.Create(r.Context(), values); err != nil {
		s.tableFormFailure(w, r, t, err, "New record", tableAction(t.Slug(), searchFor(r)), values)
		return
	}
	size, err := s.tablePageSize(r)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.renderTableChange(w, r, t, admintable.Paging{Page: paging.LastPage, Size: size})
}

// handleTableUpdate changes a record's fields and re-renders the page the user
// was on.
func (s *Server) handleTableUpdate(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tableFor(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	id, ok := pathRecordID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	pg, err := s.pagingFor(r)
	if err != nil {
		s.pagingError(w, r, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		formError(w, err)
		return
	}
	values := formValues(r, t)
	if err := t.Update(r.Context(), id, values); err != nil {
		s.tableFormFailure(w, r, t, err, "Edit record", recordURL(t.Slug(), id, pg.Page, searchFor(r)), values)
		return
	}
	s.renderTableChange(w, r, t, pg)
}

// handleTableDelete removes a record.
func (s *Server) handleTableDelete(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tableFor(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	id, ok := pathRecordID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	pg, err := s.pagingFor(r)
	if err != nil {
		s.pagingError(w, r, err)
		return
	}
	switch err := t.Delete(r.Context(), id); {
	case err == nil:
		s.renderTableChange(w, r, t, pg)
	case errors.Is(err, admintable.ErrNotFound):
		s.tableMissing(w, r, t, pg)
	default:
		s.serverError(w, r, err)
	}
}

// renderTableChange answers a successful change with the refreshed grid and an
// empty form region.
func (s *Server) renderTableChange(w http.ResponseWriter, r *http.Request, t admintable.Table, pg admintable.Paging) {
	grid, err := s.loadGrid(r, t, pg)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.execute(w, r, "table-grid-oob", grid, http.StatusOK)
}

// tableMissing answers a stale record with 404, the refreshed grid and a
// message in the form region.
func (s *Server) tableMissing(w http.ResponseWriter, r *http.Request, t admintable.Table, pg admintable.Paging) {
	grid, err := s.loadGrid(r, t, pg)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.execute(w, r, "table-missing-oob", tableMissingView{GridView: grid}, http.StatusNotFound)
}

// tableFormFailure re-renders the form with the reason a save failed. Field
// errors and a refusal both give 422 and keep the form in its own region. A
// record that vanished gives the stale-record answer.
func (s *Server) tableFormFailure(w http.ResponseWriter, r *http.Request, t admintable.Table, err error, heading, action string, values admintable.Values) {
	var fieldErrs admintable.FieldErrors
	var refused admintable.Refused
	switch {
	case errors.As(err, &fieldErrs):
		s.execute(w, r, "table-form", newTableFormView(t, heading, action, values, fieldErrs, ""), http.StatusUnprocessableEntity)
	case errors.As(err, &refused):
		s.execute(w, r, "table-form", newTableFormView(t, heading, action, values, nil, refused.Message), http.StatusUnprocessableEntity)
	case errors.Is(err, admintable.ErrNotFound):
		pg, perr := s.pagingFor(r)
		if perr != nil {
			s.pagingError(w, r, perr)
			return
		}
		s.tableMissing(w, r, t, pg)
	default:
		s.serverError(w, r, err)
	}
}

// pagingError answers a bad page value with 400 and anything else with 500.
func (s *Server) pagingError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errBadPage) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.serverError(w, r, err)
}

// pathRecordID parses the {id} path value. A malformed ID is treated as unknown.
func pathRecordID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// formValues reads the form values named by the table's fields.
func formValues(r *http.Request, t admintable.Table) admintable.Values {
	values := admintable.Values{}
	for _, f := range t.Fields() {
		values[f.Name] = r.PostForm.Get(f.Name)
	}
	return values
}
