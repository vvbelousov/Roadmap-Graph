package server

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func restartIntegrity(t *testing.T, c *client) {
	t.Helper()
	var seq int
	var name, path string
	if e := c.s.db.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); e != nil {
		t.Fatal(e)
	}
	if e := c.s.db.Close(); e != nil {
		t.Fatal(e)
	}
	db, e := OpenDB(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	c.s.db = db // retain authenticated test client; all durable state is read from the reopened database.
}
func TestIntegrityCancelledPublicationRecovery(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	g, _ := c.s.graph()
	m := milestone(c, g.Projects[0], "Publish once", "2027-03-01")
	path := "/api/tracker/milestones/" + m.ID
	c.expect("POST", path+"/publish", `{"selection_id":"core-selection"}`, 202)
	f.loseCreate = true
	if e := c.s.syncTracker(context.Background(), cfg); e == nil {
		t.Fatal("expected lost response")
	}
	var original string
	c.s.db.QueryRow("SELECT id FROM tracker_outbox WHERE local_id=?", m.ID).Scan(&original)
	c.expect("POST", path+"/unlink", `{}`, 200)
	var active int
	c.s.db.QueryRow("SELECT count(*) FROM tracker_outbox WHERE local_id=?", m.ID).Scan(&active)
	if active != 0 {
		t.Fatal("cancel left executable operation")
	}
	restartIntegrity(t, c)
	c.expect("POST", path+"/publish", `{"selection_id":"core-selection"}`, 409)
	c.expect("POST", path+"/recover", `{}`, 200)
	c.expect("POST", path+"/publish", `{"selection_id":"core-selection","retain":true}`, 202)
	var restored string
	c.s.db.QueryRow("SELECT id FROM tracker_outbox WHERE local_id=?", m.ID).Scan(&restored)
	if original != restored {
		t.Fatal("publication identity changed")
	}
	syncOK(t, c.s, cfg)
	if f.creates != 1 {
		t.Fatalf("created %d issues", f.creates)
	}
}
func TestIntegrityPartialPublicationMappingRestart(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	delete(cfg.Statuses, "open")
	delete(cfg.WriteStatuses, "planned")
	saveTestTracker(c, cfg, 200)
	g, _ := c.s.graph()
	m := milestone(c, g.Projects[0], "Completed publish", "2027-03-01")
	c.expect("PATCH", "/api/milestones/"+m.ID, `{"status":"completed","completed_date":"2027-03-01"}`, 200)
	c.expect("POST", "/api/tracker/milestones/"+m.ID+"/publish", `{"selection_id":"core-selection"}`, 202)
	if e := c.s.syncTracker(context.Background(), cfg); e == nil {
		t.Fatal("expected mapping failure")
	}
	var state, id, key, msg string
	var attempts int
	if e := c.s.db.QueryRow("SELECT state,external_id,issue_key FROM tracker_publications WHERE local_id=?", m.ID).Scan(&state, &id, &key); e != nil {
		t.Fatal(e)
	}
	c.s.db.QueryRow("SELECT attempts,error FROM tracker_outbox WHERE local_id=?", m.ID).Scan(&attempts, &msg)
	if state != "created_needs_mapping" || id == "" || key == "" || attempts != 1 || msg == "" {
		t.Fatal(state, id, key, attempts, msg)
	}
	other := milestone(c, g.Projects[0], "Unrelated pending publication", "2027-03-01")
	c.expect("PATCH", "/api/milestones/"+other.ID, `{"status":"completed","completed_date":"2027-03-01"}`, 200)
	c.expect("POST", "/api/tracker/milestones/"+other.ID+"/publish", `{"selection_id":"core-selection"}`, 202)
	restartIntegrity(t, c)
	cfg.Statuses["open"] = "planned"
	saveTestTracker(c, cfg, 409)
	c.expect("POST", "/api/tracker/milestones/"+other.ID+"/unlink", `{}`, 200)
	saveTestTracker(c, cfg, 200)
	syncOK(t, c.s, cfg)
	if f.creates != 1 {
		t.Fatalf("created %d issues", f.creates)
	}
	got := localByExternal(t, c.s, id)
	if got.Status != "completed" {
		t.Fatal(got)
	}
}
func TestIntegrityOrganizationPendingDefense(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	g, _ := c.s.graph()
	m := milestone(c, g.Projects[0], "Queued", "2027-03-01")
	c.expect("POST", "/api/tracker/milestones/"+m.ID+"/publish", `{"selection_id":"core-selection"}`, 202)
	connection, _ := c.s.connectionConfig()
	connection.OrgID = "other"
	c.expect("PUT", "/api/tracker/config", jsonText(connection), 409)
	// Even a bypass of the configuration API cannot retarget persisted work.
	c.s.db.Exec("UPDATE tracker_outbox SET organization='X-Org-ID:other'")
	if e := c.s.publishPending(context.Background(), cfg); e == nil {
		t.Fatal("worker accepted organization mismatch")
	}
	c.s.db.Exec("UPDATE tracker_outbox SET organization='X-Org-ID:org'")
	c.s.db.Exec("UPDATE tracker_publications SET organization='X-Org-ID:other'")
	if e := c.s.publishPending(context.Background(), cfg); e == nil {
		t.Fatal("worker accepted publication journal identity mismatch")
	}
	if f.creates != 0 || f.pages != 0 {
		t.Fatal("worker contacted wrong organization")
	}
}
func TestIntegrityStaleEntityEdits(t *testing.T) {
	c, _ := setup(t)
	p := project(c)
	m := milestone(c, p, "Original", "2027-03-01")
	path := "/api/milestones/" + m.ID
	c.expect("PATCH", path, fmt.Sprintf(`{"revision":%d,"planned_date":"2027-04-01"}`, m.Revision), 200)
	code, _ := c.call("PATCH", path, fmt.Sprintf(`{"revision":%d,"title":"Stale","planned_date":"2027-03-01"}`, m.Revision))
	if code != 409 {
		t.Fatal(code)
	}
	var current Milestone
	json.Unmarshal(c.expect("GET", path, "", 200), &current)
	if current.PlannedDate != "2027-04-01" || current.Title != "Original" || current.Revision != m.Revision+1 {
		t.Fatal(current)
	}
	code, _ = c.call("PATCH", path, `{"title":"No revision"}`)
	if code != 409 {
		t.Fatal(code)
	}
	c.expect("PATCH", "/api/projects/"+p.ID, fmt.Sprintf(`{"revision":%d,"name":"New"}`, p.Revision), 200)
	code, _ = c.call("PATCH", "/api/projects/"+p.ID, fmt.Sprintf(`{"revision":%d,"description":"Stale"}`, p.Revision))
	if code != 409 {
		t.Fatal(code)
	}
	var count int
	c.s.db.QueryRow("SELECT count(*) FROM tracker_outbox").Scan(&count)
	if count != 0 {
		t.Fatal("rejected write queued outgoing intent")
	}
}
func TestIntegrityTrackerChangesAdvanceRevision(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	f.add("1", "Remote")
	importTracker(t, c)
	old := localByExternal(t, c.s, "1")
	f.mu.Lock()
	f.issues["1"]["dueDate"] = "2027-04-01"
	f.issues["1"]["version"] = float64(2)
	f.mu.Unlock()
	syncOK(t, c.s, cfg)
	current := localByExternal(t, c.s, "1")
	if current.Revision != old.Revision+1 {
		t.Fatal(old.Revision, current.Revision)
	}
	code, _ := c.call("PATCH", "/api/milestones/"+old.ID, fmt.Sprintf(`{"revision":%d,"title":"Stale"}`, old.Revision))
	if code != 409 {
		t.Fatal(code)
	}
	syncOK(t, c.s, cfg)
	after := localByExternal(t, c.s, "1")
	if after.Revision != current.Revision {
		t.Fatal("bookkeeping advanced revision")
	}
	var count int
	c.s.db.QueryRow("SELECT count(*) FROM tracker_outbox").Scan(&count)
	if count != 0 {
		t.Fatal("stale edit queued an operation")
	}
}

