package server

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func (f *fakeTracker) barrier(t *testing.T, method, path string) (<-chan struct{}, chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blockMethod = method
	f.blockPath = path
	f.entered = make(chan struct{})
	f.release = make(chan struct{})
	release := f.release
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	return f.entered, f.release
}
func awaitTracker(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("Tracker barrier timed out")
	}
}
func outgoing(t *testing.T, s *Server) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); s.processTrackerOutbox(context.Background()) }()
	return done
}
func editTrackerTitle(c *client, id, title string) {
	c.expect("PATCH", "/api/milestones/"+id, jsonText(map[string]string{"title": title}), 200)
}
func (f *fakeTracker) resetRequests() { f.mu.Lock(); defer f.mu.Unlock(); f.requests = nil }
func TestTargetedOutboxRequestScope(t *testing.T) {
	c, f, _ := trackerSetup(t)
	for i := 1; i <= 120; i++ {
		f.add(fmt.Sprint(i), fmt.Sprint("issue ", i))
	}
	importTracker(t, c)
	m := localByExternal(t, c.s, "1")
	f.resetRequests()
	editTrackerTitle(c, m.ID, "local A")
	f.mu.Lock()
	n := len(f.requests)
	f.mu.Unlock()
	if n != 0 {
		t.Fatalf("local save made %d remote requests", n)
	}
	c.s.processTrackerOutbox(context.Background())
	f.mu.Lock()
	defer f.mu.Unlock()
	want := []string{"GET /v3/issues/1", "PATCH /v3/issues/1", "GET /v3/issues/1"}
	if jsonText(f.requests) != jsonText(want) {
		t.Fatalf("requests: %v", f.requests)
	}
	t.Logf("fake fixture: 120 issues, outgoing update: %d requests, 0 searches, 0 link fetches", len(f.requests))
}
func TestOutgoingPreservesSecondEditAndDoesNotBlockCRUD(t *testing.T) {
	c, f, _ := trackerSetup(t)
	f.add("1", "base")
	f.add("2", "other")
	importTracker(t, c)
	m := localByExternal(t, c.s, "1")
	other := localByExternal(t, c.s, "2")
	editTrackerTitle(c, m.ID, "A")
	entered, release := f.barrier(t, "PATCH", "/v3/issues/1")
	done := outgoing(t, c.s)
	awaitTracker(t, entered)
	// Both writes complete while the remote PATCH remains blocked.
	editTrackerTitle(c, other.ID, "unrelated")
	editTrackerTitle(c, m.ID, "B")
	close(release)
	awaitTracker(t, done)
	current := localByExternal(t, c.s, "1")
	if current.Title != "B" {
		t.Fatalf("new edit overwritten: %+v", current)
	}
	var generation int
	if e := c.s.db.QueryRow("SELECT generation FROM tracker_outbox WHERE local_id=? AND kind='update'", m.ID).Scan(&generation); e != nil || generation < 2 {
		t.Fatalf("second generation lost: %d %v", generation, e)
	}
	x, e := c.s.trackerRecord(m.ID)
	if e != nil || x.Baseline["title"] != "A" || len(x.Conflicts) != 0 {
		t.Fatalf("bad confirmation: %+v %v", x, e)
	}
	c.s.processTrackerOutbox(context.Background())
	f.mu.Lock()
	title := f.issues["1"]["summary"]
	f.mu.Unlock()
	if title != "B" {
		t.Fatalf("second edit did not reach Tracker: %v", title)
	}
	if x, _ = c.s.trackerRecord(m.ID); x.State != "synchronized" {
		t.Fatalf("state: %+v", x)
	}
}
func TestOutgoingWakesAndRecoversPending(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(fmt.Sprint("restart=", restart), func(t *testing.T) {
			c, f, _ := trackerSetup(t)
			f.add("1", "base")
			importTracker(t, c)
			m := localByExternal(t, c.s, "1")
			if restart {
				editTrackerTitle(c, m.ID, "pending before start")
				restarted, e := New(c.s.db, Config{Password: "test-password-strong"})
				if e != nil {
					t.Fatal(e)
				}
				restarted.tracker = c.s.tracker
				c.s = restarted
				for len(c.s.trackerWake) > 0 {
					<-c.s.trackerWake
				}
			}
			entered, release := f.barrier(t, "PATCH", "/v3/issues/1")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stopped := c.s.StartTracker(ctx)
			if !restart {
				editTrackerTitle(c, m.ID, "wake now")
			}
			awaitTracker(t, entered)
			close(release)
			// Wait for the worker to finish without racing database cleanup.
			cancel()
			awaitTracker(t, stopped)
		})
	}
}
func TestOutgoingInvalidatedContext(t *testing.T) {
	for _, action := range []string{"delete", "unlink", "pause", "config"} {
		t.Run(action, func(t *testing.T) {
			c, f, cfg := trackerSetup(t)
			f.add("1", "base")
			importTracker(t, c)
			m := localByExternal(t, c.s, "1")
			editTrackerTitle(c, m.ID, "A")
			entered, release := f.barrier(t, "GET", "/v3/issues/1")
			done := outgoing(t, c.s)
			awaitTracker(t, entered)
			switch action {
			case "delete":
				c.expect("DELETE", "/api/milestones/"+m.ID, "", 204)
			case "unlink":
				c.expect("POST", "/api/tracker/milestones/"+m.ID+"/unlink", "{}", 200)
			case "pause":
				cfg.Enabled = false
				saveTestTracker(c, cfg, 200)
			case "config":
				cfg.Interval = 600
				saveTestTracker(c, cfg, 200)
			}
			close(release)
			awaitTracker(t, done)
			f.mu.Lock()
			patches := f.patches
			f.mu.Unlock()
			if patches != 0 {
				t.Fatalf("invalid context uploaded: %d", patches)
			}
			if action == "delete" {
				var n int
				c.s.db.QueryRow("SELECT count(*) FROM milestones WHERE id=?", m.ID).Scan(&n)
				if n != 0 {
					t.Fatal("deleted milestone recreated")
				}
			}
		})
	}
}
func TestIncomingAndOutgoingCoordinate(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	f.add("1", "base")
	importTracker(t, c)
	m := localByExternal(t, c.s, "1")
	entered, release := f.barrier(t, "POST", "/v3/issues/_search")
	done := make(chan struct{})
	go func() { defer close(done); c.s.syncTracker(context.Background(), cfg) }()
	awaitTracker(t, entered)
	editTrackerTitle(c, m.ID, "new local")
	// A concurrent outgoing invocation coalesces with the active incoming run.
	c.s.processTrackerOutbox(context.Background())
	close(release)
	awaitTracker(t, done)
	c.s.processTrackerOutbox(context.Background())
	x, _ := c.s.trackerRecord(m.ID)
	if x.State != "synchronized" || x.Baseline["title"] != "new local" || len(x.Conflicts) > 0 {
		t.Fatalf("reconciliation lost edit: %+v", x)
	}
}
func TestTargetedSelectionSafety(t *testing.T) {
	c, f, cfg := trackerSetup(t)
	f.add("1", "base")
	importTracker(t, c)
	m := localByExternal(t, c.s, "1")
	cfg.Selections[0].Query = "Queue: CORE"
	cfg.Selections[0].Queue = ""
	saveTestTracker(c, cfg, 200)
	editTrackerTitle(c, m.ID, "query change")
	f.resetRequests()
	c.s.processTrackerOutbox(context.Background())
	x, _ := c.s.trackerRecord(m.ID)
	if x.State != "out_of_scope" || !strings.Contains(x.Error, "manual synchronization") {
		t.Fatalf("query not paused: %+v", x)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.patches != 0 || len(f.requests) != 1 || strings.Contains(jsonText(f.requests), "_search") {
		t.Fatalf("unsafe query requests: %v", f.requests)
	}
}

func TestTargetedDependencyScopeAndLostResponse(t *testing.T) {
	c, f, _ := trackerSetup(t)
	for i := 1; i <= 40; i++ {
		f.add(fmt.Sprint(i), fmt.Sprint("issue ", i))
	}
	importTracker(t, c)
	a, b := localByExternal(t, c.s, "1"), localByExternal(t, c.s, "2")
	d := edge(c, a, b, "requires", 201)
	f.resetRequests()
	f.loseLink = true
	c.s.processTrackerOutbox(context.Background())
	// A lost POST response is recovered by listing just the affected endpoints.
	retryTestTracker(c)
	c.s.processTrackerOutbox(context.Background())
	f.mu.Lock()
	for _, request := range f.requests {
		if request != "GET /v3/issues/1" && request != "GET /v3/issues/2" && !strings.HasPrefix(request, "POST /v3/issues/1/links") && request != "POST /v3/issues/2/links/_list" {
			t.Errorf("unrelated dependency request: %s", request)
		}
	}
	if f.linkCreates != 1 {
		t.Errorf("link duplicated: %d", f.linkCreates)
	}
	t.Logf("fake fixture: 40 issues, lost-response recovery: %d requests, only endpoints 1 and 2", len(f.requests))
	f.mu.Unlock()
	c.expect("DELETE", "/api/dependencies/"+d.ID, "", 204)
	c.s.processTrackerOutbox(context.Background())
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.links) != 0 {
		t.Fatalf("link removal not uploaded: %v", f.links)
	}
}
func TestTargetedPublicationRecovery(t *testing.T) {
	c, f, _ := trackerSetup(t)
	g, _ := c.s.graph()
	m := milestone(c, g.Projects[0], "publish", "2027-03-01")
	f.loseCreate = true
	c.expect("POST", "/api/tracker/milestones/"+m.ID+"/publish", `{"selection_id":"core-selection"}`, 202)
	c.s.processTrackerOutbox(context.Background())
	retryTestTracker(c)
	c.s.processTrackerOutbox(context.Background())
	c.s.processTrackerOutbox(context.Background())
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.creates != 1 {
		t.Fatalf("duplicate publication: %d", f.creates)
	}
	var n int
	c.s.db.QueryRow("SELECT count(*) FROM tracker_outbox").Scan(&n)
	if n != 0 {
		t.Fatalf("publication not acknowledged: %d", n)
	}
}
func TestTargetedHTTPFailuresAndRetry(t *testing.T) {
	for _, code := range []int{401, 403, 409, 429, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			c, f, _ := trackerSetup(t)
			f.add("1", "base")
			importTracker(t, c)
			m := localByExternal(t, c.s, "1")
			editTrackerTitle(c, m.ID, "local")
			if code == 401 || code == 403 {
				f.denied["1"] = code
			} else {
				f.failPatch = code
			}
			c.s.processTrackerOutbox(context.Background())
			var attempts int
			var due int64
			if e := c.s.db.QueryRow("SELECT attempts,next_attempt FROM tracker_outbox WHERE local_id=?", m.ID).Scan(&attempts, &due); e != nil || attempts != 1 || due <= time.Now().Unix() {
				t.Fatalf("retry not durable: attempts=%d due=%d err=%v", attempts, due, e)
			}
			f.mu.Lock()
			before := len(f.requests)
			delete(f.denied, "1")
			f.mu.Unlock()
			c.s.processTrackerOutbox(context.Background())
			f.mu.Lock()
			after := len(f.requests)
			f.mu.Unlock()
			if before != after {
				t.Fatal("backoff ignored")
			}
			retryTestTracker(c)
			c.s.processTrackerOutbox(context.Background())
			x, _ := c.s.trackerRecord(m.ID)
			if x.State != "synchronized" {
				t.Fatalf("retry failed: %+v", x)
			}
		})
	}
}
func TestTargetedConflictsAndQueueBoundary(t *testing.T) {
	for _, remoteChange := range []string{"title", "queue"} {
		t.Run(remoteChange, func(t *testing.T) {
			c, f, _ := trackerSetup(t)
			f.add("1", "base")
			importTracker(t, c)
			m := localByExternal(t, c.s, "1")
			editTrackerTitle(c, m.ID, "local")
			if remoteChange == "title" {
				f.issues["1"]["summary"] = "remote"
			} else {
				f.issues["1"]["queue"] = map[string]any{"key": "OTHER"}
			}
			c.s.processTrackerOutbox(context.Background())
			x, _ := c.s.trackerRecord(m.ID)
			want := "conflict"
			if remoteChange == "queue" {
				want = "out_of_scope"
			}
			if x.State != want {
				t.Fatalf("state=%s want=%s", x.State, want)
			}
			f.mu.Lock()
			patches := f.patches
			f.mu.Unlock()
			if patches != 0 {
				t.Fatal("unsafe upload")
			}
			if remoteChange == "title" {
				c.expect("POST", "/api/tracker/milestones/"+m.ID+"/resolve", `{"choices":{"title":"local"}}`, 200)
				c.s.processTrackerOutbox(context.Background())
				x, _ = c.s.trackerRecord(m.ID)
				if x.State != "synchronized" {
					t.Fatalf("resolution not uploaded: %+v", x)
				}
			}
		})
	}
}
func TestEditDuringInitialFetchRecomputesMerge(t *testing.T) {
	c, f, _ := trackerSetup(t)
	f.add("1", "base")
	importTracker(t, c)
	m := localByExternal(t, c.s, "1")
	editTrackerTitle(c, m.ID, "A")
	entered, release := f.barrier(t, "GET", "/v3/issues/1")
	done := outgoing(t, c.s)
	awaitTracker(t, entered)
	editTrackerTitle(c, m.ID, "B")
	f.mu.Lock()
	f.issues["1"]["summary"] = "remote"
	f.mu.Unlock()
	close(release)
	awaitTracker(t, done)
	x, _ := c.s.trackerRecord(m.ID)
	current := localByExternal(t, c.s, "1")
	if current.Title != "B" || x.State != "conflict" || x.Conflicts["title"].Local != "B" {
		t.Fatalf("merge used stale local data: %+v %+v", current, x)
	}
}

func TestManualSynchronizationCoalesces(t *testing.T) {
	c, _, _ := trackerSetup(t)
	for i := 0; i < 10; i++ {
		c.expect("POST", projectTrackerPath(c, "sync"), "{}", 202)
	}
	if len(c.s.trackerManual) != 1 {
		t.Fatalf("redundant manual runs: %d", len(c.s.trackerManual))
	}
}
