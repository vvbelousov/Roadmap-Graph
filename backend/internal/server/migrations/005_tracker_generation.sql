ALTER TABLE tracker_outbox ADD COLUMN generation INTEGER NOT NULL DEFAULT 1;
INSERT INTO schema_migrations VALUES(5);
