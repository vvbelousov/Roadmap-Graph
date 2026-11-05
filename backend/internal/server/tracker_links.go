package server

import (
	"context"
	"fmt"
	"sort"
	"time"
)

var errTrackerRetryWaiting = fmt.Errorf("outgoing link operation waiting for retry")

type trackerRemoteLink struct {
	ID   any `json:"id"`
	Type struct {
		ID string `json:"id"`
	} `json:"type"`
	Direction string `json:"direction"`
	Object    struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	} `json:"object"`
}
type trackerEdge struct {
	Dependency
	Source  string
	LinkID  string
	Mapping TrackerLinkMapping
}

func (s *Server) fetchEdges(ctx context.Context, c TrackerConfig, locals map[string]string) (map[string]Dependency, []TrackerNotice, error) {
	remote, w, e := s.remoteEdges(ctx, c, locals)
	out := map[string]Dependency{}
	for k, v := range remote {
		out[k] = v.Dependency
	}
	return out, w, e
}
func (s *Server) remoteEdges(ctx context.Context, c TrackerConfig, locals map[string]string) (map[string]trackerEdge, []TrackerNotice, error) {
	out := map[string]trackerEdge{}
	warnings := []TrackerNotice{}

	types := []string{}
	for _, m := range c.Links {
		types = append(types, m.Type)
	}
	ids := []string{}
	for id := range locals {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		sourceCfg, e := s.localTrackerContext(c, locals[id])
		if e != nil {
			return nil, warnings, e
		}
		if !sourceCfg.Enabled {
			continue
		}
		types = []string{}
		for _, mapping := range sourceCfg.Links {
			types = append(types, mapping.Type)
		}
		if len(types) == 0 {
			continue
		}
		for page := 1; page <= 10000; page++ {
			var v struct {
				Links []trackerRemoteLink `json:"links"`
			}
			_, e := s.tracker.request(ctx, c, "POST", fmt.Sprintf("%s/links/_list?perPage=100&page=%d", issuePath(id), page), map[string]any{"fields": []string{}, "linkTypes": types}, &v)
			if e != nil {
				return nil, warnings, e
			}
			for _, l := range v.Links {
				for _, m := range sourceCfg.Links {
					if m.Type != l.Type.ID {
						continue
					}
					other, ok := locals[l.Object.ID]
					if !ok {
						if sourceCfg.ProjectID != c.ProjectID {
							continue
						}
						warnings = append(warnings, TrackerNotice{Key: id, Reason: "linked issue outside selection: " + l.Object.Key})
						continue
					}
					if l.Direction != "inward" && l.Direction != "outward" {
						return nil, warnings, fmt.Errorf("unknown link direction")
					}
					a, b := locals[id], other
					if l.Direction != m.Direction {
						a, b = b, a
					}
					d := Dependency{PrerequisiteID: a, DependentID: b, Type: m.LocalType}
					k := edgeKey(d)
					otherCfg, e := s.localTrackerContext(c, other)
					if e != nil {
						return nil, warnings, e
					}
					if sourceCfg.ProjectID != c.ProjectID && otherCfg.ProjectID != c.ProjectID {
						continue
					}
					if e = compatibleLink(sourceCfg, otherCfg, m); e != nil {
						warnings = append(warnings, TrackerNotice{Key: id, Reason: e.Error(), EdgeKey: k})
						continue
					}
					if d.Type == "related_to" {
						d = parseEdge(k)
					}
					out[k] = trackerEdge{d, id, fmt.Sprint(l.ID), m}
				}
			}
			if len(v.Links) < 100 {
				break
			}
			if page == 10000 {
				return nil, warnings, fmt.Errorf("link pagination limit")
			}
		}
	}
	return out, warnings, nil
}
func (s *Server) reconcileEdges(ctx context.Context, c TrackerConfig, locals map[string]string) error {
	remote, warnings, e := s.remoteEdges(ctx, c, locals)
	if e != nil {
		return e
	}
	g, e := s.graph()
	if e != nil {
		return e
	}
	scope := map[string]string{}
	for ext, id := range locals {
		scope[id] = ext
	}
	blocked := map[string]bool{}
	for _, notice := range warnings {
		if notice.EdgeKey != "" {
			blocked[notice.EdgeKey] = true
		}
	}
	bound := map[string]TrackerRecord{}
	records, e := s.trackerRecords()
	if e != nil {
		return e
	}
	for _, record := range records {
		if record.LocalID != "" && record.State != "unlinked" {
			bound[record.LocalID] = record
		}
	}
	for _, d := range g.Dependencies {
		a, aok := bound[d.PrerequisiteID]
		b, bok := bound[d.DependentID]
		if aok && bok && (a.ProjectID == c.ProjectID || b.ProjectID == c.ProjectID) && a.ProjectID != b.ProjectID {
			if scope[d.PrerequisiteID] == "" || scope[d.DependentID] == "" {
				blocked[edgeKey(d)] = true
				warnings = append(warnings, TrackerNotice{Key: a.Key, Reason: "Dependency synchronization paused: endpoint project disabled, outside scope, conflicted or unavailable", EdgeKey: edgeKey(d)})
				continue
			}
			ac, e := s.localTrackerContext(c, d.PrerequisiteID)
			if e != nil {
				return e
			}
			bc, e := s.localTrackerContext(c, d.DependentID)
			if e != nil {
				return e
			}
			mapping := localLinkMapping(ac, d.Type)
			if mapping == nil {
				blocked[edgeKey(d)] = true
				warnings = append(warnings, TrackerNotice{Key: a.Key, Reason: "Dependency synchronization paused: endpoint mapping unavailable", EdgeKey: edgeKey(d)})
			} else if e = compatibleLink(ac, bc, *mapping); e != nil {
				blocked[edgeKey(d)] = true
				warnings = append(warnings, TrackerNotice{Key: a.Key, Reason: e.Error(), EdgeKey: edgeKey(d)})
			}
		}
	}
	for k := range remote {
		owner, _, e := s.edgeOwner(c, remote[k].Dependency, scope)
		if e != nil {
			return e
		}
		if owner != c.ProjectID || blocked[k] {
			delete(remote, k)
		}
	}
	local := map[string]Dependency{}
	for _, d := range g.Dependencies {
		if scope[d.PrerequisiteID] != "" && scope[d.DependentID] != "" && !blocked[edgeKey(d)] {
			owner, _, e := s.edgeOwner(c, d, scope)
			if e != nil {
				return e
			}
			if owner != c.ProjectID {
				continue
			}
			for _, m := range c.Links {
				if d.Type == m.LocalType {
					local[edgeKey(d)] = d
				}
			}
		}
	}
	baseline := map[string]bool{}
	rows, e := s.db.Query("SELECT edge_key,baseline FROM tracker_edges")
	if e != nil {
		return e
	}
	for rows.Next() {
		var k string
		var b bool
		if e = rows.Scan(&k, &b); e != nil {
			rows.Close()
			return e
		}
		baseline[k] = b
	}
	rows.Close()
	for k := range baseline {
		d := parseEdge(k)
		a, aok := bound[d.PrerequisiteID]
		b, bok := bound[d.DependentID]
		if aok && bok && a.ProjectID != b.ProjectID && (a.ProjectID == c.ProjectID || b.ProjectID == c.ProjectID) && (scope[d.PrerequisiteID] == "" || scope[d.DependentID] == "") {
			blocked[k] = true
			warnings = append(warnings, TrackerNotice{Key: a.Key, Reason: "Dependency synchronization paused: endpoint project disabled, pending, outside scope or unavailable", EdgeKey: k})
		}
	}
	for k := range baseline {
		d := parseEdge(k)
		if scope[d.PrerequisiteID] == "" || scope[d.DependentID] == "" || blocked[k] {
			delete(baseline, k)
			continue
		}
		owner, _, e := s.edgeOwner(c, d, scope)
		if e != nil {
			return e
		}
		if owner != c.ProjectID {
			delete(baseline, k)
		}
	}
	for _, notice := range warnings {
		if notice.EdgeKey != "" {
			s.db.Exec("INSERT INTO tracker_edges(edge_key,baseline,error) VALUES(?,0,?) ON CONFLICT(edge_key) DO UPDATE SET error=excluded.error", notice.EdgeKey, notice.Reason)
		}
	}
	keys := map[string]bool{}
	for k := range remote {
		keys[k] = true
	}
	for k := range local {
		keys[k] = true
	}
	for k := range baseline {
		keys[k] = true
	}
	// Validate the complete proposed incoming set before touching local links.
	proposed := g
	proposed.Dependencies = []Dependency{}
	for _, d := range g.Dependencies {
		_, l := local[edgeKey(d)]
		_, r := remote[edgeKey(d)]
		b := baseline[edgeKey(d)]
		if l && l == b && !r {
			continue
		}
		proposed.Dependencies = append(proposed.Dependencies, d)
	}
	incoming := []Dependency{}
	for k, r := range remote {
		_, l := local[k]
		if !l && !baseline[k] {
			incoming = append(incoming, r.Dependency)
		}
	}
	if e = checkEdges(proposed, incoming); e != nil {
		return e
	}
	ordered := []string{}
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	for _, k := range ordered {
		d := parseEdge(k)
		if scope[d.PrerequisiteID] == "" || scope[d.DependentID] == "" {
			continue
		}
		owner, ownerLocal, e := s.edgeOwner(c, d, scope)
		if e != nil {
			return e
		}
		if owner != c.ProjectID || blocked[k] {
			continue
		}
		_, l := local[k]
		r, exists := remote[k]
		b := baseline[k]
		if l != exists {
			if l == b {
				tx, e := s.db.Begin()
				if e != nil {
					return e
				}
				if exists {
					_, e = tx.Exec("INSERT OR IGNORE INTO dependencies VALUES(?,?,?,?,?)", token(), d.PrerequisiteID, d.DependentID, d.Type, now())
				} else {
					if d.Type == "related_to" {
						_, e = tx.Exec("DELETE FROM dependencies WHERE dependency_type='related_to' AND ((prerequisite_milestone_id=? AND dependent_milestone_id=?) OR (prerequisite_milestone_id=? AND dependent_milestone_id=?))", d.PrerequisiteID, d.DependentID, d.DependentID, d.PrerequisiteID)
					} else {
						_, e = tx.Exec("DELETE FROM dependencies WHERE prerequisite_milestone_id=? AND dependent_milestone_id=? AND dependency_type=?", d.PrerequisiteID, d.DependentID, d.Type)
					}
				}
				if e == nil {
					_, e = tx.Exec("INSERT INTO tracker_edges(edge_key,baseline,owner_project_id,error) VALUES(?,?,?,'') ON CONFLICT(edge_key) DO UPDATE SET baseline=excluded.baseline,owner_project_id=excluded.owner_project_id,error=''", k, exists, c.ProjectID)
				}
				if e != nil {
					tx.Rollback()
					return e
				}
				if e = tx.Commit(); e != nil {
					return e
				}
				continue
			} else if l {
				if e = s.ensureLinkIntent(ownerLocal); e != nil {
					return e
				}
				var blocked int
				if e = s.db.QueryRow("SELECT count(*) FROM tracker_outbox WHERE kind='links' AND local_id IN (?,?) AND (attempts>=8 OR next_attempt>?)", d.PrerequisiteID, d.DependentID, time.Now().Unix()).Scan(&blocked); e != nil {
					return e
				}
				if blocked > 0 {
					return errTrackerRetryWaiting
				}
				var mapping *TrackerLinkMapping
				for i := range c.Links {
					if c.Links[i].LocalType == d.Type {
						mapping = &c.Links[i]
						break
					}
				}
				if mapping == nil {
					continue
				}
				_, e = s.tracker.request(ctx, c, "POST", issuePath(scope[ownerLocal])+"/links", map[string]string{"relationship": mapping.Relationship, "issue": scope[otherEndpoint(d, ownerLocal)]}, nil)
			} else {
				if e = s.ensureLinkIntent(ownerLocal); e != nil {
					return e
				}
				var blocked int
				if e = s.db.QueryRow("SELECT count(*) FROM tracker_outbox WHERE kind='links' AND local_id IN (?,?) AND (attempts>=8 OR next_attempt>?)", d.PrerequisiteID, d.DependentID, time.Now().Unix()).Scan(&blocked); e != nil {
					return e
				}
				if blocked > 0 {
					return errTrackerRetryWaiting
				}
				_, e = s.tracker.request(ctx, c, "DELETE", issuePath(r.Source)+"/links/"+r.LinkID, nil, nil)
			}
			if e != nil {
				return e
			}
		}
		if _, e = s.db.Exec("INSERT INTO tracker_edges(edge_key,baseline,owner_project_id,error) VALUES(?,?,?,'') ON CONFLICT(edge_key) DO UPDATE SET baseline=excluded.baseline,owner_project_id=excluded.owner_project_id,error=''", k, l, c.ProjectID); e != nil {
			return e
		}
	}
	for _, notice := range warnings {
		if notice.EdgeKey != "" {
			return trackerDependencyPause{Message: notice.Reason}
		}
	}
	return nil
}

func (s *Server) ensureLinkIntent(id string) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = enqueue(tx, id, "links", "{}"); e != nil {
		return e
	}
	return tx.Commit()
}

func otherEndpoint(d Dependency, source string) string {
	if source == d.PrerequisiteID {
		return d.DependentID
	}
	return d.PrerequisiteID
}
