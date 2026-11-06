package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// Link intents retain exact affected keys, including deletions. Only those local
// relationships and their required remote endpoints participate in an upload.
func (s *Server) outgoingTrackerLinks(ctx context.Context, c TrackerConfig, opID, payload string, generation int64) error {
	var keys map[string]bool
	if json.Unmarshal([]byte(payload), &keys) != nil || len(keys) == 0 {
		return fmt.Errorf("Dependency upload paused: legacy operation requires manual incoming reconciliation")
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	for _, key := range ordered {
		d := parseEdge(key)
		locals := map[string]string{}
		scope := map[string]string{}
		configs := map[string]TrackerConfig{}
		for _, id := range []string{d.PrerequisiteID, d.DependentID} {
			x, e := s.trackerRecord(id)
			if e != nil || x.State != "synchronized" {
				return fmt.Errorf("Dependency upload paused: synchronize both endpoints first")
			}
			ec, e := s.resolveTrackerConfig(x.ProjectID)
			if e != nil || !ec.Enabled {
				return fmt.Errorf("Dependency upload paused: endpoint project disabled")
			}
			locals[x.ExternalID] = id
			scope[id] = x.ExternalID
			configs[id] = ec
		}
		mapping := localLinkMapping(configs[d.PrerequisiteID], d.Type)
		if mapping == nil {
			return fmt.Errorf("Dependency upload paused: missing link mapping")
		}
		if e := compatibleLink(configs[d.DependentID], configs[d.PrerequisiteID], *mapping); e != nil {
			return e
		}
		owner, ownerLocal, e := s.edgeOwner(c, d, scope)
		if e != nil {
			return e
		}
		if owner != c.ProjectID {
			return fmt.Errorf("Dependency upload paused: owner context changed")
		}
		run := trackerRunFrom(ctx)
		connection, _ := s.connectionConfig()
		run.validate = func() error {
			conn, e := s.connectionConfig()
			if e != nil || jsonText(conn) != jsonText(connection) {
				return errTrackerStale
			}
			for id, ec := range configs {
				x, e := s.trackerRecord(id)
				if e != nil || x.ExternalID != scope[id] || x.ProjectID != ec.ProjectID {
					return errTrackerStale
				}
				current, e := s.resolveTrackerConfig(ec.ProjectID)
				if e != nil || jsonText(current) != jsonText(ec) {
					return errTrackerStale
				}
			}
			return nil
		}
		var local int
		e = s.db.QueryRow("SELECT count(*) FROM dependencies WHERE dependency_type=? AND ((prerequisite_milestone_id=? AND dependent_milestone_id=?) OR (?='related_to' AND prerequisite_milestone_id=? AND dependent_milestone_id=?))", d.Type, d.PrerequisiteID, d.DependentID, d.Type, d.DependentID, d.PrerequisiteID).Scan(&local)
		if e != nil {
			return e
		}
		for ext, id := range locals {
			issue, e := s.trackerIssue(ctx, configs[id], ext)
			if e != nil {
				return e
			}
			in, _ := targetedTrackerSelection(configs[id], issue)
			if !in {
				return fmt.Errorf("Dependency upload paused: endpoint outside queue selection; use manual reconciliation to verify query selections")
			}
		}
		remote, _, e := s.remoteEdges(ctx, c, locals)
		if e != nil {
			return e
		}
		var currentGeneration int64
		if e = s.db.QueryRow("SELECT generation FROM tracker_outbox WHERE id=?", opID).Scan(&currentGeneration); e != nil || currentGeneration != generation {
			return errTrackerStale
		}
		r, exists := remote[key]
		desired := local > 0
		if desired != exists {
			if desired {
				_, e = s.trackerRequest(ctx, c, "POST", issuePath(scope[ownerLocal])+"/links", map[string]string{"relationship": mapping.Relationship, "issue": scope[otherEndpoint(d, ownerLocal)]}, nil)
			} else {
				_, e = s.trackerRequest(ctx, c, "DELETE", issuePath(r.Source)+"/links/"+r.LinkID, nil, nil)
			}
			if e != nil {
				return e
			}
			remote, _, e = s.remoteEdges(ctx, c, locals)
			if e != nil {
				return e
			}
			_, exists = remote[key]
			if exists != desired {
				return fmt.Errorf("Dependency upload not confirmed; retry required")
			}
		}
		if _, e = s.db.Exec("INSERT INTO tracker_edges(edge_key,baseline,owner_project_id,error) VALUES(?,?,?,'') ON CONFLICT(edge_key) DO UPDATE SET baseline=excluded.baseline,owner_project_id=excluded.owner_project_id,error=''", key, exists, c.ProjectID); e != nil {
			return e
		}
	}
	_, e := s.db.Exec("DELETE FROM tracker_outbox WHERE id=? AND generation=?", opID, generation)
	return e
}
