package server

import (
	"database/sql"
	_ "embed"
	_ "github.com/mattn/go-sqlite3"
	"os"
	"path/filepath"
)

//go:embed migrations/001_init.sql
var migration string

//go:embed migrations/002_tracker.sql
var trackerMigration string

func OpenDB(path string) (*sql.DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if e != nil {
			return nil, e
		}
		f.Close()
		if e = os.Chmod(path, 0600); e != nil {
			return nil, e
		}
	}
	db, e := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000")
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec(migration + trackerMigration)
	if e != nil {
		db.Close()
		return nil, e
	}
	return db, nil
}
func (s *Server) graph() (Graph, error) {
	g := Graph{[]Project{}, []Milestone{}, []Dependency{}}
	tx, e := s.db.Begin()
	if e != nil {
		return g, e
	}
	defer tx.Rollback()
	r, e := tx.Query("SELECT * FROM projects ORDER BY sort_order,name")
	if e != nil {
		return g, e
	}
	for r.Next() {
		var p Project
		e = r.Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &p.Color, &p.Icon, &p.SortOrder, &p.CreatedAt, &p.UpdatedAt)
		if e != nil {
			r.Close()
			return g, e
		}
		g.Projects = append(g.Projects, p)
	}
	r.Close()
	r, e = tx.Query("SELECT * FROM milestones ORDER BY planned_date,id")
	if e != nil {
		return g, e
	}
	for r.Next() {
		var m Milestone
		e = r.Scan(&m.ID, &m.ProjectID, &m.Title, &m.Description, &m.Type, &m.Version, &m.PlannedDate, &m.CompletedDate, &m.Status, &m.Owner, &m.ExternalURL, &m.CreatedAt, &m.UpdatedAt)
		if e != nil {
			r.Close()
			return g, e
		}
		g.Milestones = append(g.Milestones, m)
	}
	r.Close()
	r, e = tx.Query("SELECT * FROM dependencies ORDER BY id")
	if e != nil {
		return g, e
	}
	for r.Next() {
		var d Dependency
		e = r.Scan(&d.ID, &d.PrerequisiteID, &d.DependentID, &d.Type, &d.CreatedAt)
		if e != nil {
			r.Close()
			return g, e
		}
		g.Dependencies = append(g.Dependencies, d)
	}
	r.Close()
	if e = tx.Commit(); e != nil {
		return g, e
	}
	return g, nil
}
