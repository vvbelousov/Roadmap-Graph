package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func anotherProject(c *client, slug string) Project {
	c.t.Helper()
	var p Project
	json.Unmarshal(c.expect("POST", "/api/projects", fmt.Sprintf(`{"name":%q,"slug":%q,"color":"#60a5fa"}`, slug, slug), 201), &p)
	return p
}
func saveProjectConfig(c *client, id string, p ProjectTrackerConfig) ProjectTrackerConfig {
	c.t.Helper()
	p.Revision = readProjectTestConfig(c, id).Revision
	var saved ProjectTrackerConfig
	json.Unmarshal(c.expect("PUT", "/api/projects/"+id+"/tracker/config", jsonText(p), 200), &saved)
	return saved
}
func importProject(c *client, id string) TrackerPreview {
	c.t.Helper()
	var preview TrackerPreview
	json.Unmarshal(c.expect("POST", "/api/projects/"+id+"/tracker/preview", "{}", 200), &preview)
	c.expect("POST", "/api/projects/"+id+"/tracker/import", jsonText(map[string]string{"preview_id": preview.ID}), 200)
	return preview
}
func projectPair(t *testing.T) (*client, *fakeTracker, TrackerConfig, Project, ProjectTrackerConfig) {
	c, f, a := trackerSetup(t)
	b := anotherProject(c, "beta")
	var cfg ProjectTrackerConfig
	json.Unmarshal([]byte(jsonText(projectPart(a))), &cfg)
	cfg.Revision = 0
	cfg.Selections = []ProjectTrackerSelection{{ID: "beta-destination", Queue: "BETA"}}
	cfg.Fields["planned_date"] = TrackerField{"shipDate", true, "date"}
	cfg.Statuses["open"] = "in_progress"
	delete(cfg.WriteStatuses, "planned")
	cfg.WriteStatuses["in_progress"] = "open"
	cfg = saveProjectConfig(c, b.ID, cfg)
	return c, f, a, b, cfg
}

