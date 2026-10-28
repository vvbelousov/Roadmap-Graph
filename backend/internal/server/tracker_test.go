package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeTracker struct {
	failPatch                            int
	loseLink                             bool
	mu                                   sync.Mutex
	issues                               map[string]map[string]any
	links                                map[string][3]string // link ID -> prerequisite external ID, dependent external ID, type
	denied                               map[string]int
	excluded                             map[string]bool
	pages, patches, creates, linkCreates int
	failSearch                           []int
	loseCreate                           bool
	patchRace                            bool
}

func newFakeTracker(t *testing.T, s *Server) *fakeTracker {
	t.Helper()
	f := &fakeTracker{issues: map[string]map[string]any{}, links: map[string][3]string{}, denied: map[string]int{}, excluded: map[string]bool{}}
	remote := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(remote.Close)
	s.tracker.origin = remote.URL
	s.tracker.pause = 0
	t.Setenv("ROADMAP_TRACKER_TOKEN", "secret-test-token")
	return f
}
func (f *fakeTracker) add(id, title string) {
	f.issues[id] = map[string]any{"id": id, "key": "CORE-" + id, "version": float64(1), "summary": title, "description": "description", "dueDate": "2027-02-01", "doneDate": nil, "status": map[string]any{"key": "open"}, "queue": "CORE", "assignee": map[string]any{"id": "person-1"}, "fixVersions": []any{map[string]any{"id": "v1"}}}
}
func (f *fakeTracker) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "OAuth secret-test-token" || r.Header.Get("X-Org-ID") != "org" {
		w.WriteHeader(401)
		return
	}
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	send := func(v any) { w.Header().Set("Content-Type", "application/json"); json.NewEncoder(w).Encode(v) }
	if r.URL.Path == "/v3/myself" {
		send(map[string]any{"id": "robot"})
		return
	}
	if r.URL.Path == "/v3/issues/_search" {
		f.pages++
		if len(f.failSearch) > 0 {
			code := f.failSearch[0]
			f.failSearch = f.failSearch[1:]
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(code)
			return
		}
		filter, _ := body["filter"].(map[string]any)
		ids := []string{}
		for id, v := range f.issues {
			if filter["unique"] != nil {
				if v["unique"] != filter["unique"] {
					continue
				}
			} else {
				if f.excluded[id] {
					continue
				}
				if filter["queue"] != nil && filter["queue"] != v["queue"] {
					continue
				}
			}
			ids = append(ids, id)
		}
		sort.Strings(ids)
		w.Header().Set("X-Total-Pages", fmt.Sprint(max(1, len(ids))))
		w.Header().Set("X-Total-Count", fmt.Sprint(len(ids)))
		page := parseTestInt(r.URL.Query().Get("page"))
		out := []any{}
		if page <= len(ids) {
			out = append(out, f.issues[ids[page-1]])
		}
		send(out)
		return
	}
	if r.URL.Path == "/v3/issues" && r.Method == "POST" {
		for _, v := range f.issues {
			if v["unique"] == body["unique"] {
				w.WriteHeader(409)
				return
			}
		}
		id := fmt.Sprint(len(f.issues) + 1)
		f.add(id, body["summary"].(string))
		for k, v := range body {
			f.issues[id][k] = v
		}
		f.creates++
		if f.loseCreate {
			f.loseCreate = false
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		w.WriteHeader(201)
		send(f.issues[id])
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		w.WriteHeader(404)
		return
	}
	id := parts[2]
	if code := f.denied[id]; code > 0 {
		w.WriteHeader(code)
		return
	}
	issue := f.issues[id]
	if issue == nil {
		w.WriteHeader(404)
		return
	}
	if len(parts) == 3 {
		if r.Method == "PATCH" {
			if f.failPatch != 0 {
				w.Header().Set("Retry-After", "3600")
				w.WriteHeader(f.failPatch)
				f.failPatch = 0
				return
			}
			if f.patchRace {
				f.patchRace = false
				issue["summary"] = "concurrent Tracker title"
				issue["version"] = issue["version"].(float64) + 1
			}
			if r.URL.Query().Get("version") != fmt.Sprint(issue["version"]) {
				w.WriteHeader(409)
				return
			}
			for k, v := range body {
				issue[k] = v
			}
			issue["version"] = issue["version"].(float64) + 1
			f.patches++
		}
		send(issue)
		return
	}
	if parts[3] == "transitions" {
		if len(parts) == 4 {
			send([]any{map[string]any{"id": "complete", "to": map[string]any{"key": "done"}}})
			return
		}
		issue["status"] = map[string]any{"key": "done"}
		issue["version"] = issue["version"].(float64) + 1
		send([]any{})
		return
	}
	if parts[3] == "links" {
		if len(parts) == 5 && parts[4] == "_list" {
			out := []any{}
			for key, l := range f.links {
				other, direction := "", "outward"
				if l[0] == id {
					other = l[1]
				} else if l[1] == id {
					other = l[0]
					direction = "inward"
				}
				if other != "" {
					out = append(out, map[string]any{"id": parseTestInt(key), "type": map[string]any{"id": l[2]}, "direction": direction, "object": map[string]any{"id": other, "key": "CORE-" + other}})
				}
			}
			send(map[string]any{"links": out})
			return
		}
		if r.Method == "DELETE" {
			delete(f.links, parts[4])
			w.WriteHeader(204)
			return
		}
		if r.Method == "POST" {
			typ := "depends"
			if body["relationship"] == "relates" {
				typ = "relates"
			}
			other := body["issue"].(string)
			f.links[fmt.Sprint(len(f.links)+1)] = [3]string{id, other, typ}
			f.linkCreates++
			if f.loseLink {
				f.loseLink = false
				conn, _, _ := w.(http.Hijacker).Hijack()
				conn.Close()
				return
			}
			w.WriteHeader(201)
			send(map[string]any{})
			return
		}
	}
	w.WriteHeader(404)
}
func parseTestInt(s string) int { n := 1; fmt.Sscan(s, &n); return n }
func trackerSetup(t *testing.T) (*client, *fakeTracker, TrackerConfig) {
	c, _ := setup(t)
	p := project(c)
	cfg := defaultTrackerConfig()
	cfg.Enabled = true
	cfg.OrgID = "org"
	cfg.Selections = []TrackerSelection{{Queue: "CORE", ProjectID: p.ID}}
	cfg.Fields["status"] = TrackerField{"status", true, "status"}
	cfg.Fields["completed_date"] = TrackerField{"doneDate", true, "date"}
	cfg.Fields["owner"] = TrackerField{"assignee", true, "id"}
	cfg.Fields["version"] = TrackerField{"fixVersions", true, "single_id"}
	cfg.Statuses = map[string]string{"open": "planned", "done": "completed", "rejected": "cancelled"}
	cfg.WriteStatuses = map[string]string{"planned": "open", "completed": "done", "cancelled": "rejected"}
	cfg.Links = []TrackerLinkMapping{{"depends", "outward", "requires", "is dependent by"}, {"relates", "outward", "related_to", "relates"}}
	c.expect("PUT", "/api/tracker/config", jsonText(cfg), 200)
	f := newFakeTracker(t, c.s)
	return c, f, cfg
}
func importTracker(t *testing.T, c *client) TrackerPreview {
	t.Helper()
	var p TrackerPreview
	json.Unmarshal(c.expect("POST", "/api/tracker/preview", "{}", 200), &p)
	c.expect("POST", "/api/tracker/import", jsonText(map[string]string{"preview_id": p.ID}), 200)
	return p
}
func syncOK(t *testing.T, s *Server, cfg TrackerConfig) {
	t.Helper()
	if e := s.syncTracker(context.Background(), cfg); e != nil {
		t.Fatal(e)
	}
}
func localByExternal(t *testing.T, s *Server, id string) Milestone {
	t.Helper()
	records, e := s.trackerRecords()
	if e != nil {
		t.Fatal(e)
	}
	g, e := s.graph()
	if e != nil {
		t.Fatal(e)
	}
	for _, r := range records {
		if r.ExternalID == id {
			for _, m := range g.Milestones {
				if m.ID == r.LocalID {
					return m
				}
			}
		}
	}
	t.Fatal("missing linked milestone")
	return Milestone{}
}
func TestTrackerImportPaginationMappingsAndIdentity(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	f.add("1", "Core")
	f.add("2", "ADX")
	f.add("3", "Undated")
	f.issues["3"]["dueDate"] = nil
	f.add("4", "Unmapped")
	f.issues["4"]["status"] = map[string]any{"key": "unknown"}
	f.links["1"] = [3]string{"1", "2", "depends"}
	f.links["2"] = [3]string{"1", "outside", "depends"}
	p := importTracker(t, c)
	if len(p.Items) != 2 || len(p.Skipped) != 2 || len(p.Links) != 1 || len(p.Warnings) == 0 {
		t.Fatalf("preview: %+v", p)
	}
	a, b := localByExternal(t, c.s, "1"), localByExternal(t, c.s, "2")
	g, _ := c.s.graph()
	if len(g.Dependencies) != 1 || g.Dependencies[0].PrerequisiteID != a.ID || g.Dependencies[0].DependentID != b.ID {
		t.Fatal(g.Dependencies)
	}
	if a.Owner != "person-1" || a.Version != "v1" || a.PlannedDate != "2027-02-01" {
		t.Fatal(a)
	}
	again := importTracker(t, c)
	if len(again.Items) != 0 || len(again.Links) != 0 {
		t.Fatal("repeated import changed data")
	}
	f.issues["1"]["key"] = "MOVED-99"
	syncOK(t, c.s, cfg)
	if localByExternal(t, c.s, "1").ID != a.ID {
		t.Fatal("key changed identity")
	}
}
func TestTrackerBidirectionalMergeConflictResolutionAndEcho(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	f.add("1", "Core")
	importTracker(t, c)
	m := localByExternal(t, c.s, "1")
	c.expect("PATCH", "/api/milestones/"+m.ID, `{"title":"Roadmap title"}`, 200)
	f.issues["1"]["description"] = "Tracker description"
	f.issues["1"]["version"] = float64(2)
	syncOK(t, c.s, cfg)
	m = localByExternal(t, c.s, "1")
	if m.Title != "Roadmap title" || m.Description != "Tracker description" || f.issues["1"]["summary"] != "Roadmap title" {
		t.Fatal(m)
	}
	patches := f.patches
	syncOK(t, c.s, cfg)
	if f.patches != patches {
		t.Fatal("echo")
	}
	c.expect("PATCH", "/api/milestones/"+m.ID, `{"title":"Local choice"}`, 200)
	f.issues["1"]["summary"] = "Remote choice"
	f.issues["1"]["version"] = float64(4)
	if c.s.syncTracker(context.Background(), cfg) == nil {
		t.Fatal("expected attention")
	}
	records, _ := c.s.trackerRecords()
	if records[0].State != "conflict" || records[0].Conflicts["title"].Remote != "Remote choice" {
		t.Fatal(records)
	}
	if f.patches != patches {
		t.Fatal("overwrote conflict")
	}
	c.expect("POST", "/api/tracker/milestones/"+m.ID+"/resolve", `{"choices":{"title":"local"}}`, 200)
	syncOK(t, c.s, cfg)
	if f.issues["1"]["summary"] != "Local choice" {
		t.Fatal("resolution not uploaded")
	}
	c.expect("PATCH", "/api/milestones/"+m.ID, `{"status":"completed","completed_date":"2027-02-02"}`, 200)
	syncOK(t, c.s, cfg)
	if f.issues["1"]["status"].(map[string]any)["key"] != "done" {
		t.Fatal("status not transitioned")
	}
}
func TestTrackerPublicationNetworkRecoveryAndOutboxRestart(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	g, _ := c.s.graph()
	m := milestone(c, g.Projects[0], "Publish", "2027-03-01")
	f.loseCreate = true
	c.expect("POST", "/api/tracker/milestones/"+m.ID+"/publish", `{"selection":0}`, 202)
	if c.s.syncTracker(context.Background(), cfg) == nil {
		t.Fatal("expected lost response")
	}
	f.mu.Lock()
	creates := f.creates
	f.mu.Unlock()
	if creates != 1 {
		t.Fatal(creates)
	}
	// New Server instance on the same persisted SQLite state simulates process recovery.
	var dbPath string
	var seq int
	var dbName string
	if err := c.s.db.QueryRow("PRAGMA database_list").Scan(&seq, &dbName, &dbPath); err != nil {
		t.Fatal(err)
	}
	if err := c.s.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, e := OpenDB(dbPath)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { reopened.Close() })
	restarted, e := New(reopened, Config{Password: "test-password-strong"})
	if e != nil {
		t.Fatal(e)
	}
	restarted.tracker = c.s.tracker
	c.s = restarted
	c.s.db.Exec("UPDATE tracker_outbox SET next_attempt=0")
	syncOK(t, c.s, cfg)
	f.mu.Lock()
	creates = f.creates
	f.mu.Unlock()
	if creates != 1 {
		t.Fatal("duplicate issue")
	}
	m = localByExternal(t, c.s, "1")
	if m.Title != "Publish" {
		t.Fatal(m)
	}
	var n int
	c.s.db.QueryRow("SELECT count(*) FROM tracker_outbox").Scan(&n)
	if n != 0 {
		t.Fatal("outbox not drained")
	}
}
func TestTrackerLinksBothDirectionsAndCycle(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	f.add("1", "Core")
	f.add("2", "ADX")
	f.add("3", "More")
	importTracker(t, c)
	a, b, d := localByExternal(t, c.s, "1"), localByExternal(t, c.s, "2"), localByExternal(t, c.s, "3")
	ab := edge(c, a, b, "requires", 201)
	syncOK(t, c.s, cfg)
	if f.linkCreates != 1 {
		t.Fatal("link not uploaded")
	}
	syncOK(t, c.s, cfg)
	if f.linkCreates != 1 {
		t.Fatal("duplicate link")
	}
	c.expect("DELETE", "/api/dependencies/"+ab.ID, "", 204)
	syncOK(t, c.s, cfg)
	if len(f.links) != 0 {
		t.Fatal("link not removed")
	}
	f.links["1"] = [3]string{"1", "2", "depends"}
	f.links["2"] = [3]string{"2", "3", "depends"}
	syncOK(t, c.s, cfg)
	g, _ := c.s.graph()
	if len(g.Dependencies) != 2 {
		t.Fatal(g)
	}
	f.links["3"] = [3]string{"3", "1", "depends"}
	if e := c.s.syncTracker(context.Background(), cfg); e == nil || !strings.Contains(e.Error(), "cycle") {
		t.Fatal(e)
	}
	g, _ = c.s.graph()
	if len(g.Dependencies) != 2 {
		t.Fatal("partial invalid incoming links")
	}
	delete(f.links, "3")
	delete(f.links, "1")
	syncOK(t, c.s, cfg)
	g, _ = c.s.graph()
	if len(g.Dependencies) != 1 || g.Dependencies[0].PrerequisiteID != b.ID || g.Dependencies[0].DependentID != d.ID {
		t.Fatal(g.Dependencies)
	}
}
func TestTrackerScopeAccessDeletionAndUnlink(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	f.add("1", "Core")
	importTracker(t, c)
	m := localByExternal(t, c.s, "1")
	f.excluded["1"] = true
	syncOK(t, c.s, cfg)
	records, _ := c.s.trackerRecords()
	if records[0].State != "out_of_scope" {
		t.Fatal(records)
	}
	f.denied["1"] = 403
	c.s.syncTracker(context.Background(), cfg)
	records, _ = c.s.trackerRecords()
	if records[0].State != "lost_access" {
		t.Fatal(records)
	}
	f.denied["1"] = 404
	c.s.syncTracker(context.Background(), cfg)
	records, _ = c.s.trackerRecords()
	if records[0].State != "missing_or_inaccessible" {
		t.Fatal(records)
	}
	f.denied["1"] = 410
	c.s.syncTracker(context.Background(), cfg)
	records, _ = c.s.trackerRecords()
	if records[0].State != "deleted" {
		t.Fatal(records)
	}
	g, _ := c.s.graph()
	if len(g.Milestones) != 1 {
		t.Fatal("data removed")
	}
	c.expect("POST", "/api/tracker/milestones/"+m.ID+"/unlink", "{}", 200)
	delete(f.denied, "1")
	delete(f.excluded, "1")
	if p := importTracker(t, c); len(p.Items) != 0 {
		t.Fatal("excluded issue reimported")
	}
	c.expect("DELETE", "/api/milestones/"+m.ID, "", 204)
	if len(f.issues) != 1 {
		t.Fatal("Tracker issue deleted")
	}
	importTracker(t, c)
	g, _ = c.s.graph()
	if len(g.Milestones) != 0 {
		t.Fatal("deleted milestone reimported")
	}
}
func TestTrackerRetriesConcurrencySecurityAndWhatIf(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	f.add("1", "Core")
	f.failSearch = []int{429, 503}
	importTracker(t, c)
	if f.pages < 3 {
		t.Fatal("not retried")
	}
	m := localByExternal(t, c.s, "1")
	patches := f.patches
	c.expect("GET", "/api/milestones/"+m.ID+"/impact?planned_date=2027-09-01", "", 200)
	var n int
	c.s.db.QueryRow("SELECT count(*) FROM tracker_outbox").Scan(&n)
	if n != 0 || f.patches != patches {
		t.Fatal("simulation has side effects")
	}
	c.expect("PATCH", "/api/milestones/"+m.ID, `{"title":"Local"}`, 200)
	f.patchRace = true
	c.s.syncTracker(context.Background(), cfg)
	if f.issues["1"]["summary"] != "concurrent Tracker title" {
		t.Fatal("version check missing")
	}
	c.s.db.Exec("UPDATE tracker_outbox SET next_attempt=0")
	c.s.syncTracker(context.Background(), cfg)
	records, _ := c.s.trackerRecords()
	if records[0].State != "conflict" {
		t.Fatal(records)
	}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	response := c.expect("GET", "/api/tracker", "", 200)
	if strings.Contains(string(response), "secret-test-token") || strings.Contains(logs.String(), "secret-test-token") {
		t.Fatal("token leaked")
	}
	_, err := c.s.tracker.request(context.Background(), cfg, "POST", "//evil.invalid/v3/issues/_search", nil, nil)
	if err == nil {
		t.Fatal("unsafe URL accepted")
	}
	_, err = c.s.tracker.request(context.Background(), cfg, "GET", "https://evil.invalid/v3/myself", nil, nil)
	if err == nil {
		t.Fatal("unsafe origin accepted")
	}
	c.s.db.Exec("UPDATE tracker_connection SET lease='other',lease_until=?", time.Now().Add(time.Minute).Unix())
	if c.s.syncTracker(context.Background(), cfg) == nil {
		t.Fatal("concurrent lease accepted")
	}
}

