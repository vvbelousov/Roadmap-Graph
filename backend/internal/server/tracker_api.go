package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func (s *Server) trackerRoutes() {
	s.mux.HandleFunc("GET /api/tracker", s.trackerStatus)
	s.mux.HandleFunc("PUT /api/tracker/config", s.configureTracker)
	s.mux.HandleFunc("POST /api/tracker/test", func(w http.ResponseWriter, r *http.Request) {
		c, e := s.trackerConfig()
		if e != nil {
			fail(w, 500, "Database error")
			return
		}
		if e = validateTracker(c); e != nil {
			fail(w, 422, e.Error())
			return
		}
		_, e = s.tracker.request(r.Context(), c, "GET", "/v3/myself", nil, nil)
		if e != nil {
			fail(w, 502, e.Error())
			return
		}
		respond(w, 200, map[string]bool{"connected": true})
	})
	s.mux.HandleFunc("POST /api/tracker/preview", func(w http.ResponseWriter, r *http.Request) {
		c, e := s.trackerConfig()
		if e != nil {
			fail(w, 500, "Database error")
			return
		}
		if !c.Enabled {
			fail(w, 409, "Tracker integration disabled")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()
		p, e := s.makePreview(ctx, c)
		if e != nil {
			fail(w, 422, e.Error())
			return
		}
		fp, e := s.fingerprint()
		if e == nil {
			_, e = s.db.Exec("DELETE FROM tracker_previews WHERE expires<?", time.Now().Unix())
		}
		if e == nil {
			_, e = s.db.Exec("INSERT INTO tracker_previews VALUES(?,?,?,?)", p.ID, jsonText(p), fp, time.Now().Add(15*time.Minute).Unix())
		}
		if e != nil {
			fail(w, 500, "Database error")
			return
		}
		respond(w, 200, p)
	})
	s.mux.HandleFunc("POST /api/tracker/import", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			PreviewID string `json:"preview_id"`
		}
		if !decode(w, r, &b) {
			return
		}
		var raw, fp string
		var expires int64
		e := s.db.QueryRow("SELECT snapshot,fingerprint,expires FROM tracker_previews WHERE id=?", b.PreviewID).Scan(&raw, &fp, &expires)
		current, er := s.fingerprint()
		if e != nil || er != nil || fp != current || expires < time.Now().Unix() {
			fail(w, 409, "Preview expired or local data/configuration changed; preview again")
			return
		}
		var p TrackerPreview
		if json.Unmarshal([]byte(raw), &p) != nil {
			fail(w, 500, "Invalid preview")
			return
		}
		if e = s.applyPreview(p); e != nil {
			fail(w, 409, "Import rejected: "+e.Error())
			return
		}
		respond(w, 200, map[string]bool{"imported": true})
	})
	s.mux.HandleFunc("POST /api/tracker/sync", func(w http.ResponseWriter, r *http.Request) {
		c, e := s.trackerConfig()
		if e != nil {
			fail(w, 500, "Database error")
			return
		}
		if !c.Enabled {
			fail(w, 409, "Tracker integration disabled")
			return
		}
		var lease int64
		s.db.QueryRow("SELECT lease_until FROM tracker_connection").Scan(&lease)
		if lease > time.Now().Unix() {
			fail(w, 409, "Synchronization already running")
			return
		}
		go func() {
			s.writeMu.Lock()
			defer s.writeMu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			fresh, e := s.trackerConfig()
			if e == nil {
				s.syncTracker(ctx, fresh)
			}
		}()
		respond(w, 202, map[string]bool{"scheduled": true})
	})
	s.mux.HandleFunc("POST /api/tracker/retry", func(w http.ResponseWriter, r *http.Request) {
		tx, e := s.db.Begin()
		if e != nil {
			fail(w, 500, "Database error")
			return
		}
		defer tx.Rollback()
		_, e = tx.Exec("UPDATE tracker_outbox SET attempts=0,next_attempt=0,error=''")
		if e == nil {
			_, e = tx.Exec("DELETE FROM tracker_retry")
		}
		if e == nil {
			e = tx.Commit()
		}
		if e != nil {
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
	c, e := s.trackerConfig()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	records, e := s.trackerRecords()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	var last, err string
	var until int64
	s.db.QueryRow("SELECT last_success,last_error,lease_until FROM tracker_connection WHERE id='tracker'").Scan(&last, &err, &until)
	ops := []map[string]any{}
	rows, e := s.db.Query("SELECT local_id,kind,attempts,next_attempt,error FROM tracker_outbox ORDER BY id")
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, kind, msg string
		var n int
		var next int64
		rows.Scan(&id, &kind, &n, &next, &msg)
		ops = append(ops, map[string]any{"local_id": id, "kind": kind, "attempts": n, "next_attempt": next, "error": msg})
	}
	respond(w, 200, map[string]any{"config": c, "records": records, "outbox": ops, "last_success": last, "last_error": err, "running": until > time.Now().Unix()})
}
func (s *Server) configureTracker(w http.ResponseWriter, r *http.Request) {
	var c TrackerConfig
	if !decode(w, r, &c) {
		return
	}
	if e := validateTracker(c); e != nil {
		fail(w, 422, e.Error())
		return
	}
	old, e := s.trackerConfig()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	var n int
	s.db.QueryRow("SELECT count(*) FROM tracker_records").Scan(&n)
	if n > 0 && (old.OrgID != c.OrgID || old.OrgHeader != c.OrgHeader) {
		fail(w, 409, "This database is already bound to a Tracker organization")
		return
	}
	s.db.QueryRow("SELECT count(*) FROM tracker_records WHERE local_id IS NOT NULL AND state!='unlinked'").Scan(&n)
	if n > 0 && !sameConfigMapping(old, c) {
		fail(w, 409, "Unlink imported milestones before changing organization or field/status/link mappings")
		return
	}
	s.db.QueryRow("SELECT count(*) FROM tracker_outbox WHERE kind='publish'").Scan(&n)
	if n > 0 && !sameConfigMapping(old, c) {
		fail(w, 409, "Pending publications must finish or be unlinked before changing mappings")
		return
	}
	_, e = s.db.Exec("INSERT INTO tracker_connection(id,config) VALUES('tracker',?) ON CONFLICT(id) DO UPDATE SET config=excluded.config", jsonText(c))
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	respond(w, 200, c)
}
func (s *Server) publishTracker(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Selection int `json:"selection"`
	}
	if !decode(w, r, &b) {
		return
	}
	c, e := s.trackerConfig()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	if !c.Enabled || b.Selection < 0 || b.Selection >= len(c.Selections) || c.Selections[b.Selection].Queue == "" {
		fail(w, 422, "Select an enabled queue destination")
		return
	}
	g, e := s.graph()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	id := r.PathValue("id")
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
	sel := c.Selections[b.Selection]
	if sel.ProjectID != "" && sel.ProjectID != m.ProjectID {
		fail(w, 422, "Destination project differs from milestone project")
		return
	}
	records, e := s.trackerRecords()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	for _, x := range records {
		if x.LocalID == id {
			fail(w, 409, "Milestone already linked or excluded; use a different milestone")
			return
		}
	}
	// Validate representability before queuing an externally visible operation.
	if _, ok := c.Fields["status"]; ok && c.WriteStatuses[m.Status] == "" {
		fail(w, 422, "Publication requires a write status mapping for this milestone")
		return
	}
	tx, e := s.db.Begin()
	if e == nil {
		e = enqueue(tx, id, "publish", sel.Queue)
	}
	if e != nil {
		if tx != nil {
			tx.Rollback()
		}
		fail(w, 409, "Cannot queue publication")
		return
	}
	if e = tx.Commit(); e != nil {
		fail(w, 500, "Database error")
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
	c, e := s.trackerConfig()
	if e != nil {
		return e
	}
	var linked *TrackerRecord
	for i := range records {
		if records[i].LocalID == m.ID && records[i].State != "unlinked" {
			linked = &records[i]
		}
	}
	if linked != nil {
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
		if e = enqueue(tx, d.PrerequisiteID, "links", "{}"); e != nil {
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