func TestProjectTrackerIsolationIncomingOutgoingPauseAndStaleSaves(t *testing.T) {
	c, f, a, b, bp := projectPair(t)
	f.add("1", "Core")
	f.add("2", "Beta")
	f.issues["2"]["queue"] = "BETA"
	f.issues["2"]["shipDate"] = "2027-05-12"
	importProject(c, a.ProjectID)
	importProject(c, b.ID)
	am, bm := localByExternal(t, c.s, "1"), localByExternal(t, c.s, "2")
	if am.Status != "planned" || bm.Status != "in_progress" || bm.PlannedDate != "2027-05-12" {
		t.Fatal(am, bm)
	}
	before := jsonText(readProjectTestConfig(c, b.ID))
	ap := readProjectTestConfig(c, a.ProjectID)
	ap.Fields["description"] = TrackerField{"description", false, "string"}
	saveProjectConfig(c, a.ProjectID, ap)
	if jsonText(readProjectTestConfig(c, b.ID)) != before {
		t.Fatal("saving A changed B")
	}
	c.expect("PATCH", "/api/milestones/"+am.ID, `{"description":"blocked"}`, 409)
	c.expect("PATCH", "/api/milestones/"+bm.ID, `{"description":"Beta edit","planned_date":"2027-05-14"}`, 200)
	if e := c.s.syncTracker(context.Background(), TrackerConfig{}); e != nil {
		t.Fatal(e)
	}
	if f.issues["2"]["shipDate"] != "2027-05-14" || f.issues["2"]["dueDate"] != "2027-02-01" || f.issues["2"]["description"] != "Beta edit" {
		t.Fatal(f.issues["2"])
	}
	// Stale revision, disabled projects and ordinary local projects.
	stale := bp
	bp = saveProjectConfig(c, b.ID, bp)
	c.expect("PUT", "/api/projects/"+b.ID+"/tracker/config", jsonText(stale), 409)
	bp.Enabled = false
	saveProjectConfig(c, b.ID, bp)
	c.expect("PATCH", "/api/milestones/"+bm.ID, `{"title":"Paused Beta edit"}`, 200)
	f.issues["1"]["summary"] = "Updated Core"
	f.issues["2"]["summary"] = "Remote Beta edit"
	if e := c.s.syncTracker(context.Background(), TrackerConfig{}); e != nil {
		t.Fatal(e)
	}
	if localByExternal(t, c.s, "1").Title != "Updated Core" || localByExternal(t, c.s, "2").Title != "Paused Beta edit" || f.issues["2"]["summary"] != "Remote Beta edit" {
		t.Fatal("disabled project changed")
	}
	var pending int
	c.s.db.QueryRow("SELECT count(*) FROM tracker_outbox WHERE project_id=?", b.ID).Scan(&pending)
	if pending != 1 {
		t.Fatal("paused intent lost")
	}
	local := anotherProject(c, "local")
	c.expect("PUT", "/api/projects/"+local.ID+"/tracker/config", `{"enabled":false,"revision":0}`, 200)
	milestone(c, local, "Ordinary local", "2027-01-01")
}
func TestProjectScopedPreviewOwnershipOverlapAndLegacyAPIRejection(t *testing.T) {
	c, f, a, b, _ := projectPair(t)
	f.add("1", "Core")
	var preview TrackerPreview
	json.Unmarshal(c.expect("POST", "/api/projects/"+a.ProjectID+"/tracker/preview", "{}", 200), &preview)
	c.expect("POST", "/api/projects/"+b.ID+"/tracker/import", jsonText(map[string]string{"preview_id": preview.ID}), 409)
	c.expect("POST", "/api/projects/"+a.ProjectID+"/tracker/import", jsonText(map[string]string{"preview_id": preview.ID}), 200)
	bp := readProjectTestConfig(c, b.ID)
	bp.Selections[0].Queue = "CORE"
	saveProjectConfig(c, b.ID, bp)
	json.Unmarshal(c.expect("POST", "/api/projects/"+b.ID+"/tracker/preview", "{}", 200), &preview)
	if len(preview.Items) != 0 || len(preview.Skipped) != 1 || !strings.Contains(preview.Skipped[0].Reason, "another Roadmap project") {
		t.Fatal(preview)
	}
	c.expect("PUT", "/api/tracker/config", jsonText(a), 400)
	c.expect("POST", "/api/tracker/preview", "{}", 410)
	c.expect("PUT", "/api/projects/"+a.ProjectID+"/tracker/config", `{"enabled":false,"revision":0,"selections":[{"id":"bad","project_id":"other"}]}`, 400)
	c.expect("GET", "/api/projects/nonexistent/tracker", "", 404)
}
func TestProjectPublicationStableSelectionContextMoveAndDeletion(t *testing.T) {
	c, f, a, b, _ := projectPair(t)
	g, _ := c.s.graph()
	var projectA Project
	for _, p := range g.Projects {
		if p.ID == a.ProjectID {
			projectA = p
		}
	}
	m := milestone(c, projectA, "Publish", "2027-04-01")
	c.expect("POST", "/api/tracker/milestones/"+m.ID+"/publish", `{"selection_id":"beta-destination"}`, 422)
	ap := readProjectTestConfig(c, a.ProjectID)
	ap.Selections = append(ap.Selections, ProjectTrackerSelection{ID: "second", Queue: "SECOND"})
	ap = saveProjectConfig(c, a.ProjectID, ap)
	c.expect("POST", "/api/tracker/milestones/"+m.ID+"/publish", `{"selection_id":"core-selection"}`, 202)
	ap.Selections[0], ap.Selections[1] = ap.Selections[1], ap.Selections[0]
	ap = saveProjectConfig(c, a.ProjectID, ap)
	changed := ap
	changed.Fields = map[string]TrackerField{}
	for k, v := range ap.Fields {
		changed.Fields[k] = v
	}
	changed.Fields["description"] = TrackerField{"description", false, "string"}
	c.expect("PUT", "/api/projects/"+a.ProjectID+"/tracker/config", jsonText(changed), 409)
	c.expect("PATCH", "/api/milestones/"+m.ID, fmt.Sprintf(`{"project_id":%q}`, b.ID), 409)
	if e := c.s.syncTracker(context.Background(), TrackerConfig{ProjectID: a.ProjectID}); e != nil {
		t.Fatal(e)
	}
	if f.creates != 1 || f.issues["1"]["queue"] != "CORE" {
		t.Fatal("publication selection changed after reorder")
	}
	c.expect("PATCH", "/api/milestones/"+m.ID, fmt.Sprintf(`{"project_id":%q}`, b.ID), 409)
	c.expect("PATCH", "/api/milestones/"+m.ID, `{"title":"Pending edit"}`, 200)
	c.expect("DELETE", "/api/projects/"+a.ProjectID, "", 204)
	if e := c.s.syncTracker(context.Background(), TrackerConfig{}); e != nil {
		t.Fatal(e)
	}
	var ops, configs int
	c.s.db.QueryRow("SELECT count(*) FROM tracker_outbox WHERE project_id=?", a.ProjectID).Scan(&ops)
	c.s.db.QueryRow("SELECT count(*) FROM tracker_project_config WHERE project_id=?", a.ProjectID).Scan(&configs)
	if ops != 0 || configs != 0 || len(f.issues) != 1 {
		t.Fatal("project deletion left unsafe operations")
	}
	records, _ := c.s.trackerRecords()
	if records[0].State != "unlinked" || records[0].LocalID != "" {
		t.Fatal(records)
	}
}
func TestCrossProjectDependenciesOwnershipCompatibilityAndDisabledEndpoint(t *testing.T) {
	c, f, a, b, _ := projectPair(t)
	f.add("1", "Core")
	f.add("2", "Beta")
	f.issues["2"]["queue"] = "BETA"
	f.issues["2"]["shipDate"] = "2027-05-01"
	importProject(c, a.ProjectID)
	importProject(c, b.ID)
	am, bm := localByExternal(t, c.s, "1"), localByExternal(t, c.s, "2")
	d := edge(c, am, bm, "requires", 201)
	if e := c.s.syncTracker(context.Background(), TrackerConfig{ProjectID: b.ID}); e != nil {
		t.Fatal(e)
	}
	if f.linkCreates != 0 {
		t.Fatal("dependent project published prerequisite-owned edge")
	}
	if e := c.s.syncTracker(context.Background(), TrackerConfig{ProjectID: a.ProjectID}); e != nil {
		t.Fatal(e)
	}
	if f.linkCreates != 1 {
		t.Fatal("owner did not publish cross-project edge")
	}
	if e := c.s.syncTracker(context.Background(), TrackerConfig{}); e != nil {
		t.Fatal(e)
	}
	if f.linkCreates != 1 {
		t.Fatal("cross link duplicated")
	}
	bp := readProjectTestConfig(c, b.ID)
	bp.Enabled = false
	saveProjectConfig(c, b.ID, bp)
	if e := c.s.syncTracker(context.Background(), TrackerConfig{ProjectID: a.ProjectID}); e == nil || !strings.Contains(e.Error(), "paused") {
		t.Fatal(e)
	}
	g, _ := c.s.graph()
	if len(g.Dependencies) != 1 || len(f.links) != 1 {
		t.Fatal("disabled endpoint link removed")
	}
	bp.Enabled = true
	saveProjectConfig(c, b.ID, bp)
	// Simulate a migrated conflicting mapping: writes must be blocked, not reinterpreted.
	bp.Links[0].Direction = "inward"
	c.s.db.Exec("UPDATE tracker_project_config SET config=? WHERE project_id=?", jsonText(bp), b.ID)
	if e := c.s.syncTracker(context.Background(), TrackerConfig{ProjectID: a.ProjectID}); e == nil || !strings.Contains(e.Error(), "configuration conflict") {
		t.Fatal(e)
	}
	if len(f.links) != 1 {
		t.Fatal("conflicting mapping mutated remote link")
	}
	bp.Links[0].Direction = "outward"
	c.s.db.Exec("UPDATE tracker_project_config SET config=? WHERE project_id=?", jsonText(bp), b.ID)
	retryTestTracker(c)
	c.expect("DELETE", "/api/dependencies/"+d.ID, "", 204)
	if e := c.s.syncTracker(context.Background(), TrackerConfig{ProjectID: b.ID}); e != nil {
		t.Fatal(e)
	}
	if len(f.links) != 1 {
		t.Fatal("dependent removed another project's edge")
	}
	if e := c.s.syncTracker(context.Background(), TrackerConfig{ProjectID: a.ProjectID}); e != nil {
		t.Fatal(e)
	}
	if len(f.links) != 0 {
		t.Fatal("owner did not remove remote edge")
	}
}
func TestInvalidProjectDoesNotPreventHealthyProjectSync(t *testing.T) {
	c, f, a, b, bp := projectPair(t)
	f.add("1", "Core")
	f.add("2", "Beta")
	f.issues["2"]["queue"] = "BETA"
	f.issues["2"]["shipDate"] = "2027-06-01"
	importProject(c, a.ProjectID)
	importProject(c, b.ID)
	ap := readProjectTestConfig(c, a.ProjectID)
	ap.Selections = nil
	c.s.db.Exec("UPDATE tracker_project_config SET config=? WHERE project_id=?", jsonText(ap), a.ProjectID)
	f.issues["2"]["summary"] = "Healthy Beta update"
	if e := c.s.syncTracker(context.Background(), TrackerConfig{}); e == nil {
		t.Fatal("invalid project not reported")
	}
	if localByExternal(t, c.s, "2").Title != "Healthy Beta update" {
		t.Fatal("healthy project was not processed")
	}
	_ = bp
}

