package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

var errTrackerStale = errors.New("local Tracker context changed during request; reconciliation will retry")

type trackerRunKey struct{}
type trackerRun struct {
	epoch    uint64
	validate func() error
	requests *int
	scope    *trackerIssueScope
}
type trackerIssueScope struct {
	localID, externalID string
	saved               Milestone
	generation          int64
}

// The caller holds writeMu for local work only. Network boundaries release it,
// then validate the captured context before allowing any local result to commit.
func (s *Server) trackerContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, trackerRunKey{}, &trackerRun{epoch: s.trackerEpoch, requests: new(int)})
}
func trackerRunFrom(ctx context.Context) *trackerRun {
	v, _ := ctx.Value(trackerRunKey{}).(*trackerRun)
	return v
}
func (s *Server) trackerNetwork(ctx context.Context, call func() error) error {
	run := trackerRunFrom(ctx)
	if run == nil {
		return call()
	} // read-only helpers and isolated tests
	if e := s.validateTrackerRun(run); e != nil {
		return e
	}
	s.writeMu.Unlock()
	e := call()
	s.writeMu.Lock()
	// Retain connection-wide cooldown even when a local edit invalidated results.
	if httpCode(e) == 401 || httpCode(e) == 429 {
		s.db.Exec("INSERT INTO tracker_retry VALUES('tracker',?) ON CONFLICT(id) DO UPDATE SET next_attempt=max(next_attempt,excluded.next_attempt)", retryTime(e, time.Now().Add(time.Minute).Unix()))
	}
	if stale := s.validateTrackerRun(run); stale != nil {
		return stale
	}
	return e
}
func (s *Server) validateTrackerRun(run *trackerRun) error {
	if run.validate != nil {
		return run.validate()
	}
	if run.epoch != s.trackerEpoch {
		return errTrackerStale
	}
	return nil
}
func (s *Server) wakeTracker() {
	select {
	case s.trackerWake <- struct{}{}:
	default:
	}
}
func (s *Server) trackerMilestone(id string) (m Milestone, e error) {
	e = s.db.QueryRow("SELECT * FROM milestones WHERE id=?", id).Scan(&m.ID, &m.ProjectID, &m.Title, &m.Description, &m.Type, &m.Version, &m.PlannedDate, &m.CompletedDate, &m.Status, &m.Owner, &m.ExternalURL, &m.CreatedAt, &m.UpdatedAt, &m.Revision)
	return
}
func (s *Server) trackerRecord(id string) (x TrackerRecord, e error) {
	var baseline, conflicts string
	e = s.db.QueryRow("SELECT external_id,issue_key,local_id,url,version,baseline,state,error,conflicts,project_id FROM tracker_records WHERE local_id=? AND state!='unlinked'", id).Scan(&x.ExternalID, &x.Key, &x.LocalID, &x.URL, &x.Version, &baseline, &x.State, &x.Error, &conflicts, &x.ProjectID)
	if e == nil {
		e = json.Unmarshal([]byte(baseline), &x.Baseline)
	}
	if e == nil {
		e = json.Unmarshal([]byte(conflicts), &x.Conflicts)
	}
	return
}

// A field edit may proceed while this issue is in flight. Binding, configuration,
// conflict decisions and outbox identity must still match the captured context.
func (s *Server) issueContext(ctx context.Context, c TrackerConfig, m Milestone, x TrackerRecord) context.Context {
	parent := trackerRunFrom(ctx)
	if parent == nil {
		return ctx
	}
	run := *parent
	scope := &trackerIssueScope{localID: m.ID, externalID: x.ExternalID, saved: m}
	s.db.QueryRow("SELECT generation FROM tracker_outbox WHERE local_id=? AND kind='update'", m.ID).Scan(&scope.generation)
	run.scope = scope
	connection, _ := s.connectionConfig()
	binding := jsonText([]any{x.ExternalID, x.ProjectID, x.Baseline, x.Conflicts})
	run.validate = func() error {
		current, e := s.resolveTrackerConfig(c.ProjectID)
		conn, ce := s.connectionConfig()
		record, re := s.trackerRecord(m.ID)
		if e != nil || ce != nil || re != nil || !current.Enabled || jsonText(current) != jsonText(c) || jsonText(conn) != jsonText(connection) || binding != jsonText([]any{record.ExternalID, record.ProjectID, record.Baseline, record.Conflicts}) {
			return errTrackerStale
		}
		return nil
	}
	return context.WithValue(ctx, trackerRunKey{}, &run)
}

// Refresh after our own baseline/conflict write, retaining the original operation
// generation until confirmation. New user edits always get a larger generation.
func (s *Server) refreshIssueContext(ctx context.Context, c TrackerConfig, m Milestone, x TrackerRecord) {
	run := trackerRunFrom(ctx)
	if run == nil || run.scope == nil {
		return
	}
	next := s.issueContext(ctx, c, m, x)
	run.validate = trackerRunFrom(next).validate
	run.scope.saved = m
}

