package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
)

type TrackerField struct {
	Field    string `json:"field"`
	Writable bool   `json:"writable"`
	Codec    string `json:"codec"`
}
type TrackerSelection struct {
	Queue     string   `json:"queue"`
	Query     string   `json:"query"`
	ProjectID string   `json:"project_id"`
	Project   *Project `json:"new_project,omitempty"`
}
type TrackerLinkMapping struct {
	Type         string `json:"type"`
	Direction    string `json:"direction"`
	LocalType    string `json:"local_type"`
	Relationship string `json:"relationship"`
}
type TrackerConfig struct {
	Enabled       bool                    `json:"enabled"`
	OrgID         string                  `json:"org_id"`
	OrgHeader     string                  `json:"org_header"`
	Auth          string                  `json:"auth"`
	Interval      int                     `json:"poll_interval_seconds"`
	Selections    []TrackerSelection      `json:"selections"`
	Fields        map[string]TrackerField `json:"fields"`
	Statuses      map[string]string       `json:"statuses"`
	WriteStatuses map[string]string       `json:"write_statuses"`
	Links         []TrackerLinkMapping    `json:"links"`
}
type TrackerRecord struct {
	ExternalID   string                     `json:"external_id"`
	ConnectionID string                     `json:"connection_id"`
	Key          string                     `json:"issue_key"`
	LocalID      string                     `json:"local_id"`
	URL          string                     `json:"url"`
	Version      int                        `json:"version"`
	Baseline     map[string]string          `json:"baseline"`
	State        string                     `json:"state"`
	Error        string                     `json:"error"`
	Conflicts    map[string]TrackerConflict `json:"conflicts"`
}
type TrackerConflict struct {
	Baseline string `json:"baseline"`
	Local    string `json:"local"`
	Remote   string `json:"remote"`
}
type TrackerPreviewItem struct {
	ExternalID string            `json:"external_id"`
	Key        string            `json:"issue_key"`
	Version    int               `json:"version"`
	Milestone  Milestone         `json:"milestone"`
	Fields     map[string]string `json:"fields"`
}
type TrackerNotice struct {
	Key     string   `json:"issue_key"`
	Reason  string   `json:"reason"`
	Reasons []string `json:"reasons,omitempty"`
}
type TrackerUnmappedStatus struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}
type TrackerPreview struct {
	UnmappedStatuses []TrackerUnmappedStatus `json:"unmapped_statuses"`
	ID               string                  `json:"id"`
	Projects         []Project               `json:"projects"`
	Items            []TrackerPreviewItem    `json:"milestones"`
	Links            []Dependency            `json:"links"`
	Skipped          []TrackerNotice         `json:"skipped"`
	Warnings         []TrackerNotice         `json:"warnings"`
}

