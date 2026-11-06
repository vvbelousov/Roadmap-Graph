# Verification and acceptance evidence

## Update: 2026-10-29 project-specific Tracker settings

Verification used isolated temporary SQLite databases, the existing local Tracker HTTP emulator, and a separate loopback backend with no Tracker token. No real Tracker issues, running deployment, or production database were modified.

- Backend: 36 test functions passed. Full `go test -race -cover ./...` passed (76.9% server statement coverage), followed by a race check of the added undirected cross-project ownership regression. `go vet ./...` and native production compilation passed.
- Frontend: 25 tests passed, including project-only status resolution, custom-date draft preservation, canonical saves, shared connection setup without projects, navigation discard protection, and cancellation of retained publications in disabled projects. Svelte checking reported zero errors/warnings; production build passed.
- Browser: all 4 Playwright scenarios passed in Chrome against the isolated backend. These cover existing roadmap behavior, global connection → project configuration → preview/confirmation/conflict resolution, Russian/light/mobile global settings, and independent A/B settings with unsaved-draft cancellation/acceptance and mobile project mapping controls.
- Docker: the local `roadmap-graph:project-settings-test` image built successfully. No production container was started, replaced or deployed.
- Migration tests start from the original v2 schema and retain issue identities, baselines/conflicts/exclusions, outbox unique IDs, publication queues and attempts/backoff. Projects without a legacy selection pause with review warnings; restart preserves independent migrated configurations.
- Project integration tests cover different queues/date fields/status meanings/permissions/outgoing transitions, stale-save rejection, preview token ownership, overlapping identities, publication selection reordering, protected mapping context and moves, disabled/invalid project isolation, deletion cleanup, cross-project hard/related link ownership and deduplication, incompatible mappings, and paused removals without retry exhaustion.

Remaining limits: one backend process per SQLite database; pending operations must finish or be explicitly unlinked before changing mappings; linked/pending milestones cannot move projects; ambiguous legacy publication destinations require review/cancellation. Actual organization-specific workflow/schema behavior still requires separately authorized service acceptance testing. Existing Tracker link/status endpoints retain their documented concurrency limits; no distributed transaction is claimed. Docker runtime/volume backup and large-graph performance were outside this change. Nothing was committed, pushed or deployed.

The earlier verification records below describe previous revisions.

## Update: 2026-10-29 diagnostics and settings

- Passed 25 backend tests, including status-key corrections after import, three-way conflict preservation after mapping changes, and credential/payload redaction in debug logs. `go vet ./...` and production backend compilation passed.
- Passed 20 frontend tests, including saving/reopening corrected keys and field permissions, exclusive outgoing status targets, and timeline diagnostics/Fit date-window recovery. Type checking reported zero errors/warnings; the production frontend build passed.
- Passed all 3 Playwright scenarios against a new isolated local database with debug logging enabled. Real Tracker data was not modified.
- Read-only inspection of the running instance found one synchronized milestone, AP-270, planned for 2026-11-06. A fresh browser rendered it without JavaScript errors. This does not reproduce the user's existing browser filters/scroll state; `?debug=1` now exposes those conditions.
- Fixed the backend restriction that rejected status-key edits after import, moved save feedback beside the controls, and prevented silently selecting multiple outgoing destinations for one local status. Added `ROADMAP_LOG_LEVEL` and credential-safe request/sync metadata.
- The running container was not rebuilt or replaced. The earlier verification record follows.


Verified on 2026-10-28 against the working tree. Tests used isolated SQLite databases and local HTTP servers; no real Tracker issues or infrastructure were modified.

| Check | Result |
|---|---|
| Backend `go test ./...` | Passed, 20 test functions including 12 Tracker integration tests |
| Backend `go test -race -cover ./...` | Passed; `internal/server` statement coverage 75.2% |
| Backend `go vet ./...` | Passed |
| Backend production `go build ./cmd/roadmap` | Passed; temporary binary outside repository |
| Frontend `npm run check` | Passed, zero errors and warnings |
| Frontend `npm test` | Passed, 16 tests across 3 files |
| Frontend `npm run build` | Passed |
| Playwright browser suite | Passed, 3 scenarios using installed Chrome and isolated local backend |
| `git diff --check` | Passed |
| Docker build | Attempted; base-image downloads stalled for several minutes and the build was stopped. Image build remains unverified. |

Backend checks used a temporary Go 1.24.7 toolchain. Frontend checks used temporary Node.js 22.22.0 and the existing lockfile; no new application dependencies were introduced.

## Tracker coverage

The local HTTP emulator verifies initial/repeated imports, pagination, stable identity across key changes, explicit date/status/person/version mappings, undated/unmapped exclusions, bidirectional field changes, independent merges, same-field conflicts and explicit resolution, version-conflict protection, status transitions, publication with lost-response recovery after closing/reopening SQLite, link direction/deduplication/create/remove, lost link response recovery, incoming-cycle rejection without partial application, 429/503 retries, persisted mutation Retry-After and connection cooldowns, bounded outbox retries and manual reset, outbox rollback together with rejected local edits, stale preview rejection, exclusion/deletion behavior, selection departure, 403/404/410 states, unsafe pagination/redirect protection, sanitized errors, credentials absent from API responses, and no outgoing operations from impact simulation.

