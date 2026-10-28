# Backend

Go standard HTTP server, SQLite via `mattn/go-sqlite3` (CGO), bcrypt local authentication.

```sh
export ROADMAP_PASSWORD='replace-with-a-long-unique-password'
export ROADMAP_COOKIE_SECURE=false # local HTTP only; production HTTPS defaults true
export ROADMAP_DEMO=true          # optional; unlocks explicit empty-database demo init
export ROADMAP_STATIC=../frontend/dist
go run ./cmd/roadmap
```

`ROADMAP_ADDR` defaults `:8080`; `ROADMAP_DB` defaults `data/roadmap.db`. Database directory is created mode 0700; DB mode 0600. Use a dedicated user. SQLite WAL/SHM files reside with DB and need the same private directory. One SQLite connection, WAL mode, foreign keys enabled, 5s busy timeout. Application writes are serialized to make hard-dependency cycle checks atomic within this process. Run one application process per SQLite database.

Authentication: `POST /api/auth/login {"password":"..."}` sets HttpOnly `rg_session`, SameSite=Strict, 12-hour lifetime and returns `{authenticated:true,csrf_token}`. `GET /api/auth/session` returns current state. All other API endpoints require the session cookie; writes require `X-CSRF-Token`. Logout is `POST /api/auth/logout`. Sessions are memory resident and restart invalidates them. Password is supplied by deployment environment, bcrypt-hashed at process start and never stored in SQLite; do not commit environment secrets. Production requires HTTPS with the secure cookie default. Login rate limit is 15 failed attempts per network peer per 15 minutes; behind a reverse proxy this limit is shared by its peer address (forwarded headers are deliberately not trusted).

API errors are `{error:{code,message}}`. List routes return JSON arrays; create and update return entities. Input body limit 1 MiB. Unknown fields, malformed JSON, invalid calendar dates, unsafe external URL schemes, missing references, duplicate edges and hard cycles are rejected. PATCH permits only mutable fields; empty optional values use empty strings, not JSON null.

- `/api/projects` GET/POST; `/{id}` GET/PATCH/DELETE. `PUT /api/projects/order {"ids":["every-project-id-exactly-once"]}` atomically reorders all lanes and returns the sorted project array; incomplete, duplicated or unknown IDs are rejected without changes.
- `/api/milestones` GET/POST; `/{id}` GET/PATCH/DELETE. GET filters `project_id`, `status`, `milestone_type`, `q`, `from`, `to`.
- `/api/dependencies` GET/POST; `/{id}` DELETE. Fields `prerequisite_milestone_id`, `dependent_milestone_id`, `dependency_type` (`requires` or `related_to`). Arrow is prerequisite → dependent.
- `/api/graph` GET `{projects,milestones,dependencies}` in a consistent read transaction.
- `/api/analysis/conflicts` GET conflicts array `{prerequisite_milestone_id,dependent_milestone_id,days,category}`.
- `/api/milestones/{id}/impact` GET `{direct,transitive,prerequisites,conflicts,existing_conflicts,new_conflicts}`. ID arrays; transitive excludes direct. Optional `planned_date=YYYY-MM-DD` previews without saving. Conflicts lists cover the full graph; direct/transitive/prerequisites are hard dependency traversal only.
- `/api/settings` GET/PUT string map, persisted transactionally; PUT merges keys.
- `/api/demo` POST only with `ROADMAP_DEMO=true`, only if zero projects. Inserts fictional demo transactionally.
- `/healthz` GET unauthenticated database liveness.

Conflict semantics: only hard edges constrain scheduling. Availability is prerequisite `completed_date` for completed milestones, otherwise its `planned_date`. Completed milestones require actual completion date. Completed dependents and cancelled endpoints do not create scheduling conflicts. Completing a prerequisite does not change descendants. Impact identifies possible effects; it does not propagate predicted dates or automatically move milestones.

Migration v1 is embedded SQL at `internal/server/migrations/001_init.sql`; schema version is recorded. Future destructive migrations must implement explicit version sequencing and backups. The current schema initializes idempotently; no demo data is inserted on boot.

```sh
go test ./...
go test -race -cover ./...
go vet ./...
go test ./internal/server -run '^$' -bench . -benchmem
```

## Optional Yandex Tracker

Migration v2 adds connection metadata, stable issue baselines/conflicts, the durable outbox, dependency baselines, import previews and retry cooldowns. `StartTracker(ctx)` runs the optional poller in the existing process. Configure `ROADMAP_TRACKER_TOKEN` only in the backend environment; no token or custom API destination is accepted by integration endpoints. The connection is disabled by default. See [Tracker setup and recovery](../docs/TRACKER.md), [API routes](../docs/API.md), and [executed checks](../docs/VERIFICATION.md).
