package server

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Password, StaticDir string
	SecureCookie        bool
	Demo                bool
}
type session struct {
	CSRF    string
	Expires time.Time
}
type attempt struct {
	N     int
	Start time.Time
}
type Server struct {
	db       *sql.DB
	cfg      Config
	hash     []byte
	mux      *http.ServeMux
	mu       sync.Mutex
	sessions map[string]session
	attempts map[string]attempt
	writeMu  sync.Mutex
	tracker  *trackerClient
}

func New(db *sql.DB, c Config) (*Server, error) {
	if len(c.Password) < 12 {
		return nil, fmt.Errorf("ROADMAP_PASSWORD must contain at least 12 characters")
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(c.Password), bcrypt.DefaultCost)
	if e != nil {
		return nil, e
	}
	c.Password = ""
	s := &Server{db: db, cfg: c, hash: hash, mux: http.NewServeMux(), sessions: map[string]session{}, attempts: map[string]attempt{}}
	s.tracker = newTrackerClient()
	s.routes()
	return s, nil
}
func token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func now() string { return time.Now().UTC().Format(time.RFC3339) }
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": http.StatusText(status), "message": msg}})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		fail(w, 400, "Invalid JSON: "+e.Error())
		return false
	}
	var extra any
	if d.Decode(&extra) == io.EOF {
		return true
	}
	fail(w, 400, "Expected a single JSON value")
	return false
}
func (s *Server) current(r *http.Request) (session, bool) {
	c, e := r.Cookie("rg_session")
	if e != nil {
		return session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[c.Value]
	if ok && time.Now().After(v.Expires) {
		delete(s.sessions, c.Value)
		return session{}, false
	}
	return v, ok
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
		public := r.URL.Path == "/api/auth/login" || r.URL.Path == "/api/auth/session"
		if !public {
			v, ok := s.current(r)
			if !ok {
				fail(w, 401, "Authentication required")
				return
			}
			if r.Method != "GET" && r.Method != "HEAD" {
				if subtle.ConstantTimeCompare([]byte(v.CSRF), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
					fail(w, 403, "Invalid CSRF token")
					return
				}
				s.writeMu.Lock()
				defer s.writeMu.Unlock()
			}
		}
	}
	s.mux.ServeHTTP(w, r)
}
func (s *Server) routes() {
	s.trackerRoutes()
	s.mux.HandleFunc("PUT /api/projects/order", s.reorderProjects)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if e := s.db.Ping(); e != nil {
			fail(w, 503, "Database unavailable")
			return
		}
		respond(w, 200, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("GET /api/auth/session", func(w http.ResponseWriter, r *http.Request) {
		v, ok := s.current(r)
		respond(w, 200, map[string]any{"authenticated": ok, "csrf_token": v.CSRF})
	})
	s.mux.HandleFunc("POST /api/auth/login", s.login)
	s.mux.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		c, _ := r.Cookie("rg_session")
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "rg_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.cfg.SecureCookie, SameSite: http.SameSiteStrictMode})
		respond(w, 200, map[string]bool{"authenticated": false})
	})
	s.mux.HandleFunc("GET /api/graph", func(w http.ResponseWriter, r *http.Request) {
		g, e := s.graph()
		if e != nil {
			fail(w, 500, "Database error")
			return
		}
		respond(w, 200, g)
	})
	s.mux.HandleFunc("GET /api/analysis/conflicts", func(w http.ResponseWriter, r *http.Request) {
		g, e := s.graph()
		if e != nil {
			fail(w, 500, "Database error")
			return
		}
		respond(w, 200, Conflicts(g))
	})
	s.mux.HandleFunc("GET /api/milestones/{id}/impact", s.impact)
	s.mux.HandleFunc("POST /api/demo", s.demo)
	s.mux.HandleFunc("GET /api/settings", s.settings)
	s.mux.HandleFunc("PUT /api/settings", s.settings)
	for _, resource := range []string{"projects", "milestones", "dependencies"} {
		s.mux.HandleFunc("GET /api/"+resource, s.list)
		s.mux.HandleFunc("POST /api/"+resource, s.create)
		s.mux.HandleFunc("DELETE /api/"+resource+"/{id}", s.remove)
		if resource != "dependencies" {
			s.mux.HandleFunc("GET /api/"+resource+"/{id}", s.get)
			s.mux.HandleFunc("PATCH /api/"+resource+"/{id}", s.patch)
		}
	}
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "Endpoint not found") })
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			fail(w, 405, "Method not allowed")
			return
		}
		path := filepath.Join(s.cfg.StaticDir, filepath.Clean("/"+r.URL.Path))
		if st, e := os.Stat(path); e == nil && !st.IsDir() {
			http.ServeFile(w, r, path)
			return
		}
		http.ServeFile(w, r, filepath.Join(s.cfg.StaticDir, "index.html"))
	})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &b) {
		return
	}
	key, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		key = r.RemoteAddr
	}
	s.mu.Lock()
	a := s.attempts[key]
	if time.Since(a.Start) > 15*time.Minute {
		a = attempt{Start: time.Now()}
	}
	if a.N >= 15 {
		s.mu.Unlock()
		fail(w, 429, "Too many login attempts; retry after 15 minutes")
		return
	}
	a.N++
	s.attempts[key] = a
	s.mu.Unlock()
	if bcrypt.CompareHashAndPassword(s.hash, []byte(b.Password)) != nil {
		fail(w, 401, "Invalid password")
		return
	}
	id := token()
	v := session{token(), time.Now().Add(12 * time.Hour)}
	s.mu.Lock()
	delete(s.attempts, key)
	for k, x := range s.sessions {
		if time.Now().After(x.Expires) {
			delete(s.sessions, k)
		}
	}
	s.sessions[id] = v
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "rg_session", Value: id, Path: "/", MaxAge: 43200, HttpOnly: true, Secure: s.cfg.SecureCookie, SameSite: http.SameSiteStrictMode})
	respond(w, 200, map[string]any{"authenticated": true, "csrf_token": v.CSRF})
}
func dbfail(w http.ResponseWriter, e error) {
	slog.Warn("database operation rejected", "error", e)
	fail(w, 409, "Operation conflicts with existing data or references")
}
