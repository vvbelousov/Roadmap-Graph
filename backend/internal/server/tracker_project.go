package server

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Public settings deliberately have no overlapping connection/mapping fields.
type TrackerConnectionConfig struct {
	Enabled   bool   `json:"enabled"`
	OrgID     string `json:"org_id"`
	OrgHeader string `json:"org_header"`
	Auth      string `json:"auth"`
	Interval  int    `json:"poll_interval_seconds"`
	Revision  int    `json:"revision"`
}
type ProjectTrackerSelection struct {
	ID    string `json:"id"`
	Queue string `json:"queue"`
	Query string `json:"query"`
}
type ProjectTrackerConfig struct {
	Enabled       bool                      `json:"enabled"`
	Revision      int                       `json:"revision"`
	Selections    []ProjectTrackerSelection `json:"selections"`
	Fields        map[string]TrackerField   `json:"fields"`
	Statuses      map[string]string         `json:"statuses"`
	WriteStatuses map[string]string         `json:"write_statuses"`
	Links         []TrackerLinkMapping      `json:"links"`
}

func defaultProjectTrackerConfig() ProjectTrackerConfig {
	d := defaultTrackerConfig()
	return ProjectTrackerConfig{Selections: []ProjectTrackerSelection{}, Fields: d.Fields, Statuses: d.Statuses, WriteStatuses: d.WriteStatuses, Links: d.Links}
}
func connectionPart(c TrackerConfig) TrackerConnectionConfig {
	return TrackerConnectionConfig{Enabled: c.Enabled, OrgID: c.OrgID, OrgHeader: c.OrgHeader, Auth: c.Auth, Interval: c.Interval, Revision: c.Revision}
}
func projectPart(c TrackerConfig) ProjectTrackerConfig {
	p := ProjectTrackerConfig{Enabled: c.Enabled, Revision: c.Revision, Fields: c.Fields, Statuses: c.Statuses, WriteStatuses: c.WriteStatuses, Links: c.Links, Selections: []ProjectTrackerSelection{}}
	for _, sel := range c.Selections {
		p.Selections = append(p.Selections, ProjectTrackerSelection{sel.ID, sel.Queue, sel.Query})
	}
	return p
}
func composeTracker(global TrackerConnectionConfig, p ProjectTrackerConfig, id string) TrackerConfig {
	c := TrackerConfig{Enabled: global.Enabled && p.Enabled, OrgID: global.OrgID, OrgHeader: global.OrgHeader, Auth: global.Auth, Interval: global.Interval, ProjectID: id, Revision: p.Revision, Fields: p.Fields, Statuses: p.Statuses, WriteStatuses: p.WriteStatuses, Links: p.Links}
	for _, sel := range p.Selections {
		c.Selections = append(c.Selections, TrackerSelection{ID: sel.ID, Queue: sel.Queue, Query: sel.Query, ProjectID: id})
	}
	return c
}
func (s *Server) connectionConfig() (TrackerConnectionConfig, error) {
	var raw string
	var revision int
	c := TrackerConnectionConfig{OrgHeader: "X-Org-ID", Auth: "OAuth", Interval: 300}
	e := s.db.QueryRow("SELECT config,revision FROM tracker_connection WHERE id='tracker'").Scan(&raw, &revision)
	if e == sql.ErrNoRows {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	e = json.Unmarshal([]byte(raw), &c)
	c.Revision = revision
	return c, e
}
func (s *Server) projectTrackerConfig(id string) (ProjectTrackerConfig, bool, error) {
	p := defaultProjectTrackerConfig()
	var raw string
	var revision int
	e := s.db.QueryRow("SELECT config,revision FROM tracker_project_config WHERE project_id=?", id).Scan(&raw, &revision)
	if e == sql.ErrNoRows {
		return p, false, nil
	}
	if e != nil {
		return p, false, e
	}
	p = ProjectTrackerConfig{}
	e = json.Unmarshal([]byte(raw), &p)
	normalizeProjectConfig(&p)
	p.Revision = revision
	return p, true, e
}
func (s *Server) resolveTrackerConfig(id string) (TrackerConfig, error) {
	global, e := s.connectionConfig()
	if e != nil {
		return TrackerConfig{}, e
	}
	p, configured, e := s.projectTrackerConfig(id)
	if e != nil {
		return TrackerConfig{}, e
	}
	if !configured {
		return TrackerConfig{ProjectID: id}, fmt.Errorf("Project Tracker settings are not configured")
	}
	return composeTracker(global, p, id), nil
}
func (s *Server) projectExists(id string) bool {
	var n int
	return s.db.QueryRow("SELECT count(*) FROM projects WHERE id=?", id).Scan(&n) == nil && n == 1
}
func mappingHash(p ProjectTrackerConfig) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(jsonText([]any{p.Fields, p.Statuses, p.WriteStatuses, p.Links}))))
}
func validateConnection(c TrackerConnectionConfig, required bool) error {
	if !has([]string{"X-Org-ID", "X-Cloud-Org-ID"}, c.OrgHeader) || !has([]string{"OAuth", "Bearer"}, c.Auth) || c.Auth == "Bearer" && c.OrgHeader != "X-Cloud-Org-ID" {
		return fmt.Errorf("invalid organization header or authentication")
	}
	if c.Interval < 30 || c.Interval > 86400 {
		return fmt.Errorf("polling interval must be 30–86400 seconds")
	}
	if strings.ContainsAny(c.OrgID, "\r\n") || required && strings.TrimSpace(c.OrgID) == "" {
		return fmt.Errorf("Organization ID is required")
	}
	return nil
}
func validateProjectConfig(p ProjectTrackerConfig, id string) error {
	ids := map[string]bool{}
	for _, sel := range p.Selections {
		if sel.ID == "" || len(sel.ID) > 128 || ids[sel.ID] {
			return fmt.Errorf("Each selection needs a unique stable ID")
		}
		ids[sel.ID] = true
	}
	if !p.Enabled {
		return nil
	}
	c := composeTracker(TrackerConnectionConfig{Enabled: true, OrgID: "validation", OrgHeader: "X-Org-ID", Auth: "OAuth", Interval: 300}, p, id)
	return validateTracker(c)
}