func TestLegacyTrackerMigrationIsTransactionalIndependentAndDurable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, e := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(migration + trackerMigration); e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"a", "b", "orphan"} {
		if _, e = db.Exec("INSERT INTO projects VALUES(?,?,?,'','#60a5fa','',0,?,?)", id, id, id, now(), now()); e != nil {
			t.Fatal(e)
		}
	}
	tx, _ := db.Begin()
	for _, pair := range [][2]string{{"ma", "a"}, {"mb", "b"}, {"mo", "orphan"}} {
		if e = insertMilestone(tx, Milestone{ID: pair[0], ProjectID: pair[1], Title: pair[0], Type: "release", Status: "planned", PlannedDate: "2027-04-01", CreatedAt: now(), UpdatedAt: now()}); e != nil {
			t.Fatal(e)
		}
	}
	tx.Commit()
	old := defaultTrackerConfig()
	old.Enabled = true
	old.OrgID = "org"
	old.Statuses = map[string]string{"open": "planned"}
	old.Selections = []TrackerSelection{{Queue: "CORE", ProjectID: "a"}, {Queue: "BETA", ProjectID: "b"}}
	db.Exec("INSERT INTO tracker_connection(id,config,last_success) VALUES('tracker',?,'legacy-success')", jsonText(old))
	for _, pair := range [][2]string{{"1", "ma"}, {"2", "mb"}, {"3", "mo"}} {
		if _, e = db.Exec("INSERT INTO tracker_records(external_id,issue_key,local_id,url,version,baseline,state,conflicts) VALUES(?,?,?,'url',5,?,'conflict',?)", pair[0], "KEY-"+pair[0], pair[1], `{"title":"baseline"}`, `{"title":{"baseline":"baseline","local":"local","remote":"remote"}}`); e != nil {
			t.Fatal(e)
		}
	}
	db.Exec("INSERT INTO tracker_records(external_id,issue_key,url,version,baseline,state) VALUES('excluded','OLD-1','url',1,'{}','unlinked')")
	db.Exec("INSERT INTO tracker_outbox(id,local_id,kind,payload,attempts,next_attempt,error) VALUES('publication-identity','mb','publish','BETA',2,12345,'retry me')")
	db.Exec("INSERT INTO tracker_outbox(id,local_id,kind,payload) VALUES('local-intent','ma','update','{}')")
	db.Exec("INSERT INTO tracker_edges VALUES('ma|mb|requires',1)")
	db.Exec("INSERT INTO tracker_retry VALUES('tracker',9999999999)")
	db.Close()
	migrated, e := OpenDB(path)
	if e != nil {
		t.Fatal(e)
	}
	s, e := New(migrated, Config{Password: "test-password-strong"})
	if e != nil {
		t.Fatal(e)
	}
	a, ok, e := s.projectTrackerConfig("a")
	if e != nil || !ok || len(a.Selections) != 1 || a.Selections[0].ID == "" {
		t.Fatal(a, e)
	}
	b, _, _ := s.projectTrackerConfig("b")
	orphan, _, _ := s.projectTrackerConfig("orphan")
	if orphan.Enabled || len(orphan.Selections) != 0 {
		t.Fatal("orphan binding guessed a selection")
	}
	var warning, selection, identity, payload string
	var attempts, next int
	migrated.QueryRow("SELECT warning FROM tracker_project_config WHERE project_id='orphan'").Scan(&warning)
	if warning == "" {
		t.Fatal("missing migration warning")
	}
	migrated.QueryRow("SELECT id,selection_id,payload,attempts,next_attempt FROM tracker_outbox WHERE kind='publish'").Scan(&identity, &selection, &payload, &attempts, &next)
	if identity != "publication-identity" || selection != b.Selections[0].ID || payload != "BETA" || attempts != 2 || next != 12345 {
		t.Fatal("publication identity or retry state changed")
	}
	records, _ := s.trackerRecords()
	if len(records) != 4 || records[0].Conflicts["title"].Local != "local" {
		t.Fatal(records)
	}
	a.Fields["planned_date"] = TrackerField{"customDate", true, "date"}
	migrated.Exec("UPDATE tracker_project_config SET config=? WHERE project_id='a'", jsonText(a))
	b, _, _ = s.projectTrackerConfig("b")
	if b.Fields["planned_date"].Field != "dueDate" {
		t.Fatal("project configurations share mutable mappings")
	}
	migrated.Close()
	again, e := OpenDB(path)
	if e != nil {
		t.Fatal(e)
	}
	defer again.Close()
	var n int
	again.QueryRow("SELECT count(*) FROM tracker_project_config").Scan(&n)
	if n != 3 {
		t.Fatal("migration was applied twice")
	}
	var raw string
	again.QueryRow("SELECT config FROM tracker_connection").Scan(&raw)
	if strings.Contains(raw, "fields") || strings.Contains(raw, "selections") {
		t.Fatal("global mappings survived migration")
	}
}

