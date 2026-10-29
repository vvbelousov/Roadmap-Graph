package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTrackerRejectsIssueKeyQueueWithoutNetwork(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	cfg.Selections[0].Queue = "AP-270"
	body := c.expect("PUT", "/api/tracker/config", jsonText(cfg), 422)
	for _, hint := range []string{"issue key AP-270", "queue AP", "Key: AP-270"} {
		if !strings.Contains(string(body), hint) {
			t.Fatalf("missing actionable hint %q: %s", hint, body)
		}
	}
	// Validate previously saved settings too, before even publication recovery can send HTTP.
	if _, e := c.s.db.Exec("UPDATE tracker_connection SET config=?", jsonText(cfg)); e != nil {
		t.Fatal(e)
	}
	if _, e := c.s.makePreview(context.Background(), cfg); e == nil {
		t.Fatal("legacy invalid selection accepted by preview")
	}
	if e := c.s.syncTracker(context.Background(), cfg); e == nil {
		t.Fatal("legacy invalid selection accepted by sync")
	}
	if f.pages != 0 {
		t.Fatal("invalid queue sent to Tracker")
	}
	response := c.expect("GET", "/api/tracker", "", 200)
	if !strings.Contains(string(response), "Key: AP-270") {
		t.Fatalf("error not persisted: %s", response)
	}
	cfg.Selections[0].Queue = "AP"
	if e := validateTracker(cfg); e != nil {
		t.Fatal(e)
	}
	cfg.Selections[0].Queue = ""
	cfg.Selections[0].Query = "Key: AP-270"
	if e := validateTracker(cfg); e != nil {
		t.Fatal(e)
	}
}

func TestTrackerHTTPValidationDiagnosticsDoNotExposeResponseData(t *testing.T) {
	c, _, cfg := trackerSetup(t)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(422)
		fmt.Fprint(w, `{"errors":{"queue":"private issue content; secret-test-token","Authorization":"OAuth secret-test-token","secret-test-token":"secret"},"errorMessages":["secret-test-token"],"description":"private issue content"}`)
	}))
	defer remote.Close()
	c.s.tracker.origin = remote.URL
	err := c.s.syncTracker(context.Background(), cfg)
	if httpCode(err) != 422 {
		t.Fatal(err)
	}
	if got := err.Error(); got != "Tracker HTTP 422 (POST /v3/issues/_search; queue)" {
		t.Fatalf("unexpected diagnostic: %s", got)
	}
	response := string(c.expect("GET", "/api/tracker", "", 200))
	for _, sensitive := range []string{"secret-test-token", "private issue content", "OAuth secret", "Authorization"} {
		if strings.Contains(response, sensitive) {
			t.Fatalf("diagnostic leaked %q", sensitive)
		}
	}
	_, err = c.s.tracker.request(context.Background(), cfg, "PATCH", "/v3/issues/issue-id?version=2&private=secret-test-token", map[string]string{"description": "private issue content"}, nil)
	if err == nil || strings.Contains(err.Error(), "?") || strings.Contains(err.Error(), "secret-test-token") || strings.Contains(err.Error(), "private issue content") {
		t.Fatalf("unsafe request diagnostic: %v", err)
	}
}

func TestTrackerPreviewDiscoversStatusesAndReportsBothImportBlockers(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	cfg.Statuses = map[string]string{}
	cfg.WriteStatuses = map[string]string{}
	cfg.Fields["planned_date"] = TrackerField{"duedatecustomer", true, "date"}
	c.expect("PUT", "/api/tracker/config", jsonText(cfg), 200)
	f.add("1", "First Epic")
	f.add("2", "Second Epic")
	f.add("3", "Third Epic")
	f.issues["1"]["status"] = map[string]any{"key": "open", "display": "Открыт"}
	f.issues["2"]["status"] = map[string]any{"key": "open", "display": "Открыт"}
	f.issues["3"]["status"] = map[string]any{"key": "inProgress", "display": "В работе"}
	var p TrackerPreview
	if e := json.Unmarshal(c.expect("POST", "/api/tracker/preview", "{}", 200), &p); e != nil {
		t.Fatal(e)
	}
	if len(p.Items) != 0 || len(p.Skipped) != 3 || len(p.UnmappedStatuses) != 2 {
		t.Fatalf("unexpected preview: %+v", p)
	}
	found := map[string]TrackerUnmappedStatus{}
	for _, status := range p.UnmappedStatuses {
		found[status.Key] = status
	}
	if found["open"].Name != "Открыт" || found["open"].Count != 2 || found["inProgress"].Count != 1 {
		t.Fatal(found)
	}
	for _, skipped := range p.Skipped {
		if len(skipped.Reasons) != 2 || !strings.Contains(skipped.Reason, "unmapped Tracker status:") || !strings.Contains(skipped.Reason, "duedatecustomer") {
			t.Fatal(skipped)
		}
	}
	// Explicit user mappings and the configured custom date field make both epics importable.
	cfg.Statuses = map[string]string{"open": "planned", "inProgress": "in_progress"}
	c.expect("PUT", "/api/tracker/config", jsonText(cfg), 200)
	for _, issue := range f.issues {
		issue["duedatecustomer"] = "2027-04-12"
	}
	p = importTracker(t, c)
	if len(p.Items) != 3 || len(p.Skipped) != 0 || len(p.UnmappedStatuses) != 0 {
		t.Fatalf("mapping repair failed: %+v", p)
	}
	if localByExternal(t, c.s, "3").Status != "in_progress" || localByExternal(t, c.s, "1").PlannedDate != "2027-04-12" {
		t.Fatal("custom date or status not applied")
	}
}

func TestTrackerCanCorrectStatusKeysAfterImportWithoutUnlinking(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	cfg.Statuses["In Progress"] = "in_progress"
	cfg.WriteStatuses["in_progress"] = "In Progress"
	c.expect("PUT", "/api/tracker/config", jsonText(cfg), 200)
	f.add("1", "Imported milestone")
	importTracker(t, c)
	m := localByExternal(t, c.s, "1")
	delete(cfg.Statuses, "In Progress")
	cfg.Statuses["inProgress"] = "in_progress"
	cfg.WriteStatuses["in_progress"] = "inProgress"
	c.expect("PUT", "/api/tracker/config", jsonText(cfg), 200)
	loaded, e := c.s.trackerConfig()
	if e != nil {
		t.Fatal(e)
	}
	if _, old := loaded.Statuses["In Progress"]; old || loaded.Statuses["inProgress"] != "in_progress" || loaded.WriteStatuses["in_progress"] != "inProgress" {
		t.Fatal(loaded)
	}
	records, _ := c.s.trackerRecords()
	if len(records) != 1 || records[0].State == "unlinked" || records[0].LocalID != m.ID {
		t.Fatal("mapping correction unlinked the milestone")
	}
	// A mapping meaning change and an independent local status edit remain a conflict.
	c.expect("PATCH", "/api/milestones/"+m.ID, `{"status":"in_progress"}`, 200)
	cfg.Statuses["open"] = "delayed"
	delete(cfg.WriteStatuses, "planned")
	cfg.WriteStatuses["delayed"] = "open"
	c.expect("PUT", "/api/tracker/config", jsonText(cfg), 200)
	if c.s.syncTracker(context.Background(), cfg) == nil {
		t.Fatal("status mapping conflict not reported")
	}
	records, _ = c.s.trackerRecords()
	conflict := records[0].Conflicts["status"]
	if records[0].State != "conflict" || conflict.Local != "in_progress" || conflict.Remote != "delayed" || f.patches != 0 {
		t.Fatalf("mapping change overwrote disputed data: %+v", records)
	}
}
