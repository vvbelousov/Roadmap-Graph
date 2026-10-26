package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type client struct {
	s            *Server
	cookie, csrf string
	t            *testing.T
}

func (c *client) call(method, path, body string) (int, []byte) {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if c.cookie != "" {
		r.Header.Set("Cookie", c.cookie)
	}
	if c.csrf != "" {
		r.Header.Set("X-CSRF-Token", c.csrf)
	}
	w := httptest.NewRecorder()
	c.s.ServeHTTP(w, r)
	return w.Code, w.Body.Bytes()
}
func (c *client) expect(method, path, body string, want int) []byte {
	c.t.Helper()
	code, b := c.call(method, path, body)
	if code != want {
		c.t.Fatalf("%s %s got %d want %d: %s", method, path, code, want, b)
	}
	return b
}
func setup(t *testing.T) (*client, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db.sqlite")
	db, e := OpenDB(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	s, e := New(db, Config{Password: "test-password-strong", Demo: true})
	if e != nil {
		t.Fatal(e)
	}
	c := &client{s: s, t: t}
	r := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBufferString(`{"password":"test-password-strong"}`))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	c.cookie = w.Result().Cookies()[0].Name + "=" + w.Result().Cookies()[0].Value
	var session map[string]any
	json.Unmarshal(w.Body.Bytes(), &session)
	c.csrf = session["csrf_token"].(string)
	return c, path
}
func project(c *client) Project {
	var p Project
	json.Unmarshal(c.expect("POST", "/api/projects", `{"name":"Тест","slug":"test","color":"#60a5fa"}`, 201), &p)
	return p
}
func milestone(c *client, p Project, title, date string) Milestone {
	var m Milestone
	json.Unmarshal(c.expect("POST", "/api/milestones", fmt.Sprintf(`{"project_id":%q,"title":%q,"planned_date":%q}`, p.ID, title, date), 201), &m)
	return m
}
func edge(c *client, a, b Milestone, typ string, status int) Dependency {
	var d Dependency
	json.Unmarshal(c.expect("POST", "/api/dependencies", fmt.Sprintf(`{"prerequisite_milestone_id":%q,"dependent_milestone_id":%q,"dependency_type":%q}`, a.ID, b.ID, typ), status), &d)
	return d
}
func TestCRUDGraphCyclesImpactPersistence(t *testing.T) {
	c, path := setup(t)
	p := project(c)
	c.expect("GET", "/api/projects/"+p.ID, "", 200)
	c.expect("PATCH", "/api/projects/"+p.ID, `{"name":"Updated","sort_order":2}`, 200)
	a := milestone(c, p, "A", "2027-02-01")
	b := milestone(c, p, "B", "2027-03-01")
	d := milestone(c, p, "C", "2027-04-01")
	ab := edge(c, a, b, "requires", 201)
	edge(c, b, d, "requires", 201)
	edge(c, d, a, "requires", 422)
	edge(c, d, a, "related_to", 201)
	edge(c, a, a, "requires", 422)
	missing := a
	missing.ID = "missing"
	edge(c, missing, b, "requires", 409)
	edge(c, a, b, "requires", 409)
	var impact struct {
		Direct, Transitive, Prerequisites []string
		NewConflicts                      []Conflict `json:"new_conflicts"`
	}
	json.Unmarshal(c.expect("GET", "/api/milestones/"+a.ID+"/impact?planned_date=2027-03-10", "", 200), &impact)
	if len(impact.Direct) != 1 || impact.Direct[0] != b.ID || len(impact.Transitive) != 1 || impact.Transitive[0] != d.ID || len(impact.NewConflicts) != 1 {
		t.Fatalf("impact %+v", impact)
	}
	c.expect("PATCH", "/api/milestones/"+a.ID, `{"planned_date":"2027-03-10"}`, 200)
	var conflicts []Conflict
	json.Unmarshal(c.expect("GET", "/api/analysis/conflicts", "", 200), &conflicts)
	if len(conflicts) != 1 || conflicts[0].Days != 9 {
		t.Fatalf("conflicts %+v", conflicts)
	}
	c.expect("GET", "/api/milestones?status=planned&project_id="+p.ID, "", 200)
	c.expect("PUT", "/api/settings", `{"language":"ru","theme":"light"}`, 200)
	// Open a separate DB connection to confirm committed data is independent of server memory.
	db, e := OpenDB(path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var count int
	db.QueryRow("SELECT COUNT(*) FROM milestones").Scan(&count)
	if count != 3 {
		t.Fatal(count)
	}
	c.expect("DELETE", "/api/dependencies/"+ab.ID, "", 204)
	c.expect("DELETE", "/api/milestones/"+b.ID, "", 204)
	c.expect("GET", "/api/milestones/"+b.ID, "", 404)
	c.expect("DELETE", "/api/projects/"+p.ID, "", 204)
	g, _ := c.s.graph()
	if len(g.Milestones) != 0 || len(g.Dependencies) != 0 {
		t.Fatal("orphans")
	}
	c.expect("DELETE", "/api/projects/"+p.ID, "", 404)
}
func TestAuthAndValidation(t *testing.T) {
	c, _ := setup(t)
	saved := c.cookie
	c.cookie = ""
	c.expect("GET", "/api/graph", "", 401)
	c.expect("POST", "/api/projects", `{}`, 401)
	c.cookie = saved
	csrf := c.csrf
	c.csrf = ""
	c.expect("POST", "/api/projects", `{}`, 403)
	c.csrf = csrf
	c.expect("POST", "/api/projects", `{"name":"A","slug":"a","color":"red"}`, 422)
	c.expect("POST", "/api/projects", `{"unknown":true}`, 400)
	c.expect("POST", "/api/projects", `{} garbage`, 400)
	p := project(c)
	m := milestone(c, p, "A", "2027-01-01")
	for _, b := range []string{`{"planned_date":"2027-02-30"}`, `{"external_url":"javascript:alert(1)"}`, `{"status":"completed"}`, `{"id":"new"}`, `{"title":null}`, `{"status":"random"}`} {
		c.expect("PATCH", "/api/milestones/"+m.ID, b, 422)
	}
	c.expect("PATCH", "/api/milestones/"+m.ID, `{"title":23}`, 400)
	c.expect("POST", "/api/auth/login", `{"password":"wrong"}`, 401)
	c.expect("POST", "/api/auth/logout", `{}`, 200)
	c.expect("GET", "/api/graph", "", 401)
}
func TestDemoAndSettings(t *testing.T) {
	c, _ := setup(t)
	c.expect("POST", "/api/demo", `{}`, 201)
	g, _ := c.s.graph()
	if len(g.Projects) != 4 || len(g.Milestones) != 16 || len(Conflicts(g)) == 0 {
		t.Fatal(g)
	}
	c.expect("POST", "/api/demo", `{}`, 409)
	c.expect("PUT", "/api/settings", `{"theme":"light"}`, 200)
	b := c.expect("GET", "/api/settings", "", 200)
	if !bytes.Contains(b, []byte("light")) {
		t.Fatal(string(b))
	}
}
func TestAnalysisSemantics(t *testing.T) {
	g := Graph{Milestones: []Milestone{{ID: "a", PlannedDate: "2027-05-01", Status: "planned"}, {ID: "b", PlannedDate: "2027-04-01", Status: "planned"}, {ID: "c", PlannedDate: "2027-03-01", Status: "planned"}}, Dependencies: []Dependency{{PrerequisiteID: "a", DependentID: "b", Type: "requires"}, {PrerequisiteID: "b", DependentID: "c", Type: "requires"}, {PrerequisiteID: "c", DependentID: "a", Type: "related_to"}}}
	if len(Reach(g.Dependencies, "a", false)) != 2 || len(Reach(g.Dependencies, "c", true)) != 2 || len(Conflicts(g)) != 2 {
		t.Fatal("analysis")
	}
	g.Milestones[0].Status = "completed"
	g.Milestones[0].CompletedDate = "2027-01-01"
	if len(Conflicts(g)) != 1 {
		t.Fatal("completion date ignored")
	}
	g.Milestones[1].Status = "cancelled"
	if len(Conflicts(g)) != 0 {
		t.Fatal("cancelled shouldn't constrain")
	}
}
func TestRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persist.sqlite")
	func() {
		db, e := OpenDB(path)
		if e != nil {
			t.Fatal(e)
		}
		defer db.Close()
		_, e = db.Exec("INSERT INTO settings VALUES('persist','yes')")
		if e != nil {
			t.Fatal(e)
		}
	}()
	db, e := OpenDB(path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var v string
	if e = db.QueryRow("SELECT value FROM settings WHERE key='persist'").Scan(&v); e != nil || v != "yes" {
		t.Fatal(v, e)
	}
}
func BenchmarkAnalysis300x500(b *testing.B) {
	g := Graph{}
	for i := 0; i < 300; i++ {
		g.Milestones = append(g.Milestones, Milestone{ID: fmt.Sprint(i), PlannedDate: time.Date(2027, 1, 1+i, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), Status: "planned"})
	}
	for i := 0; i < 500; i++ {
		a := i % 298
		g.Dependencies = append(g.Dependencies, Dependency{PrerequisiteID: fmt.Sprint(a), DependentID: fmt.Sprint(a + 1 + i%2), Type: "requires"})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Conflicts(g)
		Reach(g.Dependencies, "0", false)
	}
}

func TestAuthExpiryRateLimitAndBodyLimits(t *testing.T) {
	c, _ := setup(t)
	c.s.mu.Lock()
	for k, v := range c.s.sessions {
		v.Expires = time.Now().Add(-time.Second)
		c.s.sessions[k] = v
	}
	c.s.mu.Unlock()
	c.expect("GET", "/api/graph", "", 401)
	c.cookie = ""
	for i := 0; i < 15; i++ {
		c.expect("POST", "/api/auth/login", `{"password":"wrong"}`, 401)
	}
	c.expect("POST", "/api/auth/login", `{"password":"test-password-strong"}`, 429)
	c.expect("POST", "/api/auth/login", `{"password":"`+strings.Repeat("a", 1<<20)+`"}`, 400)
}
func TestDemoDisabledAndSecureCookie(t *testing.T) {
	db, e := OpenDB(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s, e := New(db, Config{Password: "test-password-strong", SecureCookie: true})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"password":"test-password-strong"}`))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	ck := w.Result().Cookies()[0]
	if !ck.Secure || !ck.HttpOnly || ck.MaxAge != 43200 {
		t.Fatalf("unsafe cookie %+v", ck)
	}
	var v map[string]any
	json.Unmarshal(w.Body.Bytes(), &v)
	c := &client{s: s, t: t, cookie: ck.Name + "=" + ck.Value, csrf: v["csrf_token"].(string)}
	c.expect("POST", "/api/demo", "", 403)
}

func TestProjectReorderTransaction(t *testing.T) {
	c, _ := setup(t)
	p := project(c)
	var q Project
	json.Unmarshal(c.expect("POST", "/api/projects", `{"name":"Second","slug":"second","color":"#34d399","sort_order":5}`, 201), &q)
	c.expect("PUT", "/api/projects/order", fmt.Sprintf(`{"ids":[%q,%q]}`, q.ID, p.ID), 200)
	g, e := c.s.graph()
	if e != nil {
		t.Fatal(e)
	}
	if g.Projects[0].ID != q.ID || g.Projects[0].SortOrder != 0 || g.Projects[1].ID != p.ID || g.Projects[1].SortOrder != 1 {
		t.Fatalf("unexpected order: %+v", g.Projects)
	}
	for _, body := range []string{fmt.Sprintf(`{"ids":[%q,%q]}`, p.ID, p.ID), fmt.Sprintf(`{"ids":[%q,"missing"]}`, p.ID), fmt.Sprintf(`{"ids":[%q]}`, p.ID), `{"ids":[]}`} {
		c.expect("PUT", "/api/projects/order", body, 422)
		after, e := c.s.graph()
		if e != nil {
			t.Fatal(e)
		}
		if after.Projects[0].ID != q.ID || after.Projects[0].SortOrder != 0 || after.Projects[1].ID != p.ID || after.Projects[1].SortOrder != 1 {
			t.Fatal("invalid reorder mutated stored order")
		}
	}
}
