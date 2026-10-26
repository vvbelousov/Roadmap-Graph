package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var colorRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
var slugRE = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func resource(r *http.Request) string {
	return strings.Split(strings.TrimPrefix(r.URL.Path, "/api/"), "/")[0]
}
func has(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
func date(s string) bool {
	d, e := time.Parse("2006-01-02", s)
	return e == nil && d.Format("2006-01-02") == s
}
func validateProject(p Project) error {
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 160 {
		return fmt.Errorf("Project name is required (max 160 bytes)")
	}
	if !slugRE.MatchString(p.Slug) || len(p.Slug) > 100 {
		return fmt.Errorf("Slug must use letters, numbers, underscore or hyphen")
	}
	if !colorRE.MatchString(p.Color) {
		return fmt.Errorf("Color must be #RRGGBB")
	}
	if len(p.Description) > 10000 || len(p.Icon) > 100 {
		return fmt.Errorf("Description or icon too long")
	}
	return nil
}
func validateMilestone(m Milestone) error {
	if strings.TrimSpace(m.Title) == "" || len(m.Title) > 200 || m.ProjectID == "" {
		return fmt.Errorf("Title and project are required")
	}
	if !date(m.PlannedDate) || (m.CompletedDate != "" && !date(m.CompletedDate)) {
		return fmt.Errorf("Use ISO YYYY-MM-DD calendar dates")
	}
	if !has([]string{"release", "feature", "integration", "market_launch", "end_of_life", "migration", "custom"}, m.Type) {
		return fmt.Errorf("Invalid milestone type")
	}
	if !has([]string{"planned", "in_progress", "completed", "delayed", "cancelled"}, m.Status) {
		return fmt.Errorf("Invalid milestone status")
	}
	if m.Status == "completed" && m.CompletedDate == "" {
		return fmt.Errorf("Completed milestone requires actual completion date")
	}
	if len(m.Description) > 20000 || len(m.Version) > 100 || len(m.Owner) > 200 || len(m.ExternalURL) > 2048 {
		return fmt.Errorf("Field too long")
	}
	if m.ExternalURL != "" {
		u, e := url.Parse(m.ExternalURL)
		if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
			return fmt.Errorf("External URL must be HTTP(S)")
		}
	}
	return nil
}
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	g, e := s.graph()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	switch resource(r) {
	case "projects":
		respond(w, 200, g.Projects)
	case "dependencies":
		respond(w, 200, g.Dependencies)
	case "milestones":
		out := []Milestone{}
		q := r.URL.Query()
		for _, m := range g.Milestones {
			if q.Get("project_id") != "" && m.ProjectID != q.Get("project_id") {
				continue
			}
			if q.Get("status") != "" && m.Status != q.Get("status") {
				continue
			}
			if q.Get("milestone_type") != "" && m.Type != q.Get("milestone_type") {
				continue
			}
			if q.Get("q") != "" && !strings.Contains(strings.ToLower(m.Title+" "+m.Description), strings.ToLower(q.Get("q"))) {
				continue
			}
			if q.Get("from") != "" && m.PlannedDate < q.Get("from") {
				continue
			}
			if q.Get("to") != "" && m.PlannedDate > q.Get("to") {
				continue
			}
			out = append(out, m)
		}
		respond(w, 200, out)
	}
}
func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	g, e := s.graph()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	id := r.PathValue("id")
	if resource(r) == "projects" {
		for _, p := range g.Projects {
			if p.ID == id {
				respond(w, 200, p)
				return
			}
		}
	} else {
		for _, m := range g.Milestones {
			if m.ID == id {
				respond(w, 200, m)
				return
			}
		}
	}
	fail(w, 404, "Record not found")
}
func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	switch resource(r) {
	case "projects":
		p := Project{Color: "#60a5fa"}
		if !decode(w, r, &p) {
			return
		}
		p.ID = token()
		p.CreatedAt = now()
		p.UpdatedAt = p.CreatedAt
		if e := validateProject(p); e != nil {
			fail(w, 422, e.Error())
			return
		}
		_, e := s.db.Exec("INSERT INTO projects VALUES(?,?,?,?,?,?,?,?,?)", p.ID, p.Name, p.Slug, p.Description, p.Color, p.Icon, p.SortOrder, p.CreatedAt, p.UpdatedAt)
		if e != nil {
			dbfail(w, e)
			return
		}
		respond(w, 201, p)
	case "milestones":
		m := Milestone{Type: "release", Status: "planned"}
		if !decode(w, r, &m) {
			return
		}
		m.ID = token()
		m.CreatedAt = now()
		m.UpdatedAt = m.CreatedAt
		if e := validateMilestone(m); e != nil {
			fail(w, 422, e.Error())
			return
		}
		_, e := s.db.Exec("INSERT INTO milestones VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)", m.ID, m.ProjectID, m.Title, m.Description, m.Type, m.Version, m.PlannedDate, m.CompletedDate, m.Status, m.Owner, m.ExternalURL, m.CreatedAt, m.UpdatedAt)
		if e != nil {
			dbfail(w, e)
			return
		}
		respond(w, 201, m)
	case "dependencies":
		var d Dependency
		if !decode(w, r, &d) {
			return
		}
		if d.PrerequisiteID == d.DependentID || d.PrerequisiteID == "" || d.DependentID == "" || !has([]string{"requires", "related_to"}, d.Type) {
			fail(w, 422, "Invalid dependency endpoints or type")
			return
		}
		g, e := s.graph()
		if e != nil {
			fail(w, 500, "Database error")
			return
		}
		if d.Type == "requires" && has(Reach(g.Dependencies, d.DependentID, false), d.PrerequisiteID) {
			fail(w, 422, "Hard dependency would create a cycle")
			return
		}
		d.ID = token()
		d.CreatedAt = now()
		_, e = s.db.Exec("INSERT INTO dependencies VALUES(?,?,?,?,?)", d.ID, d.PrerequisiteID, d.DependentID, d.Type, d.CreatedAt)
		if e != nil {
			dbfail(w, e)
			return
		}
		respond(w, 201, d)
	}
}
func mergePatch(w http.ResponseWriter, r *http.Request, v any) bool {
	var fields map[string]json.RawMessage
	if !decode(w, r, &fields) {
		return false
	}
	for _, k := range []string{"id", "created_at", "updated_at"} {
		if _, ok := fields[k]; ok {
			fail(w, 422, "Cannot modify "+k)
			return false
		}
	}
	old, _ := json.Marshal(v)
	var m map[string]json.RawMessage
	json.Unmarshal(old, &m)
	for k, val := range fields {
		if _, ok := m[k]; !ok {
			fail(w, 400, "Unknown field "+k)
			return false
		}
		if string(val) == "null" {
			fail(w, 422, "Null values are not supported; use empty string")
			return false
		}
		m[k] = val
	}
	b, _ := json.Marshal(m)
	if e := json.Unmarshal(b, v); e != nil {
		fail(w, 400, "Invalid field type")
		return false
	}
	return true
}
func (s *Server) patch(w http.ResponseWriter, r *http.Request) {
	g, e := s.graph()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	id := r.PathValue("id")
	if resource(r) == "projects" {
		for _, p := range g.Projects {
			if p.ID != id {
				continue
			}
			if !mergePatch(w, r, &p) {
				return
			}
			if e = validateProject(p); e != nil {
				fail(w, 422, e.Error())
				return
			}
			p.UpdatedAt = now()
			_, e = s.db.Exec("UPDATE projects SET name=?,slug=?,description=?,color=?,icon=?,sort_order=?,updated_at=? WHERE id=?", p.Name, p.Slug, p.Description, p.Color, p.Icon, p.SortOrder, p.UpdatedAt, id)
			if e != nil {
				dbfail(w, e)
				return
			}
			respond(w, 200, p)
			return
		}
	} else {
		for _, m := range g.Milestones {
			if m.ID != id {
				continue
			}
			if !mergePatch(w, r, &m) {
				return
			}
			if e = validateMilestone(m); e != nil {
				fail(w, 422, e.Error())
				return
			}
			m.UpdatedAt = now()
			_, e = s.db.Exec("UPDATE milestones SET project_id=?,title=?,description=?,milestone_type=?,version=?,planned_date=?,completed_date=?,status=?,owner=?,external_url=?,updated_at=? WHERE id=?", m.ProjectID, m.Title, m.Description, m.Type, m.Version, m.PlannedDate, m.CompletedDate, m.Status, m.Owner, m.ExternalURL, m.UpdatedAt, id)
			if e != nil {
				dbfail(w, e)
				return
			}
			respond(w, 200, m)
			return
		}
	}
	fail(w, 404, "Record not found")
}
func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	table := resource(r)
	if !has([]string{"projects", "milestones", "dependencies"}, table) {
		fail(w, 404, "Not found")
		return
	}
	res, e := s.db.Exec("DELETE FROM "+table+" WHERE id=?", r.PathValue("id"))
	if e != nil {
		dbfail(w, e)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404, "Record not found")
		return
	}
	w.WriteHeader(204)
}
func (s *Server) impact(w http.ResponseWriter, r *http.Request) {
	g, e := s.graph()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	id := r.PathValue("id")
	found := false
	for _, m := range g.Milestones {
		if m.ID == id {
			found = true
		}
	}
	if !found {
		fail(w, 404, "Milestone not found")
		return
	}
	before := Conflicts(g)
	if proposed := r.URL.Query().Get("planned_date"); proposed != "" {
		if !date(proposed) {
			fail(w, 422, "Invalid proposed date")
			return
		}
		for i := range g.Milestones {
			if g.Milestones[i].ID == id {
				g.Milestones[i].PlannedDate = proposed
			}
		}
	}
	direct := []string{}
	for _, d := range g.Dependencies {
		if d.Type == "requires" && d.PrerequisiteID == id {
			direct = append(direct, d.DependentID)
		}
	}
	all := Reach(g.Dependencies, id, false)
	trans := []string{}
	for _, x := range all {
		if !has(direct, x) {
			trans = append(trans, x)
		}
	}
	upstream := Reach(g.Dependencies, id, true)
	conflicts := Conflicts(g)
	newC := []Conflict{}
	for _, c := range conflicts {
		old := false
		for _, b := range before {
			if b.PrerequisiteID == c.PrerequisiteID && b.DependentID == c.DependentID {
				old = true
			}
		}
		if !old {
			newC = append(newC, c)
		}
	}
	respond(w, 200, map[string]any{"direct": direct, "transitive": trans, "prerequisites": upstream, "conflicts": conflicts, "existing_conflicts": before, "new_conflicts": newC})
}
func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		out := map[string]string{}
		rows, e := s.db.Query("SELECT key,value FROM settings")
		if e != nil {
			fail(w, 500, "Database error")
			return
		}
		defer rows.Close()
		for rows.Next() {
			var k, v string
			rows.Scan(&k, &v)
			out[k] = v
		}
		respond(w, 200, out)
		return
	}
	var b map[string]string
	if !decode(w, r, &b) {
		return
	}
	if len(b) > 30 {
		fail(w, 422, "Too many settings")
		return
	}
	tx, e := s.db.Begin()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	defer tx.Rollback()
	for k, v := range b {
		if len(k) > 80 || len(v) > 10000 {
			fail(w, 422, "Setting too long")
			return
		}
		if _, e = tx.Exec("INSERT INTO settings VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, v); e != nil {
			dbfail(w, e)
			return
		}
	}
	if e = tx.Commit(); e != nil {
		dbfail(w, e)
		return
	}
	respond(w, 200, b)
}

