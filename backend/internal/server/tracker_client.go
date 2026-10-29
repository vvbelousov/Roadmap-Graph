package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The destination is not configurable through environment, storage or the API.
const trackerOrigin = "https://api.tracker.yandex.net"

type trackerClient struct {
	http   *http.Client
	origin string
	pause  time.Duration
}

func newTrackerClient() *trackerClient {
	return &trackerClient{http: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, origin: trackerOrigin, pause: 220 * time.Millisecond}
}

type trackerHTTPError struct {
	Code    int
	RetryAt int64
	Method  string
	Path    string
	Fields  []string
}

func (e trackerHTTPError) Error() string {
	msg := fmt.Sprintf("Tracker HTTP %d", e.Code)
	if e.Method != "" && e.Path != "" {
		msg += " (" + e.Method + " " + e.Path
		if len(e.Fields) > 0 {
			msg += "; " + strings.Join(e.Fields, ", ")
		}
		msg += ")"
	}
	return msg
}

var trackerErrorFieldRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,127}$`)

// Only validation field IDs are retained. Remote messages, headers, query strings
// and payloads can echo credentials or issue contents and must never be exposed.
func newTrackerHTTPError(resp *http.Response, method, path string, data []byte, secret string) trackerHTTPError {
	e := trackerHTTPError{Code: resp.StatusCode, RetryAt: retryAt(resp.Header), Method: method, Path: strings.ReplaceAll(path, secret, "[redacted]")}
	var payload struct {
		Errors map[string]json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(data, &payload) == nil {
		for field := range payload.Errors {
			lower := strings.ToLower(field)
			if strings.Contains(lower, "token") || strings.Contains(lower, "authorization") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(field, secret) {
				continue
			}
			if trackerErrorFieldRE.MatchString(field) {
				e.Fields = append(e.Fields, field)
			}
		}
		sort.Strings(e.Fields)
		if len(e.Fields) > 8 {
			e.Fields = e.Fields[:8]
		}
	}
	return e
}
func httpCode(e error) int {
	if h, ok := e.(trackerHTTPError); ok {
		return h.Code
	}
	return 0
}
func sleepContext(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
func (c *trackerClient) request(ctx context.Context, cfg TrackerConfig, method, path string, body, out any) (http.Header, error) {
	u, e := url.Parse(path)
	if e != nil {
		return nil, fmt.Errorf("invalid API path")
	}
	base, _ := url.Parse(c.origin)
	if u.IsAbs() && (u.Scheme != base.Scheme || u.Host != base.Host) || u.User != nil || u.Fragment != "" {
		return nil, fmt.Errorf("unsafe pagination destination")
	}
	u = base.ResolveReference(u)
	if u.Scheme != base.Scheme || u.Host != base.Host {
		return nil, fmt.Errorf("unsafe pagination destination")
	}
	if !strings.HasPrefix(u.Path, "/v3/") {
		return nil, fmt.Errorf("unsafe API path")
	}
	secret := os.Getenv("ROADMAP_TRACKER_TOKEN")
	if secret == "" {
		return nil, fmt.Errorf("ROADMAP_TRACKER_TOKEN is not configured")
	}
	b, e := json.Marshal(body)
	if e != nil {
		return nil, e
	}
	for attempt := 0; attempt < 4; attempt++ {
		if e = sleepContext(ctx, c.pause); e != nil {
			return nil, e
		}
		req, e := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(b))
		if e != nil {
			return nil, fmt.Errorf("invalid Tracker request")
		}
		req.Header.Set("Authorization", cfg.Auth+" "+secret)
		req.Header.Set(cfg.OrgHeader, cfg.OrgID)
		req.Header.Set("Content-Type", "application/json")
		resp, e := c.http.Do(req)
		if e != nil { // Never expose transport errors: URLs and remote text may contain secrets.
			if attempt == 3 || method != "GET" && !strings.Contains(u.Path, "_search") && !strings.Contains(u.Path, "_list") {
				return nil, fmt.Errorf("Tracker network failure; operation will be reconciled before retry")
			}
			if e = sleepContext(ctx, time.Duration(1<<attempt)*time.Second+time.Duration(rand.IntN(250))*time.Millisecond); e != nil {
				return nil, e
			}
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if readErr != nil {
				return nil, fmt.Errorf("Tracker response interrupted")
			}
			if out != nil && len(data) > 0 {
				if json.Unmarshal(data, out) != nil {
					return nil, fmt.Errorf("invalid Tracker JSON")
				}
			}
			return resp.Header, nil
		}
		if resp.StatusCode != 429 && resp.StatusCode < 500 || attempt == 3 {
			return resp.Header, newTrackerHTTPError(resp, method, u.EscapedPath(), data, secret)
		}
		// Mutations are reconciled by the durable worker rather than blindly retried.
		if method != "GET" && !strings.Contains(u.Path, "_search") && !strings.Contains(u.Path, "_list") {
			return resp.Header, newTrackerHTTPError(resp, method, u.EscapedPath(), data, secret)
		}
		delay := time.Duration(1<<attempt)*time.Second + time.Duration(rand.IntN(250))*time.Millisecond
		if v := resp.Header.Get("Retry-After"); v != "" {
			if sec, err := strconv.Atoi(v); err == nil && sec >= 0 {
				delay = time.Duration(sec) * time.Second
			} else if d, err := http.ParseTime(v); err == nil {
				delay = time.Until(d)
			}
		}
		if delay > 5*time.Minute {
			return resp.Header, newTrackerHTTPError(resp, method, u.EscapedPath(), data, secret)
		}
		if delay < 0 {
			delay = 0
		}
		if e = sleepContext(ctx, delay); e != nil {
			return nil, e
		}
	}
	return nil, fmt.Errorf("Tracker retry limit")
}
func (c *trackerClient) search(ctx context.Context, cfg TrackerConfig, body any) ([]map[string]any, error) {
	out := []map[string]any{}
	path := "/v3/issues/_search?perPage=100&page=1"
	seen := map[string]bool{}
	for page := 1; page <= 10000; page++ {
		if seen[path] {
			return nil, fmt.Errorf("pagination loop")
		}
		seen[path] = true
		var batch []map[string]any
		h, e := c.request(ctx, cfg, "POST", path, body, &batch)
		if e != nil {
			return nil, e
		}
		out = append(out, batch...)
		if n, _ := strconv.Atoi(h.Get("X-Total-Count")); n > 10000 {
			return nil, fmt.Errorf("selection exceeds 10000 issues; narrow the query")
		}
		next := ""
		for _, link := range strings.Split(h.Get("Link"), ",") {
			if strings.Contains(link, `rel="next"`) {
				a, b := strings.Index(link, "<"), strings.Index(link, ">")
				if a >= 0 && b > a {
					next = link[a+1 : b]
					if strings.HasPrefix(next, "api.tracker.yandex.net/") {
						next = "https://" + next
					}
				}
			}
		}
		if next != "" {
			path = next
			continue
		}
		pages, _ := strconv.Atoi(h.Get("X-Total-Pages"))
		if page >= pages && len(batch) < 100 || pages > 0 && page >= pages {
			return out, nil
		}
		if len(batch) == 0 {
			return out, nil
		}
		path = fmt.Sprintf("/v3/issues/_search?perPage=100&page=%d", page+1)
	}
	return nil, fmt.Errorf("pagination limit exceeded")
}
func issuePath(id string) string { return "/v3/issues/" + url.PathEscape(id) }
func (c *trackerClient) issue(ctx context.Context, cfg TrackerConfig, id string) (map[string]any, error) {
	var v map[string]any
	_, e := c.request(ctx, cfg, "GET", issuePath(id), nil, &v)
	return v, e
}

func retryAt(h http.Header) int64 {
	v := h.Get("Retry-After")
	if seconds, e := strconv.Atoi(v); e == nil && seconds >= 0 {
		return time.Now().Add(time.Duration(seconds) * time.Second).Unix()
	}
	if d, e := http.ParseTime(v); e == nil {
		return d.Unix()
	}
	return 0
}
func retryTime(err error, fallback int64) int64 {
	if h, ok := err.(trackerHTTPError); ok && h.RetryAt > fallback {
		return h.RetryAt
	}
	return fallback
}
