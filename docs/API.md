# REST API

All payloads are JSON with snake_case fields. IDs are opaque strings. Dates use `YYYY-MM-DD`; timestamps use UTC RFC3339. All API endpoints require authentication except `/api/auth/login` and `/api/auth/session`. `/healthz` is public.

## Authentication

`POST /api/auth/login` with `{"password":"..."}` returns `{"authenticated":true,"csrf_token":"..."}` and an HttpOnly `rg_session` cookie. `GET /api/auth/session` returns the current authentication state and CSRF token. Send cookies on subsequent requests and `X-CSRF-Token` for every non-GET/HEAD API request, including logout. `POST /api/auth/logout` invalidates the session.

## Resources

| Method | Path | Response |
| --- | --- | --- |
| GET | `/api/projects` | Array of projects ordered by sort order. |
| POST | `/api/projects` | Created project (201). |
| GET | `/api/projects/{id}` | Project. |
| PATCH | `/api/projects/{id}` | Updated project; omitted fields retained. |
| DELETE | `/api/projects/{id}` | 204; cascades milestones and dependency edges. |
| PUT | `/api/projects/order` | Atomic reorder; all project IDs and their expected `revisions` are required. |
| GET | `/api/milestones` | Array; optional `project_id`, `status`, `milestone_type`, `q`, `from`, `to` filters. |
| POST | `/api/milestones` | Created milestone (201). |
| GET | `/api/milestones/{id}` | Milestone. |
| PATCH | `/api/milestones/{id}` | Updated milestone. |
| DELETE | `/api/milestones/{id}` | 204; cascades dependency edges. |
| GET | `/api/dependencies` | Array of dependency edges. |
| POST | `/api/dependencies` | Created dependency (201). |
| DELETE | `/api/dependencies/{id}` | 204. |
| GET | `/api/graph` | `{projects:[],milestones:[],dependencies:[]}`. |
| GET | `/api/analysis/conflicts` | Conflict array. |
| GET | `/api/milestones/{id}/impact` | Direct/transitive dependent IDs, prerequisite IDs, conflict arrays. |
| GET | `/api/settings` | String-valued settings map. |
| PUT | `/api/settings` | Upsert settings map. |
| POST | `/api/demo` | Insert fictional demo transactionally; requires enabled flag and empty DB. |

## Shapes

Project create example:

```json
{"name":"COMMON MODULE","slug":"common-module","color":"#34c999","description":"Shared runtime","icon":"","sort_order":0}
```

Milestone create example:

```json
{"project_id":"opaque-project-id","title":"COMMON MODULE V2","description":"Runtime release","milestone_type":"release","version":"2.0","planned_date":"2027-03-20","completed_date":"","status":"planned","owner":"Release team","external_url":"https://example.org/releases/2"}
```

Milestone types: `release`, `feature`, `integration`, `market_launch`, `end_of_life`, `migration`, `custom`. Statuses: `planned`, `in_progress`, `completed`, `delayed`, `cancelled`. Completed milestones require a valid `completed_date`. External URLs must be HTTP(S). Responses add `id`, `created_at`, `updated_at`, and `revision`.

Dependency example:

```json
{"prerequisite_milestone_id":"common-module-v2-id","dependent_milestone_id":"project-x-v2-id","dependency_type":"requires"}
```

This means **PROJECT X V2 requires COMMON MODULE V2**. The visual arrow goes COMMON MODULE → PROJECT X. `related_to` is the informational type. Self-references, invalid milestone IDs, duplicate edges and hard-dependency cycles are rejected.

Conflict example:

```json
{"prerequisite_milestone_id":"common-module-v2-id","dependent_milestone_id":"project-x-v2-id","days":5,"category":"schedule_conflict"}
```

A positive day difference is how much later the prerequisite is available than the dependent's planned date. Completed prerequisites use actual completion dates; completed dependents and cancelled endpoints do not yield active conflicts.

