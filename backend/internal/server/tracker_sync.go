package server

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"net/url"
	"time"
)

// mergeTracker performs a three-way merge without advancing disputed baselines.
func mergeTracker(local, remote, base map[string]string) (map[string]string, map[string]string, map[string]TrackerConflict) {
	merged := map[string]string{}
	next := map[string]string{}
	conflicts := map[string]TrackerConflict{}
	for k, r := range remote {
		l, b := local[k], base[k]
		switch {
		case l == r:
			merged[k] = l
			next[k] = r
		case l == b:
			merged[k] = r
			next[k] = r
		case r == b:
			merged[k] = l
			next[k] = b
		default:
			merged[k] = l
			next[k] = b
			conflicts[k] = TrackerConflict{b, l, r}
		}
	}
	return merged, next, conflicts
}
func (s *Server) saveTrackerState(m Milestone, x TrackerRecord, pending bool) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = updateMilestone(tx, m); e != nil {
		return e
	}
	if e = saveRecord(tx, x); e != nil {
		return e
	}
	if pending {
		e = enqueue(tx, m.ID, "update", "{}")
	} else {
		_, e = tx.Exec("DELETE FROM tracker_outbox WHERE local_id=? AND kind='update'", m.ID)
	}
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Server) recordFailure(x TrackerRecord, e error) error {
	x.State = "error"
	x.Error = e.Error()
	switch httpCode(e) {
	case 403:
		x.State = "lost_access"
	case 404:
		x.State = "missing_or_inaccessible"
	case 410:
		x.State = "deleted"
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = saveRecord(tx, x); err != nil {
		return err
	}
	var attempts int
	tx.QueryRow("SELECT attempts FROM tracker_outbox WHERE local_id=? AND kind='update'", x.LocalID).Scan(&attempts)
	delay := time.Duration(1<<min(attempts, 10))*time.Second + time.Duration(rand.IntN(1000))*time.Millisecond
	_, err = tx.Exec("UPDATE tracker_outbox SET attempts=attempts+1,next_attempt=?,error=? WHERE local_id=? AND kind='update'", retryTime(e, time.Now().Add(delay).Unix()), e.Error(), x.LocalID)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if httpCode(e) == 429 {
		return e
	}
	return nil
}
func (s *Server) reconcileIssue(ctx context.Context, c TrackerConfig, m Milestone, x TrackerRecord, issue map[string]any, inSelection bool) error {
	remote, e := remoteFields(issue, c)
	if e != nil {
		return s.recordFailure(x, e)
	}
	id, key, ver, e := issueIdentity(issue)
	if e != nil {
		return s.recordFailure(x, e)
	}
	if id != x.ExternalID {
		return s.recordFailure(x, fmt.Errorf("stable issue ID changed"))
	}
	merged, base, conflicts := mergeTracker(localFields(m), remote, x.Baseline)
	setFields(&m, merged)
	if e = validateMilestone(m); e != nil {
		return s.recordFailure(x, e)
	}
	x.Key = key
	x.URL = trackerURL(key)
	m.ExternalURL = x.URL
	x.Version = ver
	x.Baseline = base
	x.Conflicts = conflicts
	x.Error = ""
	x.State = "synchronized"
	pending := false
	patch := map[string]any{}
	status := ""
	for k, v := range merged {
		if _, bad := conflicts[k]; bad || v == remote[k] {
			continue
		}
		f := c.Fields[k]
		pending = true
		if !f.Writable {
			x.Error = "local change to read-only field: " + k
			continue
		}
		if k == "status" {
			status = c.WriteStatuses[v]
			if status == "" {
				x.Error = "no write status mapping: " + v
			}
		} else {
			patch[f.Field] = remoteValue(v, f)
		}
	}
	if len(conflicts) > 0 {
		x.State = "conflict"
	} else if pending {
		x.State = "pending_upload"
	}
	if !inSelection {
		x.State = "out_of_scope"
		x.Error = "issue no longer matches selections; uploads paused"
	}
	if x.Error != "" && inSelection {
		x.State = "error"
	}
	// Persist incoming merges and conflicts before any network mutation.
	if e = s.saveTrackerState(m, x, pending); e != nil {
		return e
	}
	if len(conflicts) > 0 || !inSelection || x.Error != "" {
		return nil
	}
	var attempts int
	var due int64
	s.db.QueryRow("SELECT attempts,next_attempt FROM tracker_outbox WHERE local_id=? AND kind='update'", m.ID).Scan(&attempts, &due)
	if pending && (attempts >= 8 || due > time.Now().Unix()) {
		x.State = "error"
		x.Error = "outgoing operation waiting for retry"
		if attempts >= 8 {
			x.Error = "outgoing retry limit reached; reset failed operations"
		}
		return s.saveTrackerState(m, x, true)
	}
	if len(patch) > 0 {
		_, e = s.tracker.request(ctx, c, "PATCH", fmt.Sprintf("%s?version=%d", issuePath(id), ver), patch, nil)
		if e != nil {
			return s.recordFailure(x, e)
		}
	}
	if status != "" {
		fresh, e := s.tracker.issue(ctx, c, id)
		if e != nil {
			return s.recordFailure(x, e)
		}
		f, e := remoteFields(fresh, c)
		if e != nil {
			return s.recordFailure(x, e)
		}
		if f["status"] != remote["status"] && f["status"] != merged["status"] {
			return s.recordFailure(x, fmt.Errorf("status changed before transition; reconcile again"))
		}
		if f["status"] != merged["status"] {
			var transitions []struct {
				ID string `json:"id"`
				To struct {
					Key string `json:"key"`
				} `json:"to"`
			}
			_, e = s.tracker.request(ctx, c, "GET", issuePath(id)+"/transitions", nil, &transitions)
			if e != nil {
				return s.recordFailure(x, e)
			}
			transition := ""
			for _, t := range transitions {
				if t.To.Key == status {
					if transition != "" {
						return s.recordFailure(x, fmt.Errorf("ambiguous status transition"))
					}
					transition = t.ID
				}
			}
			if transition == "" {
				return s.recordFailure(x, fmt.Errorf("status transition unavailable or requires workflow configuration"))
			}
			_, e = s.tracker.request(ctx, c, "POST", issuePath(id)+"/transitions/"+url.PathEscape(transition)+"/_execute", map[string]any{}, nil)
			if e != nil {
				return s.recordFailure(x, e)
			}
		}
	}
	if !pending {
		return nil
	}
	// Read back; baseline only acknowledges values actually observed remotely.
	fresh, e := s.tracker.issue(ctx, c, id)
	if e != nil {
		return s.recordFailure(x, e)
	}
	after, e := remoteFields(fresh, c)
	if e != nil {
		return s.recordFailure(x, e)
	}
	_, x.Key, x.Version, e = issueIdentity(fresh)
	if e != nil {
		return s.recordFailure(x, e)
	}
	stillPending := false
	for k, v := range after {
		if v == merged[k] {
			x.Baseline[k] = v
		} else {
			stillPending = true
		}
	}
	x.State = "synchronized"
	if stillPending {
		x.State = "pending_upload"
	}
	return s.saveTrackerState(m, x, stillPending)
}
func (s *Server) syncTracker(ctx context.Context, c TrackerConfig) (err error) {
	if !c.Enabled {
		return fmt.Errorf("Tracker integration disabled")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	var retryAfter int64
	if e := s.db.QueryRow("SELECT next_attempt FROM tracker_retry WHERE id='tracker'").Scan(&retryAfter); e != nil && e != sql.ErrNoRows {
		return e
	}
	if retryAfter > time.Now().Unix() {
		return fmt.Errorf("Tracker requested delayed retry")
	}
	owner := token()
	res, e := s.db.Exec("UPDATE tracker_connection SET lease=?,lease_until=? WHERE id='tracker' AND lease_until<?", owner, time.Now().Add(12*time.Minute).Unix(), time.Now().Unix())
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return fmt.Errorf("synchronization already running")
	}
	defer func() {
		msg := ""
		if next := retryTime(err, 0); next > 0 {
			if _, er := s.db.Exec("INSERT INTO tracker_retry VALUES('tracker',?) ON CONFLICT(id) DO UPDATE SET next_attempt=excluded.next_attempt", next); er != nil && err == nil {
				err = er
			}
		}
		if err != nil {
			msg = err.Error()
		}
		if err == nil {
			_, e = s.db.Exec("UPDATE tracker_connection SET last_success=?,last_error='',lease='',lease_until=0 WHERE lease=?", now(), owner)
		} else {
			_, e = s.db.Exec("UPDATE tracker_connection SET last_error=?,lease='',lease_until=0 WHERE lease=?", msg, owner)
		}
		if err == nil {
			err = e
		}
	}()
	for _, sel := range c.Selections {
		if e = validateTrackerSelection(sel); e != nil {
			return e
		}
	}
	if e = s.publishPending(ctx, c); e != nil {
		return e
	}
	selected := map[string]map[string]any{}
	for _, sel := range c.Selections {
		body := map[string]any{"filter": map[string]string{"queue": sel.Queue}}
		if sel.Query != "" {
			body = map[string]any{"query": sel.Query}
		}
		batch, e := s.tracker.search(ctx, c, body)
		if e != nil {
			return e
		}
		for _, v := range batch {
			id, _, _, e := issueIdentity(v)
			if e != nil {
				return e
			}
			selected[id] = v
		}
	}
	records, e := s.trackerRecords()
	if e != nil {
		return e
	}
	g, e := s.graph()
	if e != nil {
		return e
	}
	ms := map[string]Milestone{}
	for _, m := range g.Milestones {
		ms[m.ID] = m
	}
	locals := map[string]string{}
	anyError := false
	for _, x := range records {
		if x.State == "unlinked" || x.LocalID == "" {
			continue
		}
		v, in := selected[x.ExternalID]
		if !in {
			v, e = s.tracker.issue(ctx, c, x.ExternalID)
			if e != nil {
				if er := s.recordFailure(x, e); er != nil {
					return er
				}
				anyError = true
				continue
			}
		}
		if e = s.reconcileIssue(ctx, c, ms[x.LocalID], x, v, in); e != nil {
			return e
		}
		var state string
		s.db.QueryRow("SELECT state FROM tracker_records WHERE external_id=?", x.ExternalID).Scan(&state)
		if state == "error" || state == "conflict" || state == "lost_access" {
			anyError = true
		}
		if in && state == "synchronized" {
			locals[x.ExternalID] = x.LocalID
		}
	}
	if e = s.reconcileEdges(ctx, c, locals); e != nil {
		if e == errTrackerRetryWaiting {
			return e
		}
		_, dbErr := s.db.Exec("UPDATE tracker_outbox SET attempts=attempts+1,next_attempt=?,error=? WHERE kind='links'", retryTime(e, time.Now().Add(30*time.Second+time.Duration(rand.IntN(1000))*time.Millisecond).Unix()), e.Error())
		if dbErr != nil {
			return dbErr
		}
		return e
	}
	// Link operations are convergent; acknowledge only after a successful reconciliation.
	for _, id := range locals {
		if _, e = s.db.Exec("DELETE FROM tracker_outbox WHERE kind='links' AND local_id=?", id); e != nil {
			return e
		}
	}
	var queued int
	if e = s.db.QueryRow("SELECT count(*) FROM tracker_outbox").Scan(&queued); e != nil {
		return e
	}
	if queued > 0 {
		anyError = true
	}
	if anyError {
		return fmt.Errorf("some issues need attention; inspect synchronization records")
	}
	return nil
}
func (s *Server) publishPending(ctx context.Context, c TrackerConfig) error {
	type operation struct {
		ID, LocalID, Payload string
		Attempts             int
		Due                  int64
	}
	ops := []operation{}
	rows, e := s.db.Query("SELECT id,local_id,payload,attempts,next_attempt FROM tracker_outbox WHERE kind='publish'")
	if e != nil {
		return e
	}
	for rows.Next() {
		var o operation
		if e = rows.Scan(&o.ID, &o.LocalID, &o.Payload, &o.Attempts, &o.Due); e != nil {
			rows.Close()
			return e
		}
		ops = append(ops, o)
	}
	rows.Close()
	g, e := s.graph()
	if e != nil {
		return e
	}
	for _, op := range ops {
		if op.Attempts >= 8 || op.Due > time.Now().Unix() {
			continue
		}
		var m Milestone
		for _, v := range g.Milestones {
			if v.ID == op.LocalID {
				m = v
			}
		}
		// Search by the persisted unique key before (re)creation; a lost response cannot duplicate issues.
		found, e := s.tracker.search(ctx, c, map[string]any{"filter": map[string]string{"unique": op.ID}})
		if e != nil {
			return e
		}
		var issue map[string]any
		if len(found) > 1 {
			return fmt.Errorf("duplicate publication identity")
		}
		if len(found) == 1 {
			issue = found[0]
		} else {
			body := map[string]any{"queue": op.Payload, "summary": m.Title, "unique": op.ID}
			for k, f := range c.Fields {
				if k != "status" && f.Writable {
					body[f.Field] = remoteValue(localFields(m)[k], f)
				}
			}
			_, e = s.tracker.request(ctx, c, "POST", "/v3/issues", body, &issue)
		}
		if e != nil {
			delay := time.Duration(1<<min(op.Attempts, 10)) * time.Second
			_, dbErr := s.db.Exec("UPDATE tracker_outbox SET attempts=attempts+1,next_attempt=?,error=? WHERE id=?", retryTime(e, time.Now().Add(delay).Unix()), e.Error(), op.ID)
			if dbErr != nil {
				return dbErr
			}
			return e
		}
		id, key, version, e := issueIdentity(issue)
		if e != nil {
			return e
		}
		f, e := remoteFields(issue, c)
		if e != nil {
			return e
		}
		tx, e := s.db.Begin()
		if e != nil {
			return e
		}
		x := TrackerRecord{ExternalID: id, Key: key, LocalID: m.ID, URL: trackerURL(key), Version: version, Baseline: f, State: "pending_upload", Conflicts: map[string]TrackerConflict{}}
		if e = saveRecord(tx, x); e == nil {
			_, e = tx.Exec("UPDATE milestones SET external_url=? WHERE id=?", x.URL, m.ID)
		}
		if e == nil {
			e = enqueue(tx, m.ID, "update", "{}")
		}
		if e == nil {
			_, e = tx.Exec("DELETE FROM tracker_outbox WHERE id=?", op.ID)
		}
		if e != nil {
			tx.Rollback()
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
	}
	return nil
}