// reorderProjects requires a full permutation so concurrent stale clients cannot
// silently omit a newly-created lane or produce duplicate order values.
func (s *Server) reorderProjects(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &body) {
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		fail(w, 500, "Database error")
		return
	}
	defer tx.Rollback()
	rows, err := tx.Query("SELECT id FROM projects")
	if err != nil {
		fail(w, 500, "Database error")
		return
	}
	existing := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			fail(w, 500, "Database error")
			return
		}
		existing[id] = true
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		fail(w, 500, "Database error")
		return
	}
	rows.Close()
	if len(body.IDs) != len(existing) {
		fail(w, 422, "Order must contain every project ID exactly once")
		return
	}
	seen := map[string]bool{}
	for _, id := range body.IDs {
		if !existing[id] || seen[id] {
			fail(w, 422, "Order must contain every project ID exactly once")
			return
		}
		seen[id] = true
	}
	stamp := now()
	for i, id := range body.IDs {
		if _, err = tx.Exec("UPDATE projects SET sort_order=?,updated_at=? WHERE id=?", i, stamp, id); err != nil {
			dbfail(w, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		dbfail(w, err)
		return
	}
	g, err := s.graph()
	if err != nil {
		fail(w, 500, "Database error")
		return
	}
	respond(w, 200, g.Projects)
}
