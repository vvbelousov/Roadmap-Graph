package server

import "time"

type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Color       string `json:"color"`
	Icon        string `json:"icon"`
	SortOrder   int    `json:"sort_order"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}
type Milestone struct {
	ID            string `json:"id"`
	ProjectID     string `json:"project_id"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Type          string `json:"milestone_type"`
	Version       string `json:"version"`
	PlannedDate   string `json:"planned_date"`
	CompletedDate string `json:"completed_date"`
	Status        string `json:"status"`
	Owner         string `json:"owner"`
	ExternalURL   string `json:"external_url"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}
type Dependency struct {
	ID             string `json:"id"`
	PrerequisiteID string `json:"prerequisite_milestone_id"`
	DependentID    string `json:"dependent_milestone_id"`
	Type           string `json:"dependency_type"`
	CreatedAt      string `json:"created_at"`
}
type Graph struct {
	Projects     []Project    `json:"projects"`
	Milestones   []Milestone  `json:"milestones"`
	Dependencies []Dependency `json:"dependencies"`
}
type Conflict struct {
	PrerequisiteID string `json:"prerequisite_milestone_id"`
	DependentID    string `json:"dependent_milestone_id"`
	Days           int    `json:"days"`
	Category       string `json:"category"`
}

func Conflicts(g Graph) []Conflict {
	out := []Conflict{}
	ms := map[string]Milestone{}
	for _, m := range g.Milestones {
		ms[m.ID] = m
	}
	for _, d := range g.Dependencies {
		if d.Type != "requires" {
			continue
		}
		p, q := ms[d.PrerequisiteID], ms[d.DependentID]
		if p.Status == "cancelled" || q.Status == "cancelled" || q.Status == "completed" {
			continue
		}
		date := p.PlannedDate
		if p.Status == "completed" && p.CompletedDate != "" {
			date = p.CompletedDate
		}
		a, _ := time.Parse("2006-01-02", date)
		b, _ := time.Parse("2006-01-02", q.PlannedDate)
		if a.After(b) {
			out = append(out, Conflict{p.ID, q.ID, int(a.Sub(b).Hours() / 24), "schedule_conflict"})
		}
	}
	return out
}

// Reach follows prerequisite -> dependent for hard edges only. The root is excluded.
func Reach(ds []Dependency, root string, reverse bool) []string {
	adj := map[string][]string{}
	for _, d := range ds {
		if d.Type != "requires" {
			continue
		}
		a, b := d.PrerequisiteID, d.DependentID
		if reverse {
			a, b = b, a
		}
		adj[a] = append(adj[a], b)
	}
	seen := map[string]bool{root: true}
	queue := []string{root}
	out := []string{}
	for len(queue) > 0 {
		a := queue[0]
		queue = queue[1:]
		for _, b := range adj[a] {
			if !seen[b] {
				seen[b] = true
				out = append(out, b)
				queue = append(queue, b)
			}
		}
	}
	return out
}
