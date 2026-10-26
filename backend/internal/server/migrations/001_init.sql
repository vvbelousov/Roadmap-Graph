
CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY);
CREATE TABLE IF NOT EXISTS projects(id TEXT PRIMARY KEY,name TEXT NOT NULL,slug TEXT NOT NULL UNIQUE,description TEXT NOT NULL,color TEXT NOT NULL,icon TEXT NOT NULL,sort_order INTEGER NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS milestones(id TEXT PRIMARY KEY,project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,title TEXT NOT NULL,description TEXT NOT NULL,milestone_type TEXT NOT NULL CHECK(milestone_type IN ('release','feature','integration','market_launch','end_of_life','migration','custom')),version TEXT NOT NULL,planned_date TEXT NOT NULL,completed_date TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('planned','in_progress','completed','delayed','cancelled')),owner TEXT NOT NULL,external_url TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS dependencies(id TEXT PRIMARY KEY,prerequisite_milestone_id TEXT NOT NULL REFERENCES milestones(id) ON DELETE CASCADE,dependent_milestone_id TEXT NOT NULL REFERENCES milestones(id) ON DELETE CASCADE,dependency_type TEXT NOT NULL CHECK(dependency_type IN ('requires','related_to')),created_at TEXT NOT NULL,CHECK(prerequisite_milestone_id!=dependent_milestone_id),UNIQUE(prerequisite_milestone_id,dependent_milestone_id,dependency_type));
CREATE INDEX IF NOT EXISTS milestones_project_date ON milestones(project_id,planned_date);
CREATE INDEX IF NOT EXISTS dependencies_dependent ON dependencies(dependent_milestone_id);
CREATE INDEX IF NOT EXISTS dependencies_prerequisite ON dependencies(prerequisite_milestone_id);
CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY,value TEXT NOT NULL);
INSERT OR IGNORE INTO schema_migrations VALUES(1);