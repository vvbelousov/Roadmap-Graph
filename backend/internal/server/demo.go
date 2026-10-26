package server

import (
	"fmt"
	"net/http"
)

func (s *Server) demo(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Demo {
		fail(w, 403, "Demo initialization disabled; set ROADMAP_DEMO=true explicitly")
		return
	}
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM projects").Scan(&count)
	if count != 0 {
		fail(w, 409, "Demo requires an empty database")
		return
	}
	tx, e := s.db.Begin()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	defer tx.Rollback()
	names := []string{"PROJECT X", "COMMON MODULE", "PROJECT F", "PROJECT Y"}
	ids := []string{"project-x", "common-module", "project-f", "project-y"}
	descs := []string{"Example application X", "Shared module for test projects", "Example application F", "Example service Y"}
	colors := []string{"#aab2c0", "#34d399", "#fb7185", "#60a5fa"}
	dates := [][]string{{"2026-12-12", "2027-03-15", "2027-09-20", "2028-02-01"}, {"2026-11-10", "2027-01-10", "2027-06-11", "2027-11-10"}, {"2026-11-15", "2027-02-02", "2027-08-18", "2028-01-15"}, {"2026-12-20", "2027-05-14", "2027-10-12", "2028-03-01"}}
	titles := [][]string{{"V1", "V2", "General availability", "Analytics studio"}, {"V1", "V2 · API v2", "V3 · Multi-tenant", "V4"}, {"V5", "V6 · Streaming v2", "V7 · New connector", "Optimization"}, {"V4 · Replication v2", "V5 · Sharding", "V2.0 · New architecture", "Migration"}}
	stamp := now()
	for i, n := range names {
		_, e = tx.Exec("INSERT INTO projects VALUES(?,?,?,?,?,?,?,?,?)", ids[i], n, ids[i], "FICTIONAL DEMO — "+descs[i], colors[i], "", i, stamp, stamp)
		if e != nil {
			dbfail(w, e)
			return
		}
		for j, t := range titles[i] {
			status := "planned"
			completed := ""
			typ := "release"
			if j == 0 {
				status = "completed"
				completed = dates[i][j]
			}
			if i == 1 && j == 1 {
				status = "in_progress"
			}
			if i == 2 && j == 1 {
				status = "delayed"
			}
			if i == 0 && j == 2 {
				typ = "market_launch"
			}
			if j == 3 {
				typ = "feature"
			}
			_, e = tx.Exec("INSERT INTO milestones VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)", fmt.Sprintf("%s-%d", ids[i], j), ids[i], t, "Fictional roadmap example. Edit or delete this milestone.", typ, fmt.Sprintf("%d.0", j+1), dates[i][j], completed, status, "Demo team", "", stamp, stamp)
			if e != nil {
				dbfail(w, e)
				return
			}
		}
	}
	edges := [][3]string{{"project-y-0", "common-module-1", "requires"}, {"project-f-1", "common-module-1", "requires"}, {"common-module-1", "project-x-1", "requires"}, {"project-y-1", "common-module-2", "requires"}, {"common-module-2", "project-x-2", "requires"}, {"project-f-2", "project-y-2", "related_to"}, {"project-y-2", "common-module-3", "requires"}}
	for i, d := range edges {
		_, e = tx.Exec("INSERT INTO dependencies VALUES(?,?,?,?,?)", fmt.Sprintf("demo-%d", i), d[0], d[1], d[2], stamp)
		if e != nil {
			dbfail(w, e)
			return
		}
	}
	if _, e = tx.Exec("INSERT INTO settings VALUES('demo','fictional')"); e != nil {
		dbfail(w, e)
		return
	}
	if e = tx.Commit(); e != nil {
		dbfail(w, e)
		return
	}
	g, e := s.graph()
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	respond(w, 201, g)
}