func TestIntegrityMigrationPreservesIntegrationState(t *testing.T) {
	c, f, _ := trackerSetup(t)
	f.add("existing", "Linked before upgrade")
	importTracker(t, c)
	g, _ := c.s.graph()
	m := milestone(c, g.Projects[0], "Pending migration", "2027-03-01")
	c.expect("POST", "/api/tracker/milestones/"+m.ID+"/publish", `{"selection_id":"core-selection"}`, 202)
	// Produce a populated v3 database by removing only the additions of migration 4.
	tx, e := c.s.db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	statements := []string{
		"UPDATE tracker_outbox SET attempts=3,next_attempt=123,error='retained retry'",
		"DROP TRIGGER projects_revision", "DROP TRIGGER milestones_revision",
		"DROP TABLE tracker_publications", "ALTER TABLE tracker_outbox DROP COLUMN organization",
		"ALTER TABLE milestones DROP COLUMN revision", "ALTER TABLE projects DROP COLUMN revision",
		"DELETE FROM schema_migrations WHERE version=4",
	}
	for _, q := range statements {
		if _, e = tx.Exec(q); e != nil {
			tx.Rollback()
			t.Fatal(e)
		}
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	restartIntegrity(t, c)
	var attempts int
	var due int64
	var msg, unique, organization, state string
	if e = c.s.db.QueryRow("SELECT id,attempts,next_attempt,error,organization FROM tracker_outbox WHERE local_id=?", m.ID).Scan(&unique, &attempts, &due, &msg, &organization); e != nil {
		t.Fatal(e)
	}
	if attempts != 3 || due != 123 || msg != "retained retry" || organization != "X-Org-ID:org" {
		t.Fatal(attempts, due, msg, organization)
	}
	if e = c.s.db.QueryRow("SELECT state FROM tracker_publications WHERE id=?", unique).Scan(&state); e != nil || state != "outcome_unknown" {
		t.Fatal(state, e)
	}
	restartIntegrity(t, c)
	var count int
	c.s.db.QueryRow("SELECT count(*) FROM tracker_publications WHERE id=?", unique).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	g, e = c.s.graph()
	if e != nil || len(g.Milestones) != 2 {
		t.Fatal(g, e)
	}
	linked := localByExternal(t, c.s, "existing")
	if linked.Title != "Linked before upgrade" || linked.Revision != 1 {
		t.Fatal(linked)
	}
	records, e := c.s.trackerRecords()
	if e != nil || len(records) != 1 || records[0].Baseline["title"] != "Linked before upgrade" {
		t.Fatal(records, e)
	}
}
func TestIntegrityDeletionPreservesPublicationRecovery(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	g, _ := c.s.graph()
	m := milestone(c, g.Projects[0], "Deleted after lost response", "2027-03-01")
	c.expect("POST", "/api/tracker/milestones/"+m.ID+"/publish", `{"selection_id":"core-selection"}`, 202)
	f.loseCreate = true
	if e := c.s.syncTracker(context.Background(), cfg); e == nil {
		t.Fatal("expected lost response")
	}
	c.expect("DELETE", "/api/projects/"+m.ProjectID, "", 204)
	restartIntegrity(t, c)
	c.expect("POST", "/api/tracker/milestones/"+m.ID+"/recover", `{}`, 200)
	var external, state string
	c.s.db.QueryRow("SELECT external_id,state FROM tracker_publications WHERE local_id=?", m.ID).Scan(&external, &state)
	if external == "" || state != "recovered_requires_decision" || f.creates != 1 {
		t.Fatal(external, state, f.creates)
	}
}
