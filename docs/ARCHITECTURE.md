# Architecture

## Boundaries

One deployment unit: a Go HTTP server and its static Svelte/Vite production build. SQLite is the only data service. The Go SQLite driver uses CGO, so the Debian build and runtime stages share a compatible libc. No Redis, workers, external APIs or infrastructure accounts are required.

The UI loads `/api/graph` as one consistent rendering input. SVG is used for timeline lanes, markers and directed connections. Dates are calendar values `YYYY-MM-DD`, not user-local midnight timestamps. All lanes share the same date-to-X mapping; reordering only changes Y coordinates.

## Dependency semantics

`prerequisite_milestone_id → dependent_milestone_id` means the prerequisite must be available first. `requires` participates in cycle prevention, scheduling constraints and impact traversal. `related_to` is informational. Creating a hard edge is rejected if the dependent already reaches the prerequisite through hard edges.

Impact reports enumerate reachable milestones without rescheduling them. Direct scheduling conflicts compare a dependent's planned date to its prerequisite's availability date (actual completion date when completed, otherwise planned date). Cancelled milestones and completed dependents are excluded from active scheduling conflicts. Completion never changes another milestone's status.

## Storage

Relational tables hold projects, milestones, dependencies and settings. Foreign-key cascades remove descendants and edges when their owning project/milestone is deleted. Unique constraints reject duplicate references and slugs. Application validations supplement SQL checks. Schema versioning runs on startup. Multi-record demo insertion is transactional and restricted to an empty database.

## Authentication and security

The operator supplies `ROADMAP_PASSWORD` through environment configuration; the server computes a bcrypt hash and uses constant-work password verification. Random session identifiers are stored in memory and set as HttpOnly, SameSite=Strict cookies. Secure cookies are enabled by default. Mutation requests require the session's CSRF token in `X-CSRF-Token`; the same-origin UI retrieves it from the session endpoint. Logout and restart invalidate sessions.

API requests require authentication except login and session discovery. Health checks expose no graph data. JSON request limits, validation, parameterized SQL, security headers, safe URL handling and restrictive database permissions reduce common attack surfaces. The container runs without root and with a read-only filesystem except its data volume and temporary directory.

The initial deployment is intended for a small trusted team sharing one account behind TLS. Password rotation invalidates sessions on restart. Scaling this design requires replacing shared single-user credentials and process-local sessions, not adding database replicas to SQLite.

## Future decisions

Keep date semantics and dependency direction stable in API versions. Before accepting external integrations, add revision/ETag handling to prevent silent concurrent edits. Before expanding authentication, decide whether local users or trusted proxy identity is the product's long-term direction. Benchmark visible SVG behavior on real target hardware before introducing canvas or graph virtualization.
