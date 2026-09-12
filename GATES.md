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
