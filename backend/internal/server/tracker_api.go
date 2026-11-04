package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func (s *Server) trackerRoutes() {
	s.mux.HandleFunc("GET /api/tracker", s.trackerStatus)
	s.mux.HandleFunc("PUT /api/tracker/config", s.configureTracker)
	s.mux.HandleFunc("POST /api/tracker/test", func(w http.ResponseWriter, r *http.Request) {
		g, e := s.connectionConfig()
		if e == nil {
			e = validateConnection(g, true)
		}
		if e != nil {
			fail(w, 422, e.Error())
			return
		}
		c := composeTracker(g, ProjectTrackerConfig{}, "")
		_, e = s.tracker.request(r.Context(), c, "GET", "/v3/myself", nil, nil)
		if e != nil {
			fail(w, 502, e.Error())
			return
		}
		respond(w, 200, map[string]bool{"connected": true})
	})
	for _, path := range []string{"preview", "import"} {
		s.mux.HandleFunc("POST /api/tracker/"+path, func(w http.ResponseWriter, r *http.Request) {
			fail(w, 410, "Use the project's Tracker settings for import and preview")
		})
	}
	s.mux.HandleFunc("POST /api/tracker/sync", s.scheduleTracker)
	s.mux.HandleFunc("POST /api/tracker/retry", func(w http.ResponseWriter, r *http.Request) {
		if _, e := s.db.Exec("DELETE FROM tracker_retry"); e != nil {
			fail(w, 500, "Database error")
			return
		}
		respond(w, 200, map[string]bool{"ready": true})
	})
	s.mux.HandleFunc("GET /api/projects/{id}/tracker", s.trackerStatus)
	s.mux.HandleFunc("PUT /api/projects/{id}/tracker/config", s.configureProjectTracker)
	s.mux.HandleFunc("POST /api/projects/{id}/tracker/preview", s.projectTrackerPreview)
	s.mux.HandleFunc("POST /api/projects/{id}/tracker/import", s.projectTrackerImport)
	s.mux.HandleFunc("POST /api/projects/{id}/tracker/sync", s.scheduleTracker)
	s.mux.HandleFunc("POST /api/projects/{id}/tracker/retry", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !s.projectExists(id) {
			fail(w, 404, "Project not found")
			return
		}
		if _, e := s.db.Exec("UPDATE tracker_outbox SET attempts=0,next_attempt=0,error='' WHERE project_id=?", id); e != nil {
			fail(w, 500, "Database error")
			return
		}
		respond(w, 200, map[string]bool{"ready": true})
	})
	s.mux.HandleFunc("POST /api/tracker/milestones/{id}/publish", s.publishTracker)
	s.mux.HandleFunc("POST /api/tracker/milestones/{id}/unlink", s.unlinkTracker)
	s.mux.HandleFunc("POST /api/tracker/milestones/{id}/resolve", s.resolveTracker)
}
func (s *Server) trackerStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id != "" && !s.projectExists(id) {
		fail(w, 404, "Project not found")
		return
	}
	g, e := s.connectionConfig()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	records, e := s.trackerRecords()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	filtered := []TrackerRecord{}
	for _, x := range records {
		if id == "" || x.ProjectID == id {
			filtered = append(filtered, x)
		}
	}
	var config any = g
	var last, msg, warning string
	var until int64
	configured := true
	if id == "" {
		s.db.QueryRow("SELECT last_success,last_error,lease_until,migration_warning FROM tracker_connection WHERE id='tracker'").Scan(&last, &msg, &until, &warning)
	} else {
		p, ok, e := s.projectTrackerConfig(id)
		if e != nil {
			fail(w, 500, "Database error")
			return
		}
		config = p
		configured = ok
		s.db.QueryRow("SELECT last_success,last_error,warning FROM tracker_project_config WHERE project_id=?", id).Scan(&last, &msg, &warning)
		s.db.QueryRow("SELECT lease_until FROM tracker_connection WHERE id='tracker'").Scan(&until)
	}
	ops := []map[string]any{}
	rows, e := s.db.Query("SELECT local_id,kind,attempts,next_attempt,error,COALESCE(project_id,''),selection_id FROM tracker_outbox WHERE (?='' OR project_id=?) ORDER BY id", id, id)
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	for rows.Next() {
		var local, kind, err, project, selection string
		var attempts int
		var next int64
		if e = rows.Scan(&local, &kind, &attempts, &next, &err, &project, &selection); e != nil {
			rows.Close()
			fail(w, 500, "Database error")
			return
		}
		ops = append(ops, map[string]any{"local_id": local, "kind": kind, "attempts": attempts, "next_attempt": next, "error": err, "project_id": project, "selection_id": selection})
	}
	rows.Close()
	summary := []map[string]any{}
	rows, e = s.db.Query("SELECT p.id,p.name,c.config,c.last_success,c.last_error,c.warning FROM projects p LEFT JOIN tracker_project_config c ON c.project_id=p.id ORDER BY p.sort_order,p.name")
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	for rows.Next() {
		var pid, name string
		var raw, success, err, warn sql.NullString
		rows.Scan(&pid, &name, &raw, &success, &err, &warn)
		var p ProjectTrackerConfig
		if raw.Valid {
			json.Unmarshal([]byte(raw.String), &p)
		}
		summary = append(summary, map[string]any{"project_id": pid, "name": name, "configured": raw.Valid, "enabled": p.Enabled, "last_success": success.String, "last_error": err.String, "warning": warn.String})
	}
	rows.Close()
	respond(w, 200, map[string]any{"config": config, "connection": g, "configured": configured, "token_available": os.Getenv("ROADMAP_TRACKER_TOKEN") != "", "project_id": id, "projects": summary, "records": filtered, "outbox": ops, "last_success": last, "last_error": msg, "warning": warning, "running": until > time.Now().Unix()})
}
func (s *Server) configureTracker(w http.ResponseWriter, r *http.Request) {
	var g TrackerConnectionConfig
	if !decode(w, r, &g) {
		return
	}
	if e := validateConnection(g, g.Enabled); e != nil {
		fail(w, 422, e.Error())
		return
	}
	old, e := s.connectionConfig()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	if old.Revision != g.Revision {
		fail(w, 409, "Settings changed in another session; reload before saving")
		return
	}
	var n int
	if e = s.db.QueryRow("SELECT count(*) FROM tracker_records").Scan(&n); e != nil {
		fail(w, 500, "Database error")
		return
	}
	if n > 0 && (old.OrgID != g.OrgID || old.OrgHeader != g.OrgHeader) {
		fail(w, 409, "This database is already bound to a Tracker organization")
		return
	}
	g.Revision++
	result, e := s.db.Exec("INSERT INTO tracker_connection(id,config,revision) VALUES('tracker',?,?) ON CONFLICT(id) DO UPDATE SET config=excluded.config,revision=excluded.revision WHERE tracker_connection.revision=?", jsonText(g), g.Revision, old.Revision)
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	if n, _ := result.RowsAffected(); n != 1 {
		fail(w, 409, "Settings changed in another session; reload before saving")
		return
	}
	respond(w, 200, g)
}
func (s *Server) configureProjectTracker(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.projectExists(id) {
		fail(w, 404, "Project not found")
		return
	}
	var p ProjectTrackerConfig
	if !decode(w, r, &p) {
		return
	}
	normalizeProjectConfig(&p)
	if e := validateProjectConfig(p, id); e != nil {
		fail(w, 422, e.Error())
		return
	}
	old, exists, e := s.projectTrackerConfig(id)
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	if old.Revision != p.Revision {
		fail(w, 409, "Settings changed in another session; reload before saving")
		return
	}
	var bound, pending int
	s.db.QueryRow("SELECT count(*) FROM tracker_records WHERE project_id=? AND state!='unlinked' AND local_id IS NOT NULL", id).Scan(&bound)
	s.db.QueryRow("SELECT count(*) FROM tracker_outbox WHERE project_id=?", id).Scan(&pending)
	g, _ := s.connectionConfig()
	if bound > 0 && !sameConfigMapping(composeTracker(g, old, id), composeTracker(g, p, id)) {
		fail(w, 409, "Unlink imported milestones before changing field IDs, formats or link mappings")
		return
	}
	if pending > 0 && mappingHash(old) != mappingHash(p) {
		fail(w, 409, "Finish or unlink pending operations before changing their mapping context")
		return
	}
	rows, e := s.db.Query("SELECT selection_id,payload FROM tracker_outbox WHERE project_id=? AND kind='publish'", id)
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	for rows.Next() {
		var selection, queue string
		rows.Scan(&selection, &queue)
		valid := false
		for _, sel := range p.Selections {
			if sel.ID == selection && sel.Queue == queue {
				valid = true
			}
		}
		if !valid {
			rows.Close()
			fail(w, 409, "Pending publication destination cannot be removed or changed; unlink it first")
			return
		}
	}
	rows.Close()
	p.Revision++
	var res sql.Result
	if exists {
		res, e = s.db.Exec("UPDATE tracker_project_config SET config=?,revision=?,warning='' WHERE project_id=? AND revision=?", jsonText(p), p.Revision, id, old.Revision)
	} else {
		res, e = s.db.Exec("INSERT INTO tracker_project_config(project_id,config,revision) VALUES(?,?,?) ON CONFLICT(project_id) DO NOTHING", id, jsonText(p), p.Revision)
	}
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		fail(w, 409, "Settings changed in another session; reload before saving")
		return
	}
	slog.DebugContext(r.Context(), "Tracker project configuration saved", "project_id", id, "enabled", p.Enabled, "revision", p.Revision, "mapped_fields", len(p.Fields), "status_mappings", len(p.Statuses), "outgoing_statuses", len(p.WriteStatuses), "status_writable", p.Fields["status"].Writable)
	respond(w, 200, p)
}
func (s *Server) projectTrackerPreview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.projectExists(id) {
		fail(w, 404, "Project not found")
		return
	}
	c, e := s.resolveTrackerConfig(id)
	if e == nil && !c.Enabled {
		e = fmt.Errorf("Tracker integration or project synchronization disabled")
	}
	if e == nil {
		e = validateTracker(c)
	}
	if e != nil {
		fail(w, 409, e.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	p, e := s.makePreview(ctx, c)
	if e != nil {
		fail(w, 422, e.Error())
		return
	}
	fp, e := s.fingerprint(id)
	if e == nil {
		_, e = s.db.Exec("INSERT INTO tracker_previews(id,snapshot,fingerprint,expires,project_id) VALUES(?,?,?,?,?)", p.ID, jsonText(p), fp, time.Now().Add(15*time.Minute).Unix(), id)
	}
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	respond(w, 200, p)
}
func (s *Server) projectTrackerImport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.projectExists(id) {
		fail(w, 404, "Project not found")
		return
	}
	var b struct {
		PreviewID string `json:"preview_id"`
	}
	if !decode(w, r, &b) {
		return
	}
	var raw, fp string
	var expires int64
	e := s.db.QueryRow("SELECT snapshot,fingerprint,expires FROM tracker_previews WHERE id=? AND project_id=?", b.PreviewID, id).Scan(&raw, &fp, &expires)
	current, er := s.fingerprint(id)
	if e != nil || er != nil || current != fp || expires < time.Now().Unix() {
		fail(w, 409, "Preview expired or local data/configuration changed; preview again")
		return
	}
	var p TrackerPreview
	if json.Unmarshal([]byte(raw), &p) != nil || p.ProjectID != id {
		fail(w, 409, "Preview belongs to another project")
		return
	}
	for _, it := range p.Items {
		if it.Milestone.ProjectID != id {
			fail(w, 409, "Preview contains another project's milestone")
			return
		}
	}
	if e = s.applyPreview(p); e != nil {
		fail(w, 409, "Import rejected: "+e.Error())
		return
	}
	respond(w, 200, map[string]bool{"imported": true})
}
func (s *Server) scheduleTracker(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id != "" && !s.projectExists(id) {
		fail(w, 404, "Project not found")
		return
	}
	g, e := s.connectionConfig()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	if !g.Enabled {
		fail(w, 409, "Tracker integration disabled")
		return
	}
	if id != "" {
		c, e := s.resolveTrackerConfig(id)
		if e != nil || !c.Enabled {
			fail(w, 409, "Project synchronization disabled or unconfigured")
			return
		}
	}
	var until int64
	s.db.QueryRow("SELECT lease_until FROM tracker_connection").Scan(&until)
	if until > time.Now().Unix() {
		fail(w, 409, "Synchronization already running")
		return
	}
	go func() {
		s.writeMu.Lock()
		defer s.writeMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if id == "" {
			s.syncTracker(ctx, TrackerConfig{})
		} else {
			s.syncTracker(ctx, TrackerConfig{ProjectID: id})
		}
	}()
	respond(w, 202, map[string]bool{"scheduled": true})
}
func (s *Server) publishTracker(w http.ResponseWriter, r *http.Request) {
	var b struct {
		SelectionID string `json:"selection_id"`
	}
	if !decode(w, r, &b) {
		return
	}
	id := r.PathValue("id")
	g, e := s.graph()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	var m Milestone
	for _, v := range g.Milestones {
		if v.ID == id {
			m = v
		}
	}
	if m.ID == "" {
		fail(w, 404, "Milestone not found")
		return
	}
	c, e := s.resolveTrackerConfig(m.ProjectID)
	if e != nil || !c.Enabled {
		fail(w, 409, "Project synchronization disabled or unconfigured")
		return
	}
	if e = validateTracker(c); e != nil {
		fail(w, 422, e.Error())
		return
	}
	var selected *TrackerSelection
	for i := range c.Selections {
		if c.Selections[i].ID == b.SelectionID && c.Selections[i].Queue != "" {
			selected = &c.Selections[i]
		}
	}
	if selected == nil {
		fail(w, 422, "Select a queue destination belonging to this project")
		return
	}
	var bound int
	s.db.QueryRow("SELECT count(*) FROM tracker_records WHERE local_id=?", id).Scan(&bound)
	if bound > 0 {
		fail(w, 409, "Milestone already linked or excluded; use a different milestone")
		return
	}
	if _, ok := c.Fields["status"]; ok && c.WriteStatuses[m.Status] == "" {
		fail(w, 422, "Publication requires a write status mapping for this milestone")
		return
	}
	tx, e := s.db.Begin()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	defer tx.Rollback()
	if e = enqueue(tx, id, "publish", selected.Queue); e == nil {
		_, e = tx.Exec("UPDATE tracker_outbox SET selection_id=? WHERE local_id=? AND kind='publish' AND selection_id=''", selected.ID, id)
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		fail(w, 409, "Cannot queue publication")
		return
	}
	respond(w, 202, map[string]bool{"pending": true})
}
func (s *Server) unlinkTracker(w http.ResponseWriter, r *http.Request) {
	tx, e := s.db.Begin()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	defer tx.Rollback()
	id := r.PathValue("id")
	_, e = tx.Exec("UPDATE tracker_records SET state='unlinked',error='',conflicts='{}' WHERE local_id=?", id)
	if e == nil {
		_, e = tx.Exec("DELETE FROM tracker_outbox WHERE local_id=?", id)
	}
	if e == nil {
		_, e = tx.Exec("DELETE FROM tracker_edges WHERE edge_key LIKE ? OR edge_key LIKE ?", id+"|%", "%|"+id+"|%")
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	respond(w, 200, map[string]bool{"unlinked": true})
}
func (s *Server) resolveTracker(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Choices map[string]string `json:"choices"`
	}
	if !decode(w, r, &b) {
		return
	}
	records, e := s.trackerRecords()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	var x TrackerRecord
	for _, v := range records {
		if v.LocalID == r.PathValue("id") {
			x = v
		}
	}
	if x.State != "conflict" {
		fail(w, 409, "No synchronization conflict")
		return
	}
	g, e := s.graph()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	var m Milestone
	for _, v := range g.Milestones {
		if v.ID == x.LocalID {
			m = v
		}
	}
	fields := localFields(m)
	if len(b.Choices) != len(x.Conflicts) {
		fail(w, 422, "Choose a value for every disputed field")
		return
	}
	for k, v := range x.Conflicts {
		choice := b.Choices[k]
		if !has([]string{"local", "remote"}, choice) {
			fail(w, 422, "Choose local or remote for "+k)
			return
		}
		if fields[k] != v.Local {
			fail(w, 409, "Local value changed; synchronize again")
			return
		}
		x.Baseline[k] = v.Remote
		if choice == "remote" {
			fields[k] = v.Remote
		}
	}
	setFields(&m, fields)
	if e = validateMilestone(m); e != nil {
		fail(w, 422, e.Error())
		return
	}
	x.Conflicts = map[string]TrackerConflict{}
	x.State = "pending_upload"
	x.Error = ""
	if e = s.saveTrackerState(m, x, true); e != nil {
		fail(w, 500, "Database error")
		return
	}
	respond(w, 200, map[string]bool{"resolved": true})
}