func TestGlobalConnectionCanBeSavedWithoutProjectsAndRejectsStaleWrites(t *testing.T) {
	c, _ := setup(t)
	newFakeTracker(t, c.s)
	cfg := TrackerConnectionConfig{Enabled: true, OrgID: "org", OrgHeader: "X-Org-ID", Auth: "OAuth", Interval: 300}
	c.expect("PUT", "/api/tracker/config", jsonText(cfg), 200)
	c.expect("POST", "/api/tracker/test", "{}", 200)
	c.expect("PUT", "/api/tracker/config", jsonText(cfg), 409)
	var response map[string]any
	json.Unmarshal(c.expect("GET", "/api/tracker", "", 200), &response)
	config := response["config"].(map[string]any)
	if _, exists := config["fields"]; exists {
		t.Fatal("global mappings still editable")
	}
	if _, exists := config["selections"]; exists {
		t.Fatal("global selections still editable")
	}
	if response["token_available"] != true {
		t.Fatal("missing token availability")
	}
}
func TestPublicationRecoveryCannotStealAnotherProjectsBinding(t *testing.T) {
	c, f, a, b, _ := projectPair(t)
	g, _ := c.s.graph()
	var projectA Project
	for _, p := range g.Projects {
		if p.ID == a.ProjectID {
			projectA = p
		}
	}
	m := milestone(c, projectA, "Pending publication", "2027-05-01")
	c.expect("POST", "/api/tracker/milestones/"+m.ID+"/publish", `{"selection_id":"core-selection"}`, 202)
	var identity string
	c.s.db.QueryRow("SELECT id FROM tracker_outbox WHERE local_id=?", m.ID).Scan(&identity)
	f.add("1", "Recovered remote issue")
	f.issues["1"]["unique"] = identity
	f.issues["1"]["queue"] = "BETA"
	f.issues["1"]["shipDate"] = "2027-05-01"
	importProject(c, b.ID)
	bound := localByExternal(t, c.s, "1")
	if e := c.s.syncTracker(context.Background(), TrackerConfig{ProjectID: a.ProjectID}); e == nil {
		t.Fatal("conflicting publication binding was reassigned")
	}
	if got := localByExternal(t, c.s, "1"); got.ID != bound.ID || got.ProjectID != b.ID {
		t.Fatal("stable issue binding stolen", got)
	}
	if f.creates != 0 {
		t.Fatal("recovery created another issue")
	}
}
func TestDisabledCrossEndpointRetainsPendingRemovalWithoutRetryExhaustion(t *testing.T) {
	c, f, a, b, _ := projectPair(t)
	f.add("1", "Core")
	f.add("2", "Beta")
	f.issues["2"]["queue"] = "BETA"
	f.issues["2"]["shipDate"] = "2027-04-01"
	importProject(c, a.ProjectID)
	importProject(c, b.ID)
	d := edge(c, localByExternal(t, c.s, "1"), localByExternal(t, c.s, "2"), "requires", 201)
	if e := c.s.syncTracker(context.Background(), TrackerConfig{ProjectID: a.ProjectID}); e != nil {
		t.Fatal(e)
	}
	bp := readProjectTestConfig(c, b.ID)
	bp.Enabled = false
	saveProjectConfig(c, b.ID, bp)
	c.expect("DELETE", "/api/dependencies/"+d.ID, "", 204)
	for i := 0; i < 10; i++ {
		if e := c.s.syncTracker(context.Background(), TrackerConfig{ProjectID: a.ProjectID}); e == nil {
			t.Fatal("disabled endpoint not explained")
		}
	}
	var attempts int
	if e := c.s.db.QueryRow("SELECT attempts FROM tracker_outbox WHERE project_id=? AND kind='links'", a.ProjectID).Scan(&attempts); e != nil || attempts != 0 {
		t.Fatal("paused intent lost or exhausted retries", attempts, e)
	}
	bp.Enabled = true
	saveProjectConfig(c, b.ID, bp)
	if e := c.s.syncTracker(context.Background(), TrackerConfig{ProjectID: a.ProjectID}); e != nil {
		t.Fatal(e)
	}
	if len(f.links) != 0 {
		t.Fatal("pending removal did not resume")
	}
}

