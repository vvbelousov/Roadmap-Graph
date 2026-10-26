# Verification and acceptance evidence

This report records execution evidence rather than intended functionality. Completed on 2026-10-25 in the supplied development environment. Results below are the final evidence from this execution. Unverified release gates are explicit.

## Executed backend checks

- `go test ./...`: 8 backend tests passed, covering CRUD, graph cycle prevention, invalid references, date validation, scheduling conflicts, direct/transitive traversal, impact simulation, authentication, CSRF, sessions, login rate limits, request-size limits, settings, demo restrictions and persistence.
- `go test -race -cover ./...`: passed on Go 1.27.3; `internal/server` coverage 76.3% (21.804 s).
- `go vet ./...`: passed.
- `python3 scripts/smoke.py`: passed against the actual executable. Uses isolated temporary SQLite, authenticates, creates Unicode project/milestones, adds a hard edge, detects a date conflict, queries impact, rejects a cycle, terminates the process, restarts it, verifies old-session rejection and exact graph persistence. Database permission verified as 0600. Reported execution time: 0.38 s.
- Backend graph benchmark, 300 milestones and 500 hard edges: combined conflict analysis and traversal 258,198 ns/op (~0.258 ms), 168,492 B/op, 996 allocations/op on this AMD EPYC 9V74 environment. This is an analysis benchmark, not a browser rendering or end-user latency measurement.

## Frontend and browser checks

- Dependency audit initially identified vulnerabilities in the Vitest 3 development toolchain. Upgraded to Vitest 5.0.5; final `npm audit --json` reports zero vulnerabilities.
- Frontend `npm run check`: zero errors, zero warnings; 13 Vitest tests passed across two files; production build passed (131.63 kB JS, 47.57 kB gzip; CSS 18.18 kB / 4.75 kB gzip).
- Playwright journey provided at `frontend/tests/e2e/core.spec.ts`; execution blocked: browser executable unavailable. Official Chromium download failed after five attempts with a truncated ZIP from the CDN. Two journeys are provided; neither has passed in this environment.
- Actual browser rendering of the sign-in page verified with the managed browser; screenshot: `screenshots/sign-in.jpg`. Authenticated graph screenshot and real browser interaction checks remain unverified. DOM integration tests use explicitly mocked network responses and a dialog adapter, so they do not prove real browser layout or end-to-end integration.

### DOM test scope

Seven actual Svelte component DOM tests cover project visibility, selection and details, dependency edge opacity/highlighting and tooltip, theme and Russian-language persistence, required fields, omission of immutable fields from PATCH, CSRF header, actual completion-date requirement, and login state. Six graph model tests cover date positioning, directional chains, informational cycles, same-lane routing, conflicts and the 30/300/500 model fixture. The model benchmark reported about 0.67 ms per combined traversal/conflict/positioning pass; this is not an SVG rendering benchmark.

## Deployment checks

A multistage Dockerfile (Go 1.27.3 builder) and Compose example are provided. The final Linux amd64 executable was built with Go 1.27.3 and passed the real-process smoke test again (0.36 s). The module language baseline remains Go 1.24. Current release source: https://go.dev/doc/devel/release.  Docker CLI/daemon are unavailable in this environment; image build, container startup and volume backup/restore have **not** been executed here. The actual Go process persistence test passed. Container verification remains a release gate; do not mark the Docker-build criterion complete based on source review.

## Acceptance matrix

| Criterion | Current evidence |
| --- | --- |
| Starts successfully | Passed actual executable smoke test. |
| SQLite persistence and restart | Passed actual process restart + exact graph comparison. |
| Project/milestone CRUD | Passed backend API tests; authenticated browser journey blocked. |
| Dependency create/remove | Passed backend API tests; authenticated browser journey blocked. |
| Shared timeline and date positioning | Passed date-to-X model tests; browser layout/navigation remains unverified. |
| Permanent edges, opacity and highlighting | DOM behavior tests passed; real browser verification remains open. |
| Direct/transitive chain inspection | Passed graph traversal and DOM selection tests; real browser inspection remains open. |
| Tooltips and click details | DOM behavior tests passed; real browser verification remains open. |
| Zoom/navigation/project filtering | Passed date-to-X model tests; browser layout/navigation remains unverified. |
| Conflicts and hard-cycle rejection | Passed backend tests and real executable smoke test. |
| English/Russian and dark/light | DOM behavior tests passed; real browser verification remains open. |
| Automated tests pass | 8 backend tests and 13 frontend tests passed; 2 Playwright scenarios blocked before launch by missing browser executable. |
| Docker image builds | Unverified: Docker unavailable. |
| Run documentation | README, architecture, API and deployment instructions present. |

## Scope and known gaps

The MVP is single-user. No enterprise RBAC, external synchronization, audit history or optimistic concurrency. SQLite and in-process sessions support one app instance. Dense graph readability and label collision management need more work. A test of the 30-project/300-milestone/500-edge browser target remains necessary; backend analysis speed alone does not establish frontend responsiveness. External production deployment was neither requested nor performed.