// Local graph writes and their durable intent share the same SQLite transaction.
func (s *Server) patchTrackedMilestone(m Milestone, old Milestone) error {
	records, e := s.trackerRecords()
	if e != nil {
		return e
	}
	c, e := s.resolveTrackerConfig(old.ProjectID)
	var linked *TrackerRecord
	for i := range records {
		if records[i].LocalID == m.ID && records[i].State != "unlinked" {
			linked = &records[i]
		}
	}
	if m.ProjectID != old.ProjectID {
		var pending int
		s.db.QueryRow("SELECT count(*) FROM tracker_outbox WHERE local_id=?", m.ID).Scan(&pending)
		if linked != nil || pending > 0 {
			return fmt.Errorf("Unlink the milestone and cancel pending operations before moving it to another project")
		}
	}
	if linked != nil {
		if e != nil {
			return e
		}
		a, b := localFields(old), localFields(m)
		for k, f := range c.Fields {
			if !f.Writable && a[k] != b[k] {
				return fmt.Errorf("Tracker field %s is read-only; unlink to edit locally", k)
			}
		}
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = updateMilestone(tx, m); e != nil {
		return e
	}
	if linked != nil {
		if linked.State != "conflict" {
			linked.State = "pending_upload"
		}
		if e = saveRecord(tx, *linked); e != nil {
			return e
		}
		if e = enqueue(tx, m.ID, "update", "{}"); e != nil {
			return e
		}
		if _, e = tx.Exec("UPDATE tracker_outbox SET attempts=0,next_attempt=0 WHERE local_id=? AND kind='update'", m.ID); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Server) mutateDependency(d Dependency, remove bool) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if !remove && d.Type == "related_to" {
		var existing int
		if e = tx.QueryRow("SELECT count(*) FROM dependencies WHERE dependency_type='related_to' AND ((prerequisite_milestone_id=? AND dependent_milestone_id=?) OR (prerequisite_milestone_id=? AND dependent_milestone_id=?))", d.PrerequisiteID, d.DependentID, d.DependentID, d.PrerequisiteID).Scan(&existing); e != nil {
			return e
		}
		if existing > 0 {
			return fmt.Errorf("related link already exists")
		}
	}
	if remove {
		_, e = tx.Exec("DELETE FROM dependencies WHERE id=?", d.ID)
	} else {
		_, e = tx.Exec("INSERT INTO dependencies VALUES(?,?,?,?,?)", d.ID, d.PrerequisiteID, d.DependentID, d.Type, d.CreatedAt)
	}
	if e != nil {
		return e
	}
	var n int
	e = tx.QueryRow("SELECT count(*) FROM tracker_records WHERE local_id IN (?,?) AND state!='unlinked'", d.PrerequisiteID, d.DependentID).Scan(&n)
	if e != nil {
		return e
	}
	if n == 2 {
		owner, e := edgeOwnerTx(tx, d)
		if e != nil {
			return e
		}
		if e = enqueue(tx, owner, "links", "{}"); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Server) deleteGraphObject(table, id string) (int64, error) {
	tx, e := s.db.Begin()
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	where := "local_id=?"
	if table == "projects" {
		where = "local_id IN (SELECT id FROM milestones WHERE project_id=?)"
	}
	if table != "dependencies" {
		if _, e = tx.Exec("UPDATE tracker_records SET state='unlinked',error='',conflicts='{}' WHERE "+where, id); e != nil {
			return 0, e
		}
	}
	res, e := tx.Exec("DELETE FROM "+table+" WHERE id=?", id)
	if e != nil {
		return 0, e
	}
	n, _ := res.RowsAffected()
	return n, tx.Commit()
}