`GET /api/milestones/{id}/impact?planned_date=2027-05-01` performs an in-memory date simulation without writing. Response fields: `direct`, `transitive`, `prerequisites` (ID arrays); `conflicts`, `existing_conflicts`, `new_conflicts` (conflict arrays). Conflict arrays describe the graph; the caller can restrict them to affected IDs. Apply a reviewed proposal explicitly using PATCH; the server does not move dependent dates.

## Errors

```json
{"error":{"code":"Unprocessable Entity","message":"Invalid planned date"}}
```

Typical statuses: 400 malformed/unknown JSON fields, 401 unauthenticated, 403 CSRF failure or disabled demo, 404 missing entity, 409 conflicting database references/uniqueness, 422 validation failure or hard-dependency cycle, 429 login throttling, 500 database failure. JSON request bodies are limited to 1 MiB. No pagination is provided in the initial MVP.

## Yandex Tracker

All routes require the existing session and mutation CSRF header. Credentials are backend environment values, never request/response fields. Unknown config fields (including tokens or destination URLs) are rejected.

| Method | Endpoint | Body / result |
|---|---|---|
| GET | `/api/tracker` | Shared `config`, project summaries, aggregate records/outbox/publications, health/warnings, `token_available`; no credentials |
| PUT | `/api/tracker/config` | Shared connection only, including current `revision`; see [example](tracker-config.example.json); returns canonical saved config with incremented revision |
| POST | `/api/tracker/test` | Tests saved shared connection without requiring any project or selection |
| POST | `/api/tracker/sync` | 202, schedules all enabled projects under the shared connection lease |
| POST | `/api/tracker/retry` | Resets only the shared connection cooldown |
| GET | `/api/projects/{id}/tracker` | Project `config`, `configured`, read-only shared `connection`, scoped records/outbox/publications, health/warnings |
| PUT | `/api/projects/{id}/tracker/config` | Independent full project config with current `revision`; see [example](tracker-project-config.example.json); canonical response increments revision |
| POST | `/api/projects/{id}/tracker/preview` | Expiring project-owned preview: `id`, `project_id`, `projects`, `milestones`, `links`, `skipped`, `warnings`, `unmapped_statuses`; no graph writes |
| POST | `/api/projects/{id}/tracker/import` | `{ "preview_id": "..." }`; atomically applies unchanged preview owned by this project |
| POST | `/api/projects/{id}/tracker/sync` | 202, schedules only this project's reconciliation; GET for results |
| POST | `/api/projects/{id}/tracker/retry` | Resets only this project's outbox attempts/backoff; then synchronize |
| POST | `/api/tracker/milestones/{id}/publish` | `{ "selection_id": "core-releases" }`; stable queue destination from the milestone's project only; 202 after durable enqueue |
| POST | `/api/tracker/milestones/{id}/unlink` | Excludes the stable external identity and clears its pending work; retains both objects |
| POST | `/api/tracker/milestones/{id}/resolve` | `{ "revision": 7, "choices": { "title": "local", "description": "remote" } }`; choose every disputed field using the owning project's mapping |

Settings GET returns revision 0 for an unsaved configuration. PUT requires that revision and returns the new one. Stale saves return 409; reload and review before retrying. Shared settings contain no selections or mappings, and project selections contain only stable `id`, `queue`, and `query` (no `project_id`/`new_project`). Unknown/legacy fields return 400. Nonexistent project IDs return 404. Legacy global `/api/tracker/preview` and `/api/tracker/import` return 410; they cannot overwrite project mappings.

Confirmation returns 409 for a wrong-project, stale, or expired token. Configuration changes in another project also invalidate previews conservatively because dependencies can cross projects. Invalid mappings/unsupported values return 422. Shared and project enablement are both required for preview/sync/publication. A disabled project can save partial settings without a selection and remains usable locally.

Field identity/codec/link changes require unlinking that project's bindings. Mapping-context changes are blocked while that project has pending operations, except the narrowly scoped partial-publication repairs described below; saved queue destinations used by publications cannot be changed or removed. Reordering selections is safe. Status mappings and write permissions may change after pending operations finish. Cross-project dependency ownership and pause rules are documented in the [Tracker guide](TRACKER.md).

