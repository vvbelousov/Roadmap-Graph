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
| PUT | `/api/projects/order` | Atomic reorder, body `{ids:[all project IDs exactly once]}`. |
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

Milestone types: `release`, `feature`, `integration`, `market_launch`, `end_of_life`, `migration`, `custom`. Statuses: `planned`, `in_progress`, `completed`, `delayed`, `cancelled`. Completed milestones require a valid `completed_date`. External URLs must be HTTP(S). Responses add `id`, `created_at`, `updated_at`.

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
