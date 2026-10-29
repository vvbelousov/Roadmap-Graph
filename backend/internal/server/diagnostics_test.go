package server

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestDebugLogsDescribeOperationsWithoutCredentialsOrBodies(t *testing.T) {
	c, _, cfg := trackerSetup(t)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)
	c.expect("GET", "/api/graph?password=private-query-secret", "", 200)
	c.expect("POST", "/api/auth/login", `{"password":"private-body-secret"}`, 401)
	c.expect("PUT", "/api/tracker/config", jsonText(cfg), 200)
	_, err := c.s.tracker.request(context.Background(), cfg, "GET", "/v3/issues/secret-test-token?token=private-query-secret", nil, nil)
	if err == nil {
		t.Fatal("missing issue unexpectedly found")
	}
	output := logs.String()
	for _, event := range []string{"API request completed", "graph loaded", "Tracker API request completed", "Tracker configuration saved", "/v3/issues/{issue}"} {
		if !strings.Contains(output, event) {
			t.Fatalf("missing debug metadata %q", event)
		}
	}
	for _, secret := range []string{"secret-test-token", "private-query-secret", "private-body-secret", c.cookie, c.csrf} {
		if strings.Contains(output, secret) {
			t.Fatalf("debug logs exposed secret %q", secret)
		}
	}
}