func TestTrackerTransactionalOutboxAndStalePreview(t *testing.T) {
	c, f, _ := trackerSetup(t)
	f.add("1", "Core")
	var p TrackerPreview
	json.Unmarshal(c.expect("POST", "/api/tracker/preview", "{}", 200), &p)
	g, _ := c.s.graph()
	milestone(c, g.Projects[0], "Local", "2027-03-01")
	c.expect("POST", "/api/tracker/import", jsonText(map[string]string{"preview_id": p.ID}), 409)
	importTracker(t, c)
	m := localByExternal(t, c.s, "1")
	if _, e := c.s.db.Exec("CREATE TRIGGER reject_outbox BEFORE INSERT ON tracker_outbox BEGIN SELECT RAISE(ABORT,'outbox unavailable'); END"); e != nil {
		t.Fatal(e)
	}
	c.expect("PATCH", "/api/milestones/"+m.ID, `{"title":"must roll back"}`, 409)
	if got := localByExternal(t, c.s, "1"); got.Title != "Core" {
		t.Fatal("edit committed without outbox", got)
	}
	c.s.db.Exec("DROP TRIGGER reject_outbox")
	cfg, _ := c.s.trackerConfig()
	cfg.Enabled = false
	c.expect("PUT", "/api/tracker/config", jsonText(cfg), 200)
	c.expect("POST", "/api/tracker/sync", "{}", 409)
	c.expect("POST", "/api/tracker/preview", "{}", 409)
	cfg.Enabled = true
	cfg.Fields["description"] = TrackerField{"description", false, "string"}
	c.expect("PUT", "/api/tracker/config", jsonText(cfg), 200) // write permission changes preserve field identity
	c.expect("PATCH", "/api/milestones/"+m.ID, `{"description":"readonly edit"}`, 409)
	cfg.Fields["description"] = TrackerField{"anotherField", true, "string"}
	c.expect("PUT", "/api/tracker/config", jsonText(cfg), 409) // cannot silently reinterpret active baselines
}
func TestTrackerSafePaginationRedirectAndSanitizedErrors(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	_ = f
	var destinationRequests int
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationRequests++; w.WriteHeader(200) }))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/myself":
			w.Header().Set("Location", destination.URL+"/v3/stolen")
			w.WriteHeader(302)
		case "/v3/issues/_search":
			w.Header().Set("Link", "<"+destination.URL+"/v3/issues/_search>; rel=\"next\"")
			fmt.Fprint(w, `[]`)
		default:
			w.WriteHeader(403)
			fmt.Fprint(w, "secret-test-token")
		}
	}))
	defer source.Close()
	c.s.tracker.origin = source.URL
	c.expect("POST", "/api/tracker/test", "{}", 502)
	if destinationRequests != 0 {
		t.Fatal("redirect followed")
	}
	if _, e := c.s.tracker.search(context.Background(), cfg, map[string]any{"filter": map[string]string{"queue": "CORE"}}); e == nil {
		t.Fatal("unsafe pagination followed")
	}
	if destinationRequests != 0 {
		t.Fatal("credentials forwarded")
	}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	_, e := c.s.tracker.issue(context.Background(), cfg, "anything")
	if e == nil || strings.Contains(e.Error(), "secret-test-token") || strings.Contains(logs.String(), "secret-test-token") {
		t.Fatal("remote error leaked token")
	}
}
func TestTrackerPendingRetryLimitAndRetryAfter(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	f.add("1", "Core")
	importTracker(t, c)
	m := localByExternal(t, c.s, "1")
	c.expect("PATCH", "/api/milestones/"+m.ID, `{"title":"Local"}`, 200)
	c.s.db.Exec("UPDATE tracker_outbox SET attempts=8")
	if c.s.syncTracker(context.Background(), cfg) == nil {
		t.Fatal("retry exhaustion hidden")
	}
	if f.patches != 0 {
		t.Fatal("retry limit ignored")
	}
	records, _ := c.s.trackerRecords()
	if records[0].State != "error" {
		t.Fatal(records)
	}
	c.expect("POST", "/api/tracker/retry", "{}", 200)
	syncOK(t, c.s, cfg)
	if f.patches != 1 {
		t.Fatal("manual retry failed")
	}
	until := time.Now().Add(time.Hour).Unix()
	if got := retryTime(trackerHTTPError{Code: 429, RetryAt: until}, time.Now().Unix()); got != until {
		t.Fatal(got)
	}
}
func TestTrackerPreviewCycleIsAtomic(t *testing.T) {
	c, f, _ := trackerSetup(t)
	f.add("1", "Core")
	f.add("2", "ADX")
	f.links["1"] = [3]string{"1", "2", "depends"}
	f.links["2"] = [3]string{"2", "1", "depends"}
	c.expect("POST", "/api/tracker/preview", "{}", 422)
	g, _ := c.s.graph()
	if len(g.Milestones) != 0 || len(g.Dependencies) != 0 {
		t.Fatal("preview persisted data")
	}
}

