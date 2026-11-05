package server

import (
	"database/sql"
	"fmt"
	"reflect"
)

func (s *Server) eligibleTrackerLocals(c TrackerConfig, current map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for ext, id := range current {
		out[ext] = id
	}
	records, e := s.trackerRecords()
	if e != nil {
		return nil, e
	}
	for _, r := range records {
		if r.ProjectID == c.ProjectID || r.LocalID == "" || r.State != "synchronized" {
			continue
		}
		other, e := s.resolveTrackerConfig(r.ProjectID)
		if e == nil && other.Enabled {
			out[r.ExternalID] = r.LocalID
		}
	}
	return out, nil
}
func (s *Server) localTrackerContext(c TrackerConfig, id string) (TrackerConfig, error) {
	var project string
	e := s.db.QueryRow("SELECT project_id FROM milestones WHERE id=?", id).Scan(&project)
	if e == sql.ErrNoRows {
		return c, nil
	}
	if e != nil {
		return TrackerConfig{}, e
	}
	if project == c.ProjectID {
		return c, nil
	}
	return s.resolveTrackerConfig(project)
}
func (s *Server) edgeOwner(c TrackerConfig, d Dependency, scope map[string]string) (string, string, error) {
	local := d.PrerequisiteID
	if d.Type == "related_to" && scope[d.DependentID] < scope[d.PrerequisiteID] {
		local = d.DependentID
	}
	cfg, e := s.localTrackerContext(c, local)
	return cfg.ProjectID, local, e
}
func edgeOwnerTx(tx *sql.Tx, d Dependency) (string, error) {
	local := d.PrerequisiteID
	if d.Type == "related_to" {
		var a, b string
		if e := tx.QueryRow("SELECT external_id FROM tracker_records WHERE local_id=?", d.PrerequisiteID).Scan(&a); e != nil {
			return "", e
		}
		if e := tx.QueryRow("SELECT external_id FROM tracker_records WHERE local_id=?", d.DependentID).Scan(&b); e != nil {
			return "", e
		}
		if b < a {
			local = d.DependentID
		}
	}
	return local, nil
}
func linkMapping(c TrackerConfig, typ string) *TrackerLinkMapping {
	for i := range c.Links {
		if c.Links[i].Type == typ {
			return &c.Links[i]
		}
	}
	return nil
}
func localLinkMapping(c TrackerConfig, typ string) *TrackerLinkMapping {
	for i := range c.Links {
		if c.Links[i].LocalType == typ {
			return &c.Links[i]
		}
	}
	return nil
}
func compatibleLink(a, b TrackerConfig, m TrackerLinkMapping) error {
	other := linkMapping(b, m.Type)
	if other == nil {
		return fmt.Errorf("Dependency synchronization paused: endpoint project has no mapping for Tracker link type %s", m.Type)
	}
	if !reflect.DeepEqual(m, *other) {
		return fmt.Errorf("Dependency configuration conflict: projects %s and %s disagree on Tracker link type %s", a.ProjectID, b.ProjectID, m.Type)
	}
	return nil
}

type trackerDependencyPause struct{ Message string }

func (e trackerDependencyPause) Error() string { return e.Message }