Frontend component tests cover connection settings, preview-before-confirmation, explicit conflict choices, publication destinations and Russian controls. Browser tests cover actual local roadmap CRUD/dependencies/conflicts/persistence; tooltip/filter/zoom/validation/theme/language behavior; and Tracker setup → test connection → preview → confirm import → conflict resolution. The Tracker browser scenario additionally checks Russian labels and the light theme at a 390-pixel mobile viewport without page overflow. Browser Tracker responses are test fixtures; production always uses the Go API.

Browser verification also exposed and fixed delayed dialog-close events clearing a newly opened form and SVG milestone hit-testing issues. Existing browser tests now target marker glyphs and accessible combobox roles and wait for asynchronous filtering.

## Remaining acceptance gates and limits

A separately authorized Tracker test organization is still required to verify real OAuth/IAM rights and rotation, organization-specific field schemas, workflow transitions/required inputs, configured dependency semantics, unique-filter indexing, access/deletion behavior and real service throttling. See [Tracker setup and limitations](TRACKER.md).

Docker build/runtime and volume backup/restore remain unverified because image retrieval stalled. Dense timeline label collisions and the 30-project/300-milestone/500-edge browser performance target were not evaluated in this integration task. The application remains one backend process per SQLite database with shared instance authentication; no enterprise RBAC or local-client optimistic concurrency was added.

Nothing was committed, pushed or deployed.

## Update: 2026-11-06 data-integrity stabilization

This section supersedes older limitations about local optimistic concurrency and
Docker verification. No real Tracker data was accessed. The original checkout was
`fe088da9f558ab221e30c5d16010dd92efb0942a`, with no pre-existing working-tree changes.

All four confirmed defects were reproduced by restoring the affected original
functions in a temporary copy with the regression scaffolding: cancellation allowed
another publication, partial creation lacked durable recovery identity, pending work
did not prevent an organization change, and a stale edit returned 200. All four
principal regression tests failed against those implementations.

| Defect | Fix and regression coverage |
| --- | --- |
| Lost response → cancel → republish | Non-executable durable publication journal; original unique identity survives cancellation/restart/deletion. Read-only recovery, explicit retain/detach and original-attempt resume. `TestIntegrityCancelledPublicationRecovery`, `TestIntegrityDeletionPreservesPublicationRecovery`. |
| Created issue with undecodable initial status | Confirmed ID/key/URL committed before field decoding; `created_needs_mapping`, recorded attempts/errors, narrowly permitted mapping repair, then initialization against the confirmed ID. `TestIntegrityPartialPublicationMappingRestart`, including rejection of a repair while unrelated publication remains queued. |
| Organization change with queued work | API counts bindings, outbox and retained journal; persisted organization context checked independently by worker and at remote call boundaries. `TestIntegrityOrganizationPendingDefense` covers both operation and journal mismatches. |
| Stale local form overwrites current data | Persisted revisions, transactional checks, revision triggers for actual entity changes, changed-field forms, preserved drafts and explicit comparison/review. `TestIntegrityStaleEntityEdits`, `TestIntegrityTrackerChangesAdvanceRevision`, UI stale-draft test and the real two-tab browser scenario. |

Migration 4 appends schema changes transactionally and once only. The populated
upgrade test retains a linked issue and its baseline, a pending publication's
unique ID, destination, attempts, cooldown and error, then reopens twice. Legacy
publication outcomes are conservative; unavailable organization identity pauses
work. Existing migrations are unchanged. No migration makes remote calls.

Frontend coverage includes English/Russian recovery controls, confirmed-issue
links, explicit retain/detach, stale rejection, changed-field requests and draft
preservation/review. The Playwright suite covers the existing CRUD, theme/language,
Tracker fixtures and independent project-settings scenarios plus actual stale
editing from two tabs. Browser tests run against an isolated temporary container
with no Tracker credentials. HTTP-emulator tests verify synchronization behavior.

The same-organization requirement and one-backend-process-per-database architecture
remain. Recovery history is deliberately retained; switching organizations with
that history is unsupported. Deleted local milestones cannot retain a binding.
Legacy operations with unknown organization identity require administrator review.
Live organization-specific Tracker workflows and real-service indexing/throttling
are outside this pass and were intentionally not exercised. No commits, pushes or
deployments were performed.

Stabilization checks:

| Check | Result |
| --- | --- |
| Backend `go test ./...` | Passed, existing suite plus 7 integrity regressions |
| Backend `go test -race ./...` | Passed; no detected races |
| Backend `go vet ./...` | Passed |
| Backend production build | Passed, binary in `/tmp` |
| Frontend type checks | Passed, zero errors/warnings |
| Frontend component/unit tests | Passed, 30 tests across 4 files |
| Frontend production build | Passed |
| Docker production build | Passed using the repository Dockerfile |
| Docker runtime/browser checks | Passed on the final image: isolated container and local Chrome, all 5 Playwright scenarios |
| `git diff --check` | Passed |

No application dependencies were changed. Native backend verification used Go
1.24.7; the Docker build also verifies the Dockerfile's Go toolchain. Browser
fixtures do not contact Tracker. Container test databases are disposable and
separate from any user database.