func TestTrackerRemovedMappingsStayLocalAndCooldownPersists(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	delete(cfg.Fields, "description")
	delete(cfg.Fields, "status")
	cfg.Statuses = map[string]string{}
	cfg.WriteStatuses = map[string]string{}
	c.expect("PUT", "/api/tracker/config", jsonText(cfg), 200)
	loaded, e := c.s.trackerConfig()
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := loaded.Fields["description"]; ok {
		t.Fatal("removed mapping restored")
	}
	f.add("1", "Core")
	importTracker(t, c)
	m := localByExternal(t, c.s, "1")
	c.expect("PATCH", "/api/milestones/"+m.ID, `{"description":"local only"}`, 200)
	syncOK(t, c.s, cfg)
	if f.issues["1"]["description"] != "description" || localByExternal(t, c.s, "1").Description != "local only" {
		t.Fatal("unmapped field transmitted")
	}
	c.s.db.Exec("INSERT INTO tracker_retry VALUES('tracker',?)", time.Now().Add(time.Hour).Unix())
	pages := f.pages
	if c.s.syncTracker(context.Background(), cfg) == nil {
		t.Fatal("cooldown ignored")
	}
	if f.pages != pages {
		t.Fatal("request made during cooldown")
	}
	c.expect("POST", "/api/tracker/retry", "{}", 200)
	syncOK(t, c.s, cfg)
}

