# Verification and acceptance evidence

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
