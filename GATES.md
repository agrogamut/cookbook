# Gates: consultation intake

OWNS: internal/**, cmd/**, web/**, docs/**, .env.example, README.md, PLAN.md, GATES.md

Scope: Public registration, optional consultation payment, Supabase staff access, administrator management and assigned doctor caseloads.

- [x] G1: Backend tests pass, including input validation, role isolation and payment verification.
  CHECK: env GOCACHE=/tmp/recipie-2-go-cache go test ./...
  EXPECT: ok
  EVIDENCE: exit=0; shell=/usr/bin/fish; cwd=/home/ghoul/.ao/data/worktrees/recipie/recipie-2; path=ff60dfbea2fd/36 entries; output=ok  	github.com/madamgy/recipie/internal/profile	(cached) | ?   	github.com/madamgy/recipie/internal/xlsx	[no test files]

- [x] G2: Backend builds and passes static analysis.
  CHECK: env GOCACHE=/tmp/recipie-2-go-cache go build ./...; and env GOCACHE=/tmp/recipie-2-go-cache go vet ./...; and echo backend-checks-passed
  EXPECT: backend-checks-passed
  EVIDENCE: exit=0; shell=/usr/bin/fish; cwd=/home/ghoul/.ao/data/worktrees/recipie/recipie-2; path=ff60dfbea2fd/36 entries; output=backend-checks-passed

- [x] G3: Frontend tests and lint pass.
  CHECK: pnpm test; and pnpm lint; and echo frontend-checks-passed
  EXPECT: frontend-checks-passed
  CWD: web
  EVIDENCE: 2026-09-11, workspace/web, requested shell /usr/bin/fish: pnpm test exited 0 (8 files, 42 tests); pnpm lint exited 0. Both checks passed again after fixing the sidebar's theme-dependent initial render.

- [x] G4: Production frontend builds.
  CHECK: pnpm exec next build --webpack
  EXPECT: Route
  CWD: web
  EVIDENCE: 2026-09-11, workspace/web, requested shell /usr/bin/fish, exit 0: compiled, TypeScript checked, all 14 routes generated. Default Turbopack build hit restricted-process port binding; supported webpack production build passed. After the sidebar fix, a restricted rebuild could not parse the TypeScript configuration subprocess output; the same webpack build passed with normal process access.

- [x] G5: Local database integration verifies persistence, restricted access and payment replay behavior.
  EVIDENCE: 2026-09-11, repository root, requested shell /usr/bin/fish, PORTAL_TEST_DATABASE_URL points only to disposable PostgreSQL on 127.0.0.1:55434. go test ./internal/portal exited 0 after final changes. Tests exercise saved registration, optional email null, assignments, RLS, concurrent order reuse, signature rejection, captured payment, refunds, replay protection, revocation and concurrent password reset. Full go test ./... also passed with TEST_DATABASE_URL set after importing and enriching checksum-verified corpus data.

- [ ] G6: The live browser shows the landing form, validation and staff sign-in without exposing staff data.
  EVIDENCE: Partial, 2026-09-11: shared preview shows the supplied logo, requested inputs and informative sections. /admin redirects anonymous visitors to /login. With local Supabase Auth configured, the actual browser login form successfully signs in both roles, logout works, the doctor can open /books, and /admin redirects the doctor to /doctor. The admin Doctors tab shows both active accounts. The sidebar theme hydration mismatch was corrected; subsequent browser navigation produced no new errors. Native date entry and screenshot automation were unreliable in earlier checks. Full registration submission and confirmation pass component and database tests, but interactive intake submission still needs browser verification.

- [ ] G7: Supabase sign-in and Razorpay checkout work against the supplied project and test credentials.
  EVIDENCE: Hosted Supabase and Razorpay credentials remain pending. Official local Supabase Auth v2.196.0 with a verified loopback TLS gateway successfully created and authenticated an administrator and doctor. The real API rejected doctor access to administrator data with HTTP 403. This establishes local identity integration, not hosted-project or live-payment readiness.

## Family accounts, booking and gated book release

Scope: Separate family accounts, transactional doctor availability and booking, persisted book artifacts with administrator approval, and the corresponding portal screens.

- [x] G8: Migration creates isolated family identity, scheduling, and book-release tables with database-enforced overlap constraints and browser access revoked.
  CHECK: env GOCACHE=/tmp/recipie-family-go-cache go test ./internal/portal -run 'TestFamily|TestAvailability|TestAppointment|TestBookRelease' -count=1
  EXPECT: family-booking-gates-passed
  EVIDENCE: 2026-09-25, fresh PostgreSQL 16 disposable database on 127.0.0.1:55433, `go test ./internal/portal -run TestFamilyBookingReleaseAndRoleBoundaries -count=1` exited 0. The test applies migration 0042, checks browser-role RLS through the existing migration path, and checks direct overlapping availability and appointment inserts return SQLSTATE 23P01.

- [x] G9: Family and staff authorization boundaries hold at the HTTP layer.
  CHECK: env GOCACHE=/tmp/recipie-family-go-cache go test ./internal/portal ./internal/api -run 'TestFamily|TestStaff|TestBookRelease|TestAppointment' -count=1
  EXPECT: authorization-gates-passed
  EVIDENCE: 2026-09-25, the same portal integration test exited 0. It verifies family ownership, staff and family cookie separation, doctor denial of admin decisions and release approval, unapproved family download returning 404, and approved family download returning the stored bytes.

- [x] G10: Backend builds, vets, and runs the full test suite with the portal database configuration.
  CHECK: env GOCACHE=/tmp/recipie-family-go-cache go test ./... -count=1; env GOCACHE=/tmp/recipie-family-go-cache go build ./...; env GOCACHE=/tmp/recipie-family-go-cache go vet ./...; echo backend-family-checks-passed
  EXPECT: backend-family-checks-passed
  EVIDENCE: 2026-09-25, `PORTAL_TEST_DATABASE_URL` pointed to the fresh disposable database and `go test ./...` exited 0. `go build ./...` and `go vet ./...` also exited 0.

- [x] G11: Family, doctor, and administrator UI tests and production build pass.
  CHECK: pnpm test; pnpm lint; pnpm exec next build --webpack; echo frontend-family-checks-passed
  EXPECT: frontend-family-checks-passed
  CWD: web
  EVIDENCE: 2026-09-25, from `web/`, `pnpm test` exited 0 with 10 files and 48 tests, `pnpm lint` exited 0, and `pnpm exec next build --webpack` exited 0 with family, doctor and admin routes generated. The component tests assert that an unapproved release renders no download action and that retry and rebooking controls appear for recoverable states.

- [x] G12: Booking rescheduling and doctor deactivation preserve the scheduling invariants.
  CHECK: env PORTAL_TEST_DATABASE_URL=postgres://recipie:recipie@127.0.0.1:55433/recipie?sslmode=disable GOCACHE=/tmp/recipie-family-go-cache go test ./internal/portal -run TestFamilyBookingReleaseAndRoleBoundaries -count=1; and echo booking-review-gate-passed
  EXPECT: booking-review-gate-passed
  EVIDENCE: exit=0; shell=/usr/bin/fish; cwd=/home/ghoul/.ao/data/worktrees/recipie/recipie-3; path=b08d56a2bdca/35 entries; output=ok  	github.com/madamgy/recipie/internal/portal	0.055s | booking-review-gate-passed

- [x] G13: Family and admin scheduling controls expose the corrected retry, rebooking, timezone, edit, and move flows.
  CHECK: pnpm test; and pnpm lint; and pnpm exec tsc --noEmit; and echo portal-review-ui-gate-passed
  EXPECT: portal-review-ui-gate-passed
  CWD: web
  EVIDENCE: exit=0; shell=/usr/bin/fish; cwd=/home/ghoul/.ao/data/worktrees/recipie/recipie-3/web; path=b08d56a2bdca/35 entries; output=- ESM syntax in a file loaded as CommonJS (vitest.config.ts:1:1). Use a `.mjs` extension or set `"type": "module"` in the closest package.json | Set `VITE_CONFIG_NATIVE_IGNORE_WARNING=true` to suppress this warning.

- [x] G14: The corrected portal remains production-buildable.
  CHECK: pnpm exec next build --webpack; and echo portal-review-build-gate-passed
  EXPECT: portal-review-build-gate-passed
  CWD: web
  EVIDENCE: exit=0; shell=/usr/bin/fish; cwd=/home/ghoul/.ao/data/worktrees/recipie/recipie-3/web; path=b08d56a2bdca/35 entries; output=ƒ  (Dynamic)  server-rendered on demand | portal-review-build-gate-passed

## Paid consultation flow implementation

Scope: public and family scheduling, mandatory captured payment, protected child access, admin decisions and refunds.

- [x] G15: Database-backed portal tests pass with PostgreSQL.
  EVIDENCE: 2026-10-07, `PORTAL_TEST_DATABASE_URL` set to a disposable PostgreSQL 16 database. Portal tests passed, including concurrency, late capture, refunds, rebooking, sibling isolation and migration down/up preservation. The earlier 2026-10-04 run did not set this variable and skipped integration tests.

- [x] G16: Full Go suite, build and vet pass.
  EVIDENCE: 2026-10-07, `go test ./... -count=1` with the portal database enabled, `go build ./...` and `go vet ./...` exited 0. Corpus-dependent tests without their separate data configuration remain outside this portal check.

- [x] G17: Frontend tests, lint and type checking pass.
  EVIDENCE: 2026-10-07, 49 component/unit tests passed; lint and TypeScript checks exited 0.

- [x] G18: Production frontend build passes.
  EVIDENCE: 2026-10-07, `pnpm exec next build --webpack` exited 0 and generated all routes.

- [x] G19: Browser E2E exercises registration through payment, admin confirmation and protected download.
  EVIDENCE: 2026-10-07, Playwright journey passed against the real Next.js application, Go handlers and PostgreSQL with simulated external providers. Includes any-doctor booking/cancellation and screenshots at 360, 768 and 1440 pixels. Screenshots and recording: `docs/evidence/consultation-flow/`.

- [ ] G20: Hosted smoke checks and controlled Razorpay test-account transaction.
  EVIDENCE: publication and hosted checks pending. Local provider simulation does not establish real gateway settlement.
