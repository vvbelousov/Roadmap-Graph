package server

import (
	"context"
	"fmt"
	"net/http"
)

func (s *Server) verifyConnectionIdentity(c TrackerConfig) error {
	current, e := s.connectionConfig()
	if e != nil {
		return e
	}
	if current.OrgID == "" || current.OrgID != c.OrgID || current.OrgHeader != c.OrgHeader {
		return fmt.Errorf("Tracker organization identity changed; retained operations require review")
	}
	return nil
}
func (s *Server) trackerRequest(ctx context.Context, c TrackerConfig, method, path string, body, out any) (http.Header, error) {
	if e := s.verifyConnectionIdentity(c); e != nil {
		return nil, e
	}
	var h http.Header
	e := s.trackerNetwork(ctx, func() (e error) { h, e = s.tracker.request(ctx, c, method, path, body, out); return })
	return h, e
}
func (s *Server) trackerIssue(ctx context.Context, c TrackerConfig, id string) (map[string]any, error) {
	if e := s.verifyConnectionIdentity(c); e != nil {
		return nil, e
	}
	var v map[string]any
	_, e := s.trackerRequest(ctx, c, "GET", issuePath(id), nil, &v)
	return v, e
}
func (s *Server) trackerSearch(ctx context.Context, c TrackerConfig, body map[string]any) ([]map[string]any, error) {
	if e := s.verifyConnectionIdentity(c); e != nil {
		return nil, e
	}
	return s.tracker.searchWith(ctx, c, body, s.trackerRequest)
}
