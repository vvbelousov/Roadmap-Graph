# Yandex Tracker synchronization

The integration is optional and disabled by default. It uses one organization and the existing Go process and SQLite database. Local projects, milestones and simulations work without a connection. No production fixture server, external worker, webhook or additional database is involved.

## Connect

1. Obtain an OAuth token with `tracker:read` and `tracker:write` permissions for a dedicated account that can access the selected queues. Alternatively use an IAM token (`Bearer`) for Yandex Identity Hub; the operator must renew it before expiry. See [official API access](https://yandex.ru/support/tracker/en/api/access).
2. Provide `ROADMAP_TRACKER_TOKEN` in the backend environment (Docker Compose passes it through). Restart the backend after replacing it. The token is never stored in SQLite, included in an API response, or logged. There is no token input in the browser.
3. Create Roadmap projects using the normal UI. Open **Settings → Yandex Tracker**, enter the organization ID and choose Yandex 360 (`X-Org-ID`) or Yandex Identity Hub (`X-Cloud-Org-ID`). Choose OAuth or Bearer, and a polling interval of 30–86400 seconds.
4. Add one or more queue selections or Tracker search queries. Assign each selection to a Roadmap project. Queues do not automatically become products. Overlapping selections targeting different projects are rejected by preview. Grouping is applied on import; later changing a selection does not move existing milestones between projects.
5. Configure explicit status mappings and field/link mappings, save, and test the saved connection. Enable outgoing status mapping and the status field's write option if Roadmap status edits should be uploaded. Closed, rejected and completed statuses must be mapped deliberately. Test connection only calls `GET /v3/myself` and does not prove write permission for every queue.
6. Enable the integration, **Preview import**, inspect skipped issues, milestone dates and dependencies, then **Confirm import**. Preview makes no remote mutations. Confirmation persists the snapshot in a local transaction. A changed graph/configuration or expired preview requires previewing again. New Tracker issues discovered later require a new confirmed import.
7. **Synchronize** schedules a background run; status refreshes every five seconds while the integration panel is open. The regular poller starts with the server. Milestone details contain the Tracker link, state, mapping limitations, conflict choices and explicit **Publish to Tracker** / **Unlink** actions.

## Mapping and ownership

Mapped, writable fields have shared ownership and use three-way reconciliation. A field changed on one side propagates; identical changes converge; independent field changes merge. Different values for the same field create a synchronization conflict with baseline, Roadmap and Tracker values. Disputed fields are never silently overwritten. Choose a value for every conflict in milestone details, then synchronize. A further remote change before the upload produces a new conflict. While any field conflict remains, that issue's outgoing changes and dependency synchronization are paused.

Unmapped fields belong to Roadmap and remain local. Mapped fields without write permission are read from Tracker and cannot be changed through the local milestone PATCH API. Unlink first to edit them locally. Neither local creation nor simulation preview publishes anything; only applying a scenario calls the normal milestone PATCH API and queues a change. Ordinary local milestones remain unpublished until an explicit publication request selects a configured queue destination.

| Local field | Tracker mapping | Format / limits |
|---|---|---|
| Title | `summary` (required) | `string`, 200 bytes maximum locally |
| Description | `description` or configured field | `string` |
| Planned date | `dueDate` or configured date field (required) | `date`, exactly YYYY-MM-DD |
| Completion date | configured date-only field | `date`; completed milestones require a value |
| Status | `status` | `status`, explicit remote key → local status mappings; optional local status → remote key for writes |
| Responsible person | `assignee` or a user field | `id`, stores the user's stable ID rather than display name |
| Version | `fixVersions` or configured version field | `single_id`, zero or one object ID; multiple values are rejected |

Date-time fields such as `resolvedAt` are not accepted as date-only fields: configure a date-only completion field. Undated issues are skipped with a reason; dates are never invented. Status transitions requiring additional workflow form fields are outside this version; failures remain visible. Field formats are an explicit operator contract, not automatic schema inference. Read-only permission failures are visible and recoverable through Retry after correcting access.

Field IDs/formats, existing status meanings and link mappings cannot change while linked milestones or pending publications exist. Unlink them before replacing those mappings. Write permissions, outgoing status targets and additional status keys can be adjusted without reinterpreting existing baselines. Organization identity cannot change after importing any issue. Exclusion is permanent for an external issue ID in this initial version, including after its local milestone is deleted. Switching organizations with retained tombstones is not supported; this version is for one organization per database.

## Dependencies

Roadmap arrows always mean **prerequisite → dependent**. Configure the Tracker `type.id`, the `direction` for which the queried issue is the prerequisite, local `requires` or `related_to`, and the outgoing Tracker relationship relative to the prerequisite. For example, after verifying your Tracker link type, a dependency mapping normally uses outward for the blocking issue and `is dependent by` for outgoing creation. The reverse response from the dependent has inward direction and is deduplicated. Do not infer link semantics from localized labels.

Only configured link types between linked, selected, synchronized milestones are managed. Each local relationship type has at most one Tracker link mapping to prevent ambiguous write/delete behavior. Outside-scope links are listed in preview and are never changed. Unconfigured links remain untouched. `related_to` is undirected locally for deduplication and never participates in cycle checks. Self-links and the entire incoming hard-edge set are checked before local link changes; an invalid cycle produces a specific error and no partial incoming edge set. Dependency scheduling warnings and field synchronization conflicts are separate UI categories.

The link baseline records presence/absence. Local creation/removal is persisted with a durable link operation in the same transaction. Remote-only presence changes are imported; local-only changes are uploaded. Link retries inspect current links first, avoiding duplicates after a lost response. Tracker link endpoints do not document an atomic version precondition: a concurrent edit between the read and write can race. The next run reconciles the result; there is no distributed transaction or claim of atomic conflict protection for links/status transitions.

## Persistence, retries and partial results

Schema migration 2 adds `tracker_connection`, `tracker_records`, `tracker_outbox`, `tracker_edges`, connection-wide `tracker_retry`, and expiring `tracker_previews`. External IDs anchor identity; keys and URLs can change without duplicating milestones. The records persist connection ID, local ID, issue URL, external version, last acknowledged mapped values, state, errors and per-field conflicts. Local edit + outgoing intent commit together. No SQLite transaction stays open during HTTP calls.

PATCH uses the documented `?version=N` concurrency control; HTTP 409 leaves the baseline intact and forces reconciliation. Publication persists a random unique identifier **before** HTTP creation. Recovery searches by that identifier and sends the same Tracker `unique` value on subsequent attempts, so Tracker rejects duplicate creation even if search indexing lags. A 409 remains recoverable; it never triggers a new unique identifier. Once creation is observed, the issue is linked in a local transaction. Publication and subsequent status/field updates can succeed separately and are shown as pending/error until reconciled.

Reads/searches have at most four immediate attempts with exponential backoff, jitter and Retry-After handling. Field/publication/link outbox operations pause after eight failed attempts until the user resets them; retry times survive restarts. Long Retry-After delays are persisted for the connection and suppress subsequent runs until due or explicitly reset. Mutation transport errors are not blindly replayed. The next operation re-reads Tracker before writing. Requests are sequential and paced below the documented search limit (one request at a time, at most approximately 4.5 requests/second). There is a 20-second HTTP timeout, ten-minute run deadline, and twelve-minute SQLite connection lease. A crashed process's lease expires before recovery can start. The intended deployment remains **one backend process per SQLite database**; the lease also rejects overlapping synchronization runs.

The existing server write mutex serializes polling/import/mutations, so a long synchronization can delay other graph writes. Reads remain available between short database transactions. Search selections above 10,000 issues are explicitly rejected; narrow or split selections. Page pagination is not a remote snapshot and a later poll may be needed after issues change during pagination. Safe same-origin Link pagination is supported; redirects are refused and authenticated requests only use `https://api.tracker.yandex.net`. Neither API nor environment settings can override this destination.

Each issue commits separately; a later failure never rolls back already completed remote writes. Errors remain visible in records, connection status and outbox entries. Retry resets failed outgoing operations; Synchronize starts reconciliation. Unresolved conflicts can prevent the connection's last-success time advancing. Missing search results are fetched by stable ID: readable issues become `out_of_scope` (retained; uploads paused), 403 is `lost_access`, 404 is `missing_or_inaccessible`, and only an explicit 410 is labeled `deleted`. Tracker commonly cannot distinguish deletion from invisibility through 404; the integration never guesses. All states retain the local milestone. Local deletion unlinks/excludes the issue; there is no remote issue DELETE operation.

## Official API references reviewed

- [Authentication and organization headers](https://yandex.ru/support/tracker/en/api/access)
- [Issue search and pagination](https://www.yandex.ru/support/tracker/en/api/issues/search-issues)
- [Issue fields and local field IDs](https://yandex.ru/support/tracker/en/api/issues/fields)
- [Version-checked PATCH](https://www.yandex.ru/support/tracker/en/api/issues/patch-issue)
- [Create issue and unique identifier](https://yandex.ru/support/tracker/en/api/issues/create-issue)
- [Read transitions](https://yandex.ru/support/tracker/en/api/issues/get-transitions) and [execute transition](https://yandex.ru/support/tracker/en/api/issues/new-transition)
- [Link response direction](https://yandex.ru/support/tracker/en/api/issues/get-links), [paginated links](https://yandex.ru/support/tracker/ru/api/issues/get-links-paginate), [create](https://www.yandex.ru/support/tracker/en/api/issues/link-issue), [remove](https://yandex.ru/support/tracker/en/api/issues/delete-link-issue)
- [Request limits](https://yandex.ru/support/tracker/en/validation)

## Separate organization acceptance checks

Automated tests use a local HTTP emulator and never contact real Tracker issues. Before production enablement, use a separately authorized test organization to verify actual field IDs/schema, OAuth/IAM rights and rotation, queue workflow transitions and required fields, the configured dependency direction/relationship pair, unique-filter indexing after lost creation responses, permission-dependent 403/404 behavior, and real throttling/Retry-After behavior. No live organization has been modified as part of implementation.