Connection failures expose sanitized diagnostics without remote bodies. Mapped read-only edits use the milestone's project permissions; unmapped edits remain local. Moving linked/pending milestones to another project is rejected. Deleting milestones/projects retains external exclusions and never deletes Tracker issues; project deletion also removes configuration, previews and pending operations.

Preview `unmapped_statuses` contains `{key,name,count}` for each unknown status in new selected issues. Skipped notices retain `reason` and may also contain a `reasons` array of simultaneous blockers. The frontend can submit explicit mappings using that project's config endpoint and request a new preview; no mapping is inferred automatically.

## Entity revision contract (migration 4)

Project and milestone responses, including `/api/graph`, carry a persisted integer
`revision`, starting at 1. PATCH requires the revision observed when editing began:

```json
{"revision":7,"title":"Reviewed title"}
```

Send only changed fields. Missing or stale revisions return HTTP 409 with an
English error explaining that the entity changed and must be reloaded/reviewed.
No entity change or outgoing intent is committed on rejection. GET the entity to
review its current values; preserve the draft and explicitly save after review.
Never replace the revision on a stale full form and automatically resubmit it.

`PUT /api/projects/order` now requires
`{"ids":["p1","p2"],"revisions":{"p1":3,"p2":8}}`; stale lane revisions return 409.
Conflict resolution requires `{"revision":7,"choices":{"title":"remote"}}`.
Tracker status records include `local_revision` for this purpose. Actual entity
changes from imports, synchronization, resolution, URL binding, and reordering
advance the same revision. Synchronization bookkeeping alone does not.

## Recoverable publication contract

Tracker status responses include `publications`: safe recovery entries with
`local_id`, `project_id`, `selection_id`, `state`, `issue_key`, `url`, and `error`.
No credentials or remote payloads are returned. This history survives removal of
an active outbox operation and deletion of the milestone or project.

- `POST /api/tracker/milestones/{id}/unlink` cancels executable intent and preserves
  publication identity. `cancelled_before_attempt` means no creation was attempted;
  `cancelled_requires_recovery` means the remote outcome remains uncertain.
- `POST /api/tracker/milestones/{id}/recover` searches the original publication
  identity or fetches the confirmed issue ID. It performs no remote mutations and
  works even after local deletion. A found issue becomes `recovered_requires_decision`.
- To retain a recovered issue for an existing milestone, publish with
  `{"selection_id":"original-selection","retain":true}`. To detach, call unlink;
  the remote issue is preserved and the journal becomes `detached`.
- If recovery finds no issue, `recovery_not_found` still means an uncertain outcome.
  Explicitly resume with `{"selection_id":"original-selection","resume":true}`.
  This resumes the original stable unique identity and reconciles it again before
  creation; it never allocates a replacement identity.
- `created_needs_mapping` means an issue exists and its identity is durable, but
  initialization cannot decode its fields. Correct the reported mapping and sync
  again. Additive incoming status mappings, or correction of the particular field
  named in the decoding error, are allowed only when all pending project work is
  affected partial publication. Established bindings keep their existing field
  mapping protection. Unrelated pending work must finish or be cancelled first.
- Confirmed IDs are fetched directly on retry, independently of search indexing.
  Post-creation decoding failures increment attempts and retain actionable errors.
  Normal retry limits still apply; use the project retry action when exhausted.

Organization ID/header changes return 409 while bindings, pending operations, or
publication history retain the old identity. The message identifies retained
integration state as the reason. Token replacement for the same organization is
unaffected; settings revisions remain mandatory. Persisted operation identity and
live connection identity are checked independently by workers before remote calls.

Migration 4 is transactional and runs once. Legacy pending publications retain
unique IDs, payloads, destinations, attempts, cooldowns and errors. Their result
is conservatively treated as uncertain. Existing connection identity is copied
only when present; operations without an identifiable organization pause for
review. Migration never contacts Tracker. Upgrade API clients together with the
backend: unversioned edits are intentionally rejected.