func jsonText(v any) string { b, _ := json.Marshal(v); return string(b) }
func defaultTrackerConfig() TrackerConfig {
	return TrackerConfig{OrgHeader: "X-Org-ID", Auth: "OAuth", Interval: 300, Selections: []TrackerSelection{}, Fields: map[string]TrackerField{"title": {"summary", true, "string"}, "description": {"description", true, "string"}, "planned_date": {"dueDate", true, "date"}, "status": {"status", false, "status"}}, Statuses: map[string]string{}, WriteStatuses: map[string]string{}, Links: []TrackerLinkMapping{}}
}
func (s *Server) trackerConfig() (TrackerConfig, error) {
	c := defaultTrackerConfig()
	var raw string
	e := s.db.QueryRow("SELECT config FROM tracker_connection WHERE id='tracker'").Scan(&raw)
	if e == sql.ErrNoRows {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	c = TrackerConfig{}
	e = json.Unmarshal([]byte(raw), &c)
	return c, e
}

var trackerIssueKeyRE = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_]*)-[0-9]+$`)

func validateTrackerSelection(sel TrackerSelection) error {
	if (sel.Queue == "") == (sel.Query == "") {
		return fmt.Errorf("choose queue or query for each selection")
	}
	if parts := trackerIssueKeyRE.FindStringSubmatch(sel.Queue); parts != nil {
		return fmt.Errorf("Queue selection contains issue key %s; use queue %s or search query Key: %s", sel.Queue, parts[1], sel.Queue)
	}
	return nil
}

var mappedFields = []string{"title", "description", "planned_date", "completed_date", "status", "owner", "version"}

func validateTracker(c TrackerConfig) error {
	if !has([]string{"X-Org-ID", "X-Cloud-Org-ID"}, c.OrgHeader) || !has([]string{"OAuth", "Bearer"}, c.Auth) || c.Auth == "Bearer" && c.OrgHeader != "X-Cloud-Org-ID" {
		return fmt.Errorf("invalid organization header or authentication")
	}
	if strings.TrimSpace(c.OrgID) == "" || strings.ContainsAny(c.OrgID, "\r\n") || c.Interval < 30 || c.Interval > 86400 {
		return fmt.Errorf("organization and polling interval (30–86400 seconds) required")
	}
	if len(c.Selections) == 0 || len(c.Selections) > 50 {
		return fmt.Errorf("configure 1–50 selections")
	}
	if c.Fields["title"].Field != "summary" || c.Fields["title"].Codec != "string" || c.Fields["planned_date"].Field == "" {
		return fmt.Errorf("summary title and planned date mappings required")
	}
	used := map[string]bool{}
	for k, f := range c.Fields {
		if !has(mappedFields, k) || f.Field == "" || used[f.Field] || !has([]string{"string", "date", "id", "single_id", "status"}, f.Codec) {
			return fmt.Errorf("invalid or duplicate field mapping: %s", k)
		}
		used[f.Field] = true
		if k == "status" && (f.Field != "status" || f.Codec != "status") {
			return fmt.Errorf("status requires status codec")
		}
		if (k == "planned_date" || k == "completed_date") && f.Codec != "date" {
			return fmt.Errorf("dates require date codec")
		}
		if has([]string{"id", "key", "unique", "queue", "version", "status"}, f.Field) && k != "status" {
			return fmt.Errorf("reserved Tracker field")
		}
	}
	for _, v := range c.Statuses {
		if !has([]string{"planned", "in_progress", "completed", "delayed", "cancelled"}, v) {
			return fmt.Errorf("invalid local status")
		}
	}
	for local, remote := range c.WriteStatuses {
		if c.Statuses[remote] != local {
			return fmt.Errorf("write status must map back to local status")
		}
	}
	for _, sel := range c.Selections {
		if e := validateTrackerSelection(sel); e != nil {
			return e
		}
		if sel.ProjectID == "" && sel.Project == nil {
			return fmt.Errorf("selection requires a project")
		}
		if sel.Project != nil {
			if sel.ProjectID != "" {
				return fmt.Errorf("choose existing or new project")
			}
			if e := validateProject(*sel.Project); e != nil {
				return e
			}
		}
	}
	keys := map[string]bool{}
	localTypes := map[string]bool{}
	for _, l := range c.Links {
		key := l.Type
		if keys[key] || localTypes[l.LocalType] || l.Type == "" || !has([]string{"inward", "outward"}, l.Direction) || !has([]string{"requires", "related_to"}, l.LocalType) || !has([]string{"is dependent by", "depends on", "relates", "is subtask for", "is parent task for"}, l.Relationship) {
			return fmt.Errorf("invalid or duplicate link mapping")
		}
		keys[key] = true
		localTypes[l.LocalType] = true
	}
	return nil
}
func (s *Server) trackerRecords() ([]TrackerRecord, error) {
	out := []TrackerRecord{}
	rows, e := s.db.Query("SELECT external_id,connection_id,issue_key,COALESCE(local_id,''),url,version,baseline,state,error,conflicts FROM tracker_records ORDER BY issue_key")
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var x TrackerRecord
		var b, c string
		if e = rows.Scan(&x.ExternalID, &x.ConnectionID, &x.Key, &x.LocalID, &x.URL, &x.Version, &b, &x.State, &x.Error, &c); e != nil {
			return out, e
		}
		json.Unmarshal([]byte(b), &x.Baseline)
		json.Unmarshal([]byte(c), &x.Conflicts)
		out = append(out, x)
	}
	return out, rows.Err()
}
func localFields(m Milestone) map[string]string {
	return map[string]string{"title": m.Title, "description": m.Description, "planned_date": m.PlannedDate, "completed_date": m.CompletedDate, "status": m.Status, "owner": m.Owner, "version": m.Version}
}
func setFields(m *Milestone, f map[string]string) {
	for k, v := range f {
		switch k {
		case "title":
			m.Title = v
		case "description":
			m.Description = v
		case "planned_date":
			m.PlannedDate = v
		case "completed_date":
			m.CompletedDate = v
		case "status":
			m.Status = v
		case "owner":
			m.Owner = v
		case "version":
			m.Version = v
		}
	}
}
func remoteFields(issue map[string]any, c TrackerConfig) (map[string]string, error) {
	out := map[string]string{}
	for k, f := range c.Fields {
		v := issue[f.Field]
		s := ""
		if v != nil {
			switch f.Codec {
			case "string", "date":
				var ok bool
				s, ok = v.(string)
				if !ok {
					return nil, fmt.Errorf("field %s is not a scalar string", f.Field)
				}
			case "id", "status":
				if o, ok := v.(map[string]any); ok {
					if f.Codec == "status" {
						s, _ = o["key"].(string)
					} else {
						s = fmt.Sprint(o["id"])
					}
				} else {
					s, _ = v.(string)
				}
			case "single_id":
				a, ok := v.([]any)
				if !ok || len(a) > 1 {
					return nil, fmt.Errorf("field %s requires zero or one version", f.Field)
				}
				if len(a) == 1 {
					o, ok := a[0].(map[string]any)
					if !ok {
						return nil, fmt.Errorf("invalid version field")
					}
					s = fmt.Sprint(o["id"])
				}
			}
		}
		if f.Codec == "date" && s != "" && !date(s) {
			return nil, fmt.Errorf("field %s requires ISO date-only data", f.Field)
		}
		if f.Codec == "status" {
			var ok bool
			s, ok = c.Statuses[s]
			if !ok {
				return nil, fmt.Errorf("unmapped Tracker status: %s", trackerStatusKey(issue[f.Field]))
			}
		}
		out[k] = s
	}
	return out, nil
}
func trackerStatusKey(v any) string {
	if status, ok := v.(map[string]any); ok {
		key, _ := status["key"].(string)
		return key
	}
	key, _ := v.(string)
	return key
}

func remoteValue(v string, f TrackerField) any {
	if f.Codec == "single_id" && v == "" {
		return []any{}
	}
	if v == "" {
		return nil
	}
	if f.Codec == "single_id" {
		return []any{map[string]string{"id": v}}
	}
	if f.Codec == "id" {
		return map[string]string{"id": v}
	}
	return v
}
func issueIdentity(v map[string]any) (string, string, int, error) {
	id, _ := v["id"].(string)
	key, _ := v["key"].(string)
	ver, _ := v["version"].(float64)
	if id == "" || key == "" || ver < 1 {
		return "", "", 0, fmt.Errorf("issue identity/version missing")
	}
	return id, key, int(ver), nil
}
func trackerURL(key string) string { return "https://tracker.yandex.ru/" + url.PathEscape(key) }
func insertMilestone(tx *sql.Tx, m Milestone) error {
	_, e := tx.Exec("INSERT INTO milestones VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)", m.ID, m.ProjectID, m.Title, m.Description, m.Type, m.Version, m.PlannedDate, m.CompletedDate, m.Status, m.Owner, m.ExternalURL, m.CreatedAt, m.UpdatedAt)
	return e
}
func updateMilestone(tx *sql.Tx, m Milestone) error {
	_, e := tx.Exec("UPDATE milestones SET project_id=?,title=?,description=?,milestone_type=?,version=?,planned_date=?,completed_date=?,status=?,owner=?,external_url=?,updated_at=? WHERE id=?", m.ProjectID, m.Title, m.Description, m.Type, m.Version, m.PlannedDate, m.CompletedDate, m.Status, m.Owner, m.ExternalURL, now(), m.ID)
	return e
}
func saveRecord(tx *sql.Tx, x TrackerRecord) error {
	var local any = x.LocalID
	if x.LocalID == "" {
		local = nil
	}
	_, e := tx.Exec(`INSERT INTO tracker_records(external_id,connection_id,issue_key,local_id,url,version,baseline,state,error,conflicts) VALUES(?,'tracker',?,?,?,?,?,?,?,?) ON CONFLICT(external_id) DO UPDATE SET issue_key=excluded.issue_key,local_id=excluded.local_id,url=excluded.url,version=excluded.version,baseline=excluded.baseline,state=excluded.state,error=excluded.error,conflicts=excluded.conflicts`, x.ExternalID, x.Key, local, x.URL, x.Version, jsonText(x.Baseline), x.State, x.Error, jsonText(x.Conflicts))
	return e
}
func enqueue(tx *sql.Tx, id, kind, payload string) error {
	_, e := tx.Exec("INSERT INTO tracker_outbox(id,local_id,kind,payload) VALUES(?,?,?,?) ON CONFLICT(local_id,kind) DO NOTHING", token(), id, kind, payload)
	return e
}
func (s *Server) fingerprint() (string, error) {
	g, e := s.graph()
	if e != nil {
		return "", e
	}
	// Sync bookkeeping timestamps alone do not invalidate a reviewable preview.
	for i := range g.Milestones {
		g.Milestones[i].UpdatedAt = ""
	}
	for i := range g.Projects {
		g.Projects[i].UpdatedAt = ""
	}
	c, e := s.trackerConfig()
	if e != nil {
		return "", e
	}
	records, e := s.trackerRecords()
	if e != nil {
		return "", e
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(jsonText([]any{g, c, records})))), nil
}
func checkEdges(g Graph, ds []Dependency) error {
	for _, d := range ds {
		if d.PrerequisiteID == d.DependentID {
			return fmt.Errorf("self dependency: %s", d.PrerequisiteID)
		}
		if d.Type == "requires" && has(Reach(g.Dependencies, d.DependentID, false), d.PrerequisiteID) {
			return fmt.Errorf("hard dependency cycle: %s → %s", d.PrerequisiteID, d.DependentID)
		}
		exists := false
		for _, old := range g.Dependencies {
			if edgeKey(old) == edgeKey(d) {
				exists = true
			}
		}
		if !exists {
			g.Dependencies = append(g.Dependencies, d)
		}
	}
	return nil
}
func edgeKey(d Dependency) string {
	a, b := d.PrerequisiteID, d.DependentID
	if d.Type == "related_to" && a > b {
		a, b = b, a
	}
	return a + "|" + b + "|" + d.Type
}
func parseEdge(k string) Dependency {
	p := strings.Split(k, "|")
	return Dependency{PrerequisiteID: p[0], DependentID: p[1], Type: p[2]}
}
func (s *Server) makePreview(ctx context.Context, c TrackerConfig) (TrackerPreview, error) {
	for _, sel := range c.Selections {
		if e := validateTrackerSelection(sel); e != nil {
			return TrackerPreview{}, e
		}
	}
	p := TrackerPreview{ID: token(), Projects: []Project{}, Items: []TrackerPreviewItem{}, Links: []Dependency{}, Skipped: []TrackerNotice{}, Warnings: []TrackerNotice{}, UnmappedStatuses: []TrackerUnmappedStatus{}}
	g, e := s.graph()
	if e != nil {
		return p, e
	}
	records, e := s.trackerRecords()
	if e != nil {
		return p, e
	}
	known := map[string]TrackerRecord{}
	for _, x := range records {
		known[x.ExternalID] = x
	}
	issues := map[string]map[string]any{}
	locals := map[string]string{}
	seen := map[string]string{}
	unmapped := map[string]*TrackerUnmappedStatus{}
	for _, sel := range c.Selections {
		pid := sel.ProjectID
		if sel.Project != nil {
			pr := *sel.Project
			pr.ID = fmt.Sprintf("tracker-project-%x", sha256.Sum256([]byte(pr.Slug)))
			pr.CreatedAt = now()
			pr.UpdatedAt = pr.CreatedAt
			for _, existing := range g.Projects {
				if existing.Slug == pr.Slug {
					pr = existing
				}
			}
			pid = pr.ID
			if strings.HasPrefix(pid, "tracker-project-") {
				p.Projects = append(p.Projects, pr)
			}
		} else {
			exists := false
			for _, pr := range g.Projects {
				if pr.ID == pid {
					exists = true
				}
			}
			if !exists {
				return p, fmt.Errorf("selection project not found")
			}
		}
		body := map[string]any{"filter": map[string]string{"queue": sel.Queue}}
		if sel.Query != "" {
			body = map[string]any{"query": sel.Query}
		}
		batch, e := s.tracker.search(ctx, c, body)
		if e != nil {
			return p, e
		}
		for _, v := range batch {
			id, key, ver, e := issueIdentity(v)
			if e != nil {
				return p, e
			}
			if old, ok := seen[id]; ok {
				if old != pid {
					return p, fmt.Errorf("issue %s matches selections with different projects", key)
				}
				continue
			}
			seen[id] = pid
			if x, ok := known[id]; ok {
				if x.State != "unlinked" && x.LocalID != "" {
					locals[id] = x.LocalID
					issues[id] = v
				}
				continue
			}
			if mapping, ok := c.Fields["status"]; ok {
				statusKey := trackerStatusKey(v[mapping.Field])
				if _, mapped := c.Statuses[statusKey]; !mapped && statusKey != "" {
					item := unmapped[statusKey]
					if item == nil {
						name := statusKey
						if raw, ok := v[mapping.Field].(map[string]any); ok {
							if display, ok := raw["display"].(string); ok && display != "" {
								name = display
							}
						}
						item = &TrackerUnmappedStatus{Key: statusKey, Name: name}
						unmapped[statusKey] = item
					}
					item.Count++
				}
			}
			f, e := remoteFields(v, c)
			reasons := []string{}
			if e != nil {
				reasons = append(reasons, e.Error())
			}
			planned := c.Fields["planned_date"].Field
			if v[planned] == nil || v[planned] == "" {
				reasons = append(reasons, fmt.Sprintf("Missing planned date: Tracker field %s is empty", planned))
			}
			if len(reasons) > 0 {
				p.Skipped = append(p.Skipped, TrackerNotice{Key: key, Reason: strings.Join(reasons, "; "), Reasons: reasons})
				continue
			}
			m := Milestone{ID: token(), ProjectID: pid, Type: "release", Status: "planned", ExternalURL: trackerURL(key), CreatedAt: now(), UpdatedAt: now()}
			setFields(&m, f)
			if e = validateMilestone(m); e != nil {
				p.Skipped = append(p.Skipped, TrackerNotice{Key: key, Reason: e.Error()})
				continue
			}
			locals[id] = m.ID
			issues[id] = v
			p.Items = append(p.Items, TrackerPreviewItem{id, key, ver, m, f})
		}
	}
	for _, status := range unmapped {
		p.UnmappedStatuses = append(p.UnmappedStatuses, *status)
	}
	sort.Slice(p.UnmappedStatuses, func(i, j int) bool { return p.UnmappedStatuses[i].Key < p.UnmappedStatuses[j].Key })
	edges, warnings, e := s.fetchEdges(ctx, c, locals)
	if e != nil {
		return p, e
	}
	p.Warnings = append(p.Warnings, warnings...)
	newIDs := map[string]bool{}
	for _, it := range p.Items {
		newIDs[it.Milestone.ID] = true
	}
	for _, d := range edges {
		if newIDs[d.PrerequisiteID] || newIDs[d.DependentID] {
			p.Links = append(p.Links, d)
		}
	}
	if e = checkEdges(g, p.Links); e != nil {
		return p, e
	}
	return p, nil
}
func (s *Server) applyPreview(p TrackerPreview) error {
	g, e := s.graph()
	if e != nil {
		return e
	}
	if e = checkEdges(g, p.Links); e != nil {
		return e
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, pr := range p.Projects {
		if _, e = tx.Exec("INSERT OR IGNORE INTO projects VALUES(?,?,?,?,?,?,?,?,?)", pr.ID, pr.Name, pr.Slug, pr.Description, pr.Color, pr.Icon, pr.SortOrder, pr.CreatedAt, pr.UpdatedAt); e != nil {
			return e
		}
	}
	for _, it := range p.Items {
		if e = insertMilestone(tx, it.Milestone); e != nil {
			return e
		}
		if e = saveRecord(tx, TrackerRecord{ExternalID: it.ExternalID, Key: it.Key, LocalID: it.Milestone.ID, URL: it.Milestone.ExternalURL, Version: it.Version, Baseline: it.Fields, State: "synchronized", Conflicts: map[string]TrackerConflict{}}); e != nil {
			return e
		}
	}
	for _, d := range p.Links {
		if _, e = tx.Exec("INSERT OR IGNORE INTO dependencies VALUES(?,?,?,?,?)", token(), d.PrerequisiteID, d.DependentID, d.Type, now()); e != nil {
			return e
		}
		if _, e = tx.Exec("INSERT INTO tracker_edges VALUES(?,1) ON CONFLICT(edge_key) DO UPDATE SET baseline=1", edgeKey(d)); e != nil {
			return e
		}
	}
	if _, e = tx.Exec("DELETE FROM tracker_previews"); e != nil {
		return e
	}
	return tx.Commit()
}

// StartTracker runs the optional poller within the existing server process.
func (s *Server) StartTracker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		next := time.Now()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if time.Now().Before(next) {
					continue
				}
				s.writeMu.Lock()
				c, e := s.trackerConfig()
				if e == nil && c.Enabled {
					run, cancel := context.WithTimeout(ctx, 10*time.Minute)
					s.syncTracker(run, c)
					cancel()
				}
				s.writeMu.Unlock()
				interval := c.Interval
				if interval < 30 {
					interval = 30
				}
				next = time.Now().Add(time.Duration(interval) * time.Second)
			}
		}
	}()
}

// Field identity/format and existing status meanings anchor persisted baselines.
// Write permissions, outgoing status targets and additional status keys can change safely.
func sameConfigMapping(a, b TrackerConfig) bool {
	if a.OrgID != b.OrgID || a.OrgHeader != b.OrgHeader || !reflect.DeepEqual(a.Links, b.Links) || len(a.Fields) != len(b.Fields) {
		return false
	}
	for k, f := range a.Fields {
		other, ok := b.Fields[k]
		if !ok || f.Field != other.Field || f.Codec != other.Codec {
			return false
		}
	}
	for k, v := range a.Statuses {
		if b.Statuses[k] != v {
			return false
		}
	}
	return true
}