func (s *Server) processTrackerOutbox(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if !s.trackerRunMu.TryLock() {
		return
	}
	defer s.trackerRunMu.Unlock()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	ctx = s.trackerContext(ctx)
	var cooldown int64
	s.db.QueryRow("SELECT next_attempt FROM tracker_retry WHERE id='tracker'").Scan(&cooldown)
	if cooldown > time.Now().Unix() {
		return
	}
	type operation struct {
		id, local, kind, project, payload string
		generation                        int64
		attempts                          int
	}
	rows, e := s.db.Query("SELECT id,local_id,kind,project_id,payload,generation,attempts FROM tracker_outbox WHERE attempts<8 AND next_attempt<=? ORDER BY rowid", time.Now().Unix())
	if e != nil {
		return
	}
	ops := []operation{}
	for rows.Next() {
		var o operation
		if e = rows.Scan(&o.id, &o.local, &o.kind, &o.project, &o.payload, &o.generation, &o.attempts); e != nil {
			break
		}
		ops = append(ops, o)
	}
	rows.Close()
	for _, op := range ops {
		if ctx.Err() != nil {
			return
		}
		// Previous operations can have changed baselines or produced new intents.
		ctx = s.trackerContext(ctx)
		c, err := s.resolveTrackerConfig(op.project)
		if err != nil || !c.Enabled {
			continue
		}
		if err = validateTracker(c); err == nil {
			err = s.verifyOutboxContext(c)
		}
		started := time.Now()
		opctx := ctx
		if err == nil {
			switch op.kind {
			case "update":
				m, me := s.trackerMilestone(op.local)
				x, xe := s.trackerRecord(op.local)
				if me != nil || xe != nil {
					continue
				}
				if m.ProjectID != c.ProjectID || x.ProjectID != c.ProjectID || x.LocalID != m.ID {
					err = fmt.Errorf("Targeted upload paused: binding and operation project disagree; review the destination")
					break
				}
				opctx = s.issueContext(ctx, c, m, x)
				var issue map[string]any
				issue, err = s.trackerIssue(opctx, c, x.ExternalID)
				if err == nil {
					in, reason := targetedTrackerSelection(c, issue)
					if !in && reason != "" {
						x.State = "out_of_scope"
						x.Error = reason
						err = s.saveTrackerState(opctx, m, x, true)
					} else {
						err = s.reconcileIssue(opctx, c, m, x, issue, in)
					}
				} else if !errors.Is(err, errTrackerStale) {
					err = s.recordFailure(opctx, x, err)
				}
			case "publish":
				err = s.publishPending(ctx, c)
			case "links":
				err = s.outgoingTrackerLinks(ctx, c, op.id, op.payload, op.generation)
			}
		}
		run := trackerRunFrom(opctx)
		record, _ := s.trackerRecord(op.local)
		if err == nil && (record.State == "out_of_scope" || record.State == "error") {
			s.db.Exec("UPDATE tracker_outbox SET next_attempt=? WHERE id=? AND generation=? AND attempts=0", time.Now().Add(time.Duration(max(30, c.Interval))*time.Second).Unix(), op.id, op.generation)
		}
		slog.InfoContext(ctx, "Tracker outgoing operation", "kind", op.kind, "project_id", op.project, "local_id", op.local, "external_id", record.ExternalID, "conflicts", len(record.Conflicts), "attempt", op.attempts+1, "duration_ms", time.Since(started).Milliseconds(), "remote_requests", *run.requests, "retry", err != nil, "stale", errors.Is(err, errTrackerStale))
		if err != nil && !errors.Is(err, errTrackerStale) {
			s.db.Exec("UPDATE tracker_outbox SET error=?,attempts=attempts+1,next_attempt=? WHERE id=? AND generation=? AND attempts=?", err.Error(), retryTime(err, time.Now().Add(30*time.Second).Unix()), op.id, op.generation, op.attempts)
		}
		if httpCode(err) == 401 || httpCode(err) == 429 {
			return
		}
	}
}

// Queue identity is explicit in an issue response. Arbitrary query membership
// cannot be inferred from GET; never broaden a targeted upload into a search.
func targetedTrackerSelection(c TrackerConfig, issue map[string]any) (bool, string) {
	queue, _ := issue["queue"].(string)
	if object, ok := issue["queue"].(map[string]any); ok {
		queue, _ = object["key"].(string)
	}
	query := false
	for _, sel := range c.Selections {
		if sel.Query != "" {
			query = true
			continue
		}
		if queue != "" && sel.Queue == queue {
			return true, ""
		}
	}
	if query {
		return false, "Targeted upload paused: arbitrary query membership cannot be verified safely; run manual synchronization or configure an explicit queue selection"
	}
	return false, ""
}