func TestProjectsUseTheirOwnOutgoingStatusTargets(t *testing.T) {
	c, f, a, b, bp := projectPair(t)
	bp.Statuses["released"] = "completed"
	bp.WriteStatuses["completed"] = "released"
	saveProjectConfig(c, b.ID, bp)
	f.add("1", "Core")
	f.add("2", "Beta")
	f.issues["2"]["queue"] = "BETA"
	f.issues["2"]["shipDate"] = "2027-04-01"
	f.transitionTargets = map[string]string{"2": "released"}
	importProject(c, a.ProjectID)
	importProject(c, b.ID)
	for _, id := range []string{"1", "2"} {
		m := localByExternal(t, c.s, id)
		c.expect("PATCH", "/api/milestones/"+m.ID, `{"status":"completed","completed_date":"2027-04-02"}`, 200)
	}
	if e := c.s.syncTracker(context.Background(), TrackerConfig{}); e != nil {
		t.Fatal(e)
	}
	if f.issues["1"]["status"].(map[string]any)["key"] != "done" || f.issues["2"]["status"].(map[string]any)["key"] != "released" {
		t.Fatal("outgoing status targets leaked between projects")
	}
}

func TestCrossProjectRelatedOwnershipUsesStableExternalIdentity(t *testing.T) {
	c, f, a, b, _ := projectPair(t)
	// Deliberately make the second project's external identity lower.
	f.add("9", "Core")
	f.add("1", "Beta")
	f.issues["1"]["queue"] = "BETA"
	f.issues["1"]["shipDate"] = "2027-05-01"
	importProject(c, a.ProjectID)
	importProject(c, b.ID)
	am, bm := localByExternal(t, c.s, "9"), localByExternal(t, c.s, "1")
	d := edge(c, am, bm, "related_to", 201)
	var intentOwner string
	if e := c.s.db.QueryRow("SELECT project_id FROM tracker_outbox WHERE kind='links'").Scan(&intentOwner); e != nil || intentOwner != b.ID {
		t.Fatal("related intent belongs to the endpoint with the lower external ID", intentOwner, e)
	}
	if e := c.s.syncTracker(context.Background(), TrackerConfig{ProjectID: a.ProjectID}); e != nil {
		t.Fatal(e)
	}
	if f.linkCreates != 0 {
		t.Fatal("non-owner published related link")
	}
	if e := c.s.syncTracker(context.Background(), TrackerConfig{ProjectID: b.ID}); e != nil {
		t.Fatal(e)
	}
	if f.linkCreates != 1 || f.links["1"] != [3]string{"1", "9", "relates"} {
		t.Fatal("related publication used the wrong endpoint", f.links)
	}
	if e := c.s.syncTracker(context.Background(), TrackerConfig{}); e != nil {
		t.Fatal(e)
	}
	g, _ := c.s.graph()
	if len(g.Dependencies) != 1 || f.linkCreates != 1 {
		t.Fatal("inverse responses duplicated related link")
	}
	c.expect("DELETE", "/api/dependencies/"+d.ID, "", 204)
	if e := c.s.syncTracker(context.Background(), TrackerConfig{ProjectID: a.ProjectID}); e != nil {
		t.Fatal(e)
	}
	if len(f.links) != 1 {
		t.Fatal("non-owner removed related link")
	}
	if e := c.s.syncTracker(context.Background(), TrackerConfig{ProjectID: b.ID}); e != nil {
		t.Fatal(e)
	}
	if len(f.links) != 0 {
		t.Fatal("related removal did not converge")
	}
}
