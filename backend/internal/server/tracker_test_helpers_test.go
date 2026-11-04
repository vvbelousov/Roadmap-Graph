package server

import "encoding/json"

func testTrackerProject(c *client) string {
	c.t.Helper()
	var id string
	if e := c.s.db.QueryRow("SELECT project_id FROM tracker_project_config ORDER BY project_id LIMIT 1").Scan(&id); e != nil {
		c.t.Fatal(e)
	}
	return id
}
func projectTrackerPath(c *client, action string) string {
	return "/api/projects/" + testTrackerProject(c) + "/tracker/" + action
}
func saveTestTracker(c *client, cfg TrackerConfig, want int) []byte {
	c.t.Helper()
	g, e := c.s.connectionConfig()
	if e != nil {
		c.t.Fatal(e)
	}
	desired := connectionPart(cfg)
	desired.Revision = g.Revision
	if jsonText(g) != jsonText(desired) {
		c.expect("PUT", "/api/tracker/config", jsonText(desired), 200)
	}
	id := cfg.ProjectID
	if id == "" {
		id = testTrackerProject(c)
	}
	p, _, e := c.s.projectTrackerConfig(id)
	if e != nil {
		c.t.Fatal(e)
	}
	next := projectPart(cfg)
	next.Revision = p.Revision
	return c.expect("PUT", "/api/projects/"+id+"/tracker/config", jsonText(next), want)
}
func retryTestTracker(c *client) {
	c.expect("POST", projectTrackerPath(c, "retry"), "{}", 200)
	c.expect("POST", "/api/tracker/retry", "{}", 200)
}
func readProjectTestConfig(c *client, id string) ProjectTrackerConfig {
	var result struct {
		Config ProjectTrackerConfig `json:"config"`
	}
	json.Unmarshal(c.expect("GET", "/api/projects/"+id+"/tracker", "", 200), &result)
	return result.Config
}