// Runs once in the same SQLite transaction as schema migration 3. No HTTP calls.
func migrateProjectTracker(db *sql.DB) error {
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var exists int
	if e = tx.QueryRow("SELECT count(*) FROM schema_migrations WHERE version=3").Scan(&exists); e != nil {
		return e
	}
	if exists > 0 {
		return tx.Commit()
	}
	if _, e = tx.Exec(projectTrackerMigration); e != nil {
		return e
	}
	var raw, last string
	e = tx.QueryRow("SELECT config,last_success FROM tracker_connection WHERE id='tracker'").Scan(&raw, &last)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	if raw != "" {
		var old TrackerConfig
		if e = json.Unmarshal([]byte(raw), &old); e != nil {
			return fmt.Errorf("legacy Tracker configuration cannot be decoded: %w", e)
		}
		if _, e = tx.Exec("INSERT INTO tracker_legacy_config VALUES('tracker',?)", raw); e != nil {
			return e
		}
		projects := map[string]bool{}
		rows, e := tx.Query("SELECT id FROM projects")
		if e != nil {
			return e
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			projects[id] = true
		}
		rows.Close()
		grouped := map[string]ProjectTrackerConfig{}
		warnings := map[string]string{}
		globalWarning := ""
		for i, sel := range old.Selections {
			if !projects[sel.ProjectID] || sel.Project != nil {
				globalWarning = "Legacy selections could not be assigned safely. Original settings are retained in tracker_legacy_config; review project destinations."
				continue
			}
			p, ok := grouped[sel.ProjectID]
			if !ok {
				p = projectPart(old)
				p.Selections = []ProjectTrackerSelection{}
			}
			stable := fmt.Sprintf("legacy-%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s:%s", sel.ProjectID, i, sel.Queue, sel.Query))))
			p.Selections = append(p.Selections, ProjectTrackerSelection{stable, sel.Queue, sel.Query})
			grouped[sel.ProjectID] = p
		}
		rows, e = tx.Query("SELECT DISTINCT project_id FROM tracker_records WHERE project_id IS NOT NULL UNION SELECT DISTINCT project_id FROM tracker_outbox WHERE project_id IS NOT NULL")
		if e != nil {
			return e
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			if _, ok := grouped[id]; !ok {
				p := projectPart(old)
				p.Enabled = false
				p.Selections = []ProjectTrackerSelection{}
				grouped[id] = p
				warnings[id] = "Legacy linked milestones or pending operations have no selection for this project. Synchronization is paused until destinations are reviewed."
			}
		}
		rows.Close()
		for id, p := range grouped {
			p.Revision = 1
			if _, e = tx.Exec("INSERT INTO tracker_project_config(project_id,config,revision,last_success,warning) VALUES(?,?,1,?,?)", id, jsonText(p), last, warnings[id]); e != nil {
				return e
			}
		}
		type op struct{ ID, Project, Kind, Queue string }
		ops := []op{}
		rows, e = tx.Query("SELECT id,COALESCE(project_id,''),kind,payload FROM tracker_outbox")
		if e != nil {
			return e
		}
		for rows.Next() {
			var o op
			if e = rows.Scan(&o.ID, &o.Project, &o.Kind, &o.Queue); e != nil {
				rows.Close()
				return e
			}
			ops = append(ops, o)
		}
		rows.Close()
		for _, o := range ops {
			p, ok := grouped[o.Project]
			if !ok {
				continue
			}
			selection := ""
			if o.Kind == "publish" {
				matches := 0
				for _, sel := range p.Selections {
					if sel.Queue == o.Queue {
						matches++
						selection = sel.ID
					}
				}
				if matches != 1 {
					p.Enabled = false
					grouped[o.Project] = p
					warnings[o.Project] = "Legacy publication destination is ambiguous or missing. Pending publication retained; review selections and unlink/cancel the operation before enabling."
					if _, e = tx.Exec("UPDATE tracker_project_config SET config=?,warning=? WHERE project_id=?", jsonText(p), warnings[o.Project], o.Project); e != nil {
						return e
					}
					selection = ""
				}
			}
			if _, e = tx.Exec("UPDATE tracker_outbox SET selection_id=?,context_hash=? WHERE id=?", selection, mappingHash(p), o.ID); e != nil {
				return e
			}
		}
		if _, e = tx.Exec("UPDATE tracker_connection SET config=?,revision=1,migration_warning=? WHERE id='tracker'", jsonText(connectionPart(old)), globalWarning); e != nil {
			return e
		}
	}
	// Related relationships are owned by the endpoint with the lower stable external ID.
	rows, err := tx.Query("SELECT edge_key FROM tracker_edges")
	if err != nil {
		return err
	}
	edgeKeys := []string{}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return err
		}
		edgeKeys = append(edgeKeys, key)
	}
	rows.Close()
	for _, key := range edgeKeys {
		parts := strings.Split(key, "|")
		if len(parts) != 3 {
			continue
		}
		if parts[2] != "related_to" {
			continue
		}
		var a, b, ownerA, ownerB string
		if err = tx.QueryRow("SELECT a.external_id,b.external_id,COALESCE(a.project_id,''),COALESCE(b.project_id,'') FROM tracker_records a JOIN tracker_records b ON b.local_id=? WHERE a.local_id=?", parts[1], parts[0]).Scan(&a, &b, &ownerA, &ownerB); err == sql.ErrNoRows {
			continue
		} else if err != nil {
			return err
		}
		owner := ownerA
		if b < a {
			owner = ownerB
		}
		if _, err = tx.Exec("UPDATE tracker_edges SET owner_project_id=? WHERE edge_key=?", nullableProject(owner), key); err != nil {
			return err
		}
	}
	if _, e = tx.Exec("INSERT INTO schema_migrations VALUES(3)"); e != nil {
		return e
	}
	return tx.Commit()
}

func normalizeProjectConfig(p *ProjectTrackerConfig) {
	if p.Fields == nil {
		p.Fields = map[string]TrackerField{}
	}
	if p.Statuses == nil {
		p.Statuses = map[string]string{}
	}
	if p.WriteStatuses == nil {
		p.WriteStatuses = map[string]string{}
	}
	if p.Selections == nil {
		p.Selections = []ProjectTrackerSelection{}
	}
	if p.Links == nil {
		p.Links = []TrackerLinkMapping{}
	}
}