func TestTrackerMutationThrottleAndLostLinkResponse(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	f.add("1", "Core")
	f.add("2", "ADX")
	importTracker(t, c)
	a, b := localByExternal(t, c.s, "1"), localByExternal(t, c.s, "2")
	c.expect("PATCH", "/api/milestones/"+a.ID, `{"title":"Local"}`, 200)
	f.failPatch = 429
	if e := c.s.syncTracker(context.Background(), cfg); httpCode(e) != 429 {
		t.Fatal("missing throttle", e)
	}
	var due int64
	c.s.db.QueryRow("SELECT next_attempt FROM tracker_outbox WHERE local_id=?", a.ID).Scan(&due)
	if due < time.Now().Add(59*time.Minute).Unix() {
		t.Fatal("Retry-After not persisted")
	}
	pages := f.pages
	if c.s.syncTracker(context.Background(), cfg) == nil || f.pages != pages {
		t.Fatal("connection did not respect throttle")
	}
	c.expect("POST", "/api/tracker/retry", "{}", 200)
	syncOK(t, c.s, cfg)
	f.loseLink = true
	edge(c, a, b, "requires", 201)
	if c.s.syncTracker(context.Background(), cfg) == nil {
		t.Fatal("lost link response not reported")
	}
	c.expect("POST", "/api/tracker/retry", "{}", 200)
	syncOK(t, c.s, cfg)
	f.mu.Lock()
	created := f.linkCreates
	f.mu.Unlock()
	if created != 1 {
		t.Fatal("duplicate link after lost response", created)
	}
}
