# Family consultation flow implementation plan

> **For implementation workers:** Follow the repository's task-by-task superpowers workflow. Each
> task below has a narrow file scope, a test oracle, and a handoff condition. Do not begin a
> dependent task until its earlier contract and tests pass.

**Goal:** Connect registration, doctor selection, appointment holds, payment, admin review, family access, and approved book delivery into one truthful flow.

**Architecture:** Keep the existing Go portal as the only authority for ownership, availability, payment state, appointment transitions, and book-release access. The public first-visit wizard uses its private registration token until the family completes a return-access verification step. The family portal consumes the same registration and appointment records through an HttpOnly session. The Next.js frontend renders state returned by the API and never infers confirmation from a checkout callback.

**Tech stack:** Go, chi, pgx/v5, Postgres migrations, Next.js App Router, React, Tailwind, shadcn/ui, Vitest, and a browser E2E runner using the gateway test account.

**Spec:** `docs/superpowers/specs/2026-10-04-family-consultation-flow.md`

**Depends on:** Current migrations `0041_consultation_portal` and `0042_family_booking_and_book_release`, the existing registration token flow, the existing family session flow, and the configured test-mode payment gateway.

## Global constraints

- The server decides every payment, appointment, ownership, and book-release state.
- A payment callback from the browser never confirms an appointment by itself.
- An appointment cannot become `confirmed` until a captured payment is verified and an administrator confirms it.
- Expired payment holds must be treated as free in the same transaction that creates the next hold.
- Specific-doctor and any-doctor requests share one availability and collision-checking path.
- Child full name and date of birth are lookup inputs. A second factor is required before child files are returned.
- Unknown child, wrong date, and wrong second factor use non-disclosing responses.
- Do not store raw one-time codes, payment secrets, or production passwords in source control.
- Keep all family and payment data behind Go server routes. Browser roles never receive direct database access.
- Preserve the current Asia/Kolkata display rules and UTC storage.
- Retain the current request-origin protection, rate limits, raw-body webhook signature check, and idempotent payment-event handling.
- Public and family pages must work at 360 px, 768 px, and 1440 px widths with keyboard navigation.
- Use the existing logo, palette, and checked-in assets. Do not add unverified clinical imagery.
- Run `go test ./...`, `go build ./...`, `go vet ./...`, `pnpm test`, `pnpm lint`, and `pnpm build` before calling the implementation complete.

## File structure

New files:

- `internal/db/migrations/0043_paid_booking_and_family_access.up.sql`
- `internal/db/migrations/0043_paid_booking_and_family_access.down.sql`
- `internal/portal/family_access.go`
- `internal/portal/family_access_test.go`
- `web/src/components/family-access.tsx`
- `web/src/components/appointment-wizard.tsx`
- `web/src/components/payment-summary.tsx`
- `web/e2e/family-consultation.spec.ts`
- `web/playwright.config.ts`

Modified files:

- `internal/portal/server.go`
- `internal/portal/booking.go`
- `internal/portal/payments.go`
- `internal/portal/guardians.go`
- `internal/portal/family_home.go`
- `internal/portal/registrations.go`
- `internal/portal/integration_test.go`
- `internal/portal/family_booking_test.go`
- `web/src/lib/api.ts`
- `web/src/lib/portal-types.ts`
- `web/src/lib/portal-utils.ts`
- `web/src/components/consultation-form.tsx`
- `web/src/components/family-portal.tsx`
- `web/src/components/admin-operations.tsx`
- `web/src/components/registration-workspace.tsx`
- `web/src/components/admin-workspace.tsx`
- `web/src/app/family/login/page.tsx`
- `web/src/app/family/register/page.tsx`
- `web/src/app/login/page.tsx`
- `web/src/app/landing.css`
- `web/src/components/consultation-form.test.tsx`
- `web/src/components/family-portal.test.tsx`
- `docs/consultation-portal.md`

---

### Task 1: Lock the transition contract with integration tests

**Files:**

- Test: `internal/portal/family_booking_test.go`
- Test: `internal/portal/integration_test.go`
- Modify: `internal/portal/booking.go`
- Modify: `internal/portal/payments.go`

**Produces:** Tests that describe the only legal transitions before the migration changes the database constraints.

- [ ] Add a test for a successful specific-doctor flow: registration token, availability, hold, order, payment verification, admin confirmation, and family status.
- [ ] Add a test for an any-doctor flow and assert that the selected doctor is eligible for the entire requested interval.
- [ ] Add tests proving that unpaid, failed, dismissed, and expired holds cannot become `confirmed`.
- [ ] Add a test for a captured payment that enters `paid_pending_admin` and remains visible to admin.
- [ ] Add a test for admin rejection of a captured request and the resulting `refund_required` state.
- [ ] Add duplicate and out-of-order payment-event tests before changing existing payment code.
- [ ] Run `go test ./internal/portal/...` and record the current failures as the implementation oracle.

**Handoff:** The test names and expected states are stable enough for the migration and handler work.

### Task 2: Add paid appointment holds and refund bookkeeping

**Files:**

- Create: `internal/db/migrations/0043_paid_booking_and_family_access.up.sql`
- Create: `internal/db/migrations/0043_paid_booking_and_family_access.down.sql`
- Modify: `internal/portal/booking.go`
- Modify: `internal/portal/payments.go`
- Modify: `internal/portal/family_home.go`

**Interfaces:**

- Appointment states include `awaiting_payment`, `paid_pending_admin`, `confirmed`, `rejected`, `expired`, `cancelled`, `refund_required`, and `refunded`.
- `awaiting_payment` rows carry `hold_expires_at`.
- Consultation orders link to the appointment attempt they fund while preserving older registration-only orders.
- Refund records retain gateway refund ID, amount, timestamps, and the actor who started the action.

- [ ] Add the appointment state constraint and hold expiry column without losing current rows.
- [ ] Add `appointment_id` to consultation orders and make the active-order rule apply to one active attempt, not the entire registration history.
- [ ] Add refund fields or a refund table that can represent requested, submitted, completed, and failed refunds.
- [ ] Add an access-factor table with registration ID, channel, destination hash, code hash, expiry, attempt count, consumed timestamp, and creation timestamp.
- [ ] Add a redacted access-attempt table or audit event path. Never persist raw codes.
- [ ] Add partial indexes for one active appointment per registration and one active hold per doctor/time range.
- [ ] Write a reversible down migration and verify it on a disposable database.
- [ ] Run `go test ./internal/db/...` with the portal database configured.

**Handoff:** The schema supports retries, expired holds, paid pending review, and refunds without deleting the audit trail.

### Task 3: Implement atomic holds and appointment transitions

**Files:**

- Modify: `internal/portal/booking.go`
- Modify: `internal/portal/server.go`
- Test: `internal/portal/family_booking_test.go`

- [ ] Add a public-token hold route for the first-visit wizard. It must verify the registration token and registration ownership before creating a hold.
- [ ] Keep the family-session route for returning family users, using the same hold implementation.
- [ ] Treat expired holds as released inside the hold transaction.
- [ ] Select a doctor using the current load-balancing rule for any-doctor requests, with an explicit `mode` retained on the row.
- [ ] Lock the relevant availability and appointment rows before inserting a hold.
- [ ] Return a conflict when another active hold or confirmed appointment occupies any part of the requested interval.
- [ ] Reject intervals outside active doctor availability, intervals in the past, and intervals whose end is not after their start.
- [ ] Add a server operation that expires an unpaid hold and releases its interval without touching unrelated registrations.
- [ ] Restrict admin confirmation to `paid_pending_admin` and require a captured payment state.
- [ ] Ensure reject, cancel, and reschedule transitions release the old interval and preserve the event history.
- [ ] Run `go test ./internal/portal/... -run 'Appointment|Hold|Booking' -v`.

**Handoff:** Concurrent requests cannot overlap, and every appointment response contains a truthful state and next action.

### Task 4: Bind payment orders to holds and add refund actions

**Files:**

- Modify: `internal/portal/payments.go`
- Modify: `internal/portal/server.go`
- Modify: `internal/portal/portal_test.go`
- Test: `internal/portal/integration_test.go`

- [ ] Add public-token and family-session order routes that accept an appointment ID, not a browser-supplied amount.
- [ ] Reuse one order for retries while the hold is active. Create a new order after expiry or a terminal refund.
- [ ] Verify the stored order ID, amount, currency, captured state, and appointment ID during checkout verification.
- [ ] Move a verified captured payment to `paid_pending_admin` in the same transaction as the appointment state update.
- [ ] Keep webhook processing authoritative when it arrives before the browser callback.
- [ ] Make duplicate event IDs harmless and prevent late events from overwriting a newer settled state.
- [ ] Add an admin-only refund action that creates an audited gateway refund request.
- [ ] Handle refund webhooks idempotently and expose the refund status to family and admin views.
- [ ] Return an explicit configuration error when payment credentials or the consultation fee are unavailable. Do not expose a fake checkout button.
- [ ] Run `go test ./internal/portal/... -run 'Payment|Refund|Webhook|Order' -v`.

**Handoff:** Payment is necessary for confirmation, and all payment failure and refund states are recoverable and visible.

### Task 5: Implement child access lookup and verification

**Files:**

- Create: `internal/portal/family_access.go`
- Create: `internal/portal/family_access_test.go`
- Modify: `internal/portal/server.go`
- Modify: `internal/portal/guardians.go`

- [ ] Add a lookup endpoint accepting normalized child full name and date of birth.
- [ ] Return a generic response for no match, wrong date, and multiple matches.
- [ ] Add a one-time-code delivery interface with a test adapter. Production wiring must use the registered guardian contact or the private registration token fallback.
- [ ] Hash codes before storage, expire them quickly, cap attempts, and consume them once.
- [ ] Issue the existing family HttpOnly session only after verification succeeds.
- [ ] Allow a verified session to select among its own child registrations without exposing other records.
- [ ] Keep legacy email/password family sessions usable only through the migration path described in the spec.
- [ ] Add tests for wrong child, wrong date, wrong code, expired code, replayed code, two matching children, and cross-registration access.
- [ ] Run `go test ./internal/portal/... -run 'FamilyAccess|Guardian|Ownership' -v`.

**Handoff:** A family can recover access without a universal password, and name plus date of birth never opens files by itself.

### Task 6: Expose the target API contract to the frontend

**Files:**

- Modify: `internal/portal/server.go`
- Modify: `internal/portal/family_home.go`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/lib/portal-types.ts`
- Modify: `web/src/lib/portal-utils.ts`

- [ ] Add typed client functions for public availability, public-token appointment holds, appointment orders, checkout verification, access lookup, access verification, refund states, and appointment decisions.
- [ ] Extend `FamilyRegistration`, `Appointment`, and payment types with hold expiry, mode, refund, and next-action fields.
- [ ] Keep response labels separate from raw state values so staff can inspect the raw state.
- [ ] Ensure all browser writes continue sending the request marker and same-origin headers.
- [ ] Add contract tests for 401, 403, 404, 409, 422, and 503 cases.
- [ ] Run `cd web && pnpm test` after the type changes and `go test ./internal/portal/...` after route changes.

**Handoff:** The frontend has a stable, typed contract and does not need to parse internal database fields.

### Task 7: Build the public registration and scheduling wizard

**Files:**

- Modify: `web/src/components/consultation-form.tsx`
- Create: `web/src/components/appointment-wizard.tsx`
- Create: `web/src/components/payment-summary.tsx`
- Modify: `web/src/lib/api.ts`
- Test: `web/src/components/consultation-form.test.tsx`
- Test: `web/src/components/family-portal.test.tsx`

- [ ] Preserve the existing registration form and reference token behavior.
- [ ] Replace the post-registration receipt-only state with a visible scheduling step.
- [ ] Add specific-doctor and any-doctor mode controls.
- [ ] Fetch only real free intervals for the selected date and optional doctor.
- [ ] Show the selected child, mode, doctor, date, interval, fee, and hold countdown in one summary.
- [ ] Disable checkout when the hold expires, payment is not configured, or the server reports a conflict.
- [ ] On checkout dismissal or failure, show retry and keep the registration without claiming a booking.
- [ ] On verified payment, show `Payment received` and `Awaiting staff confirmation` until the API says otherwise.
- [ ] Add tests for initial registration, both scheduling modes, hold expiry, failed checkout, callback verification failure, and webhook-discovered success.
- [ ] Run `cd web && pnpm test -- consultation-form family-portal`.

**Handoff:** A new family can complete registration, select a real slot, pay, and understand exactly what remains before confirmation.

### Task 8: Replace family access and dashboard screens

**Files:**

- Create: `web/src/components/family-access.tsx`
- Modify: `web/src/components/family-portal.tsx`
- Modify: `web/src/app/family/login/page.tsx`
- Modify: `web/src/app/family/register/page.tsx`
- Modify: `web/src/app/family/page.tsx`
- Test: `web/src/components/family-portal.test.tsx`

- [ ] Replace the email/password-first family page with child full name and date of birth lookup.
- [ ] Add the one-time-code or private-token verification step.
- [ ] Keep registration creation and attach-by-token as explicit, separate actions.
- [ ] Add a child selector for families with multiple verified registrations.
- [ ] Render appointment timeline, payment state, refund state, next action, and approved book releases.
- [ ] Show `Pay now`, `Choose a time`, `Awaiting confirmation`, `Confirmed`, `Retry payment`, and `Contact staff` actions only when allowed by the server state.
- [ ] Deny pending and rejected book downloads in the UI as well as at the API.
- [ ] Add tests for access verification, multiple children, cross-child denial, pending books, approved downloads, and expired sessions.
- [ ] Run `cd web && pnpm test -- family-portal`.

**Handoff:** Returning families see only their verified child records and always have one clear next action.

### Task 9: Redesign public and staff authentication surfaces

**Files:**

- Modify: `web/src/app/login/page.tsx`
- Modify: `web/src/components/staff-login.tsx`
- Modify: `web/src/app/landing.css`
- Modify: family access styles or the shared UI components as needed

- [ ] Replace the dark centered family card with a light, branded access card and clear two-step indicator.
- [ ] Add a polished staff sign-in layout that still communicates that staff credentials come from an administrator.
- [ ] Reuse the existing MadamGY logo and palette rather than introducing a second visual system.
- [ ] Add visible focus, loading, error, success, and recovery states.
- [ ] Verify mobile layout at 360 px and tablet layout at 768 px.
- [ ] Add component assertions for labels, accessible names, keyboard focus, and non-disclosing errors.
- [ ] Run `cd web && pnpm lint && pnpm test`.

**Handoff:** The screenshots' login problems are resolved without changing staff authorization behavior.

### Task 10: Add the admin payment and appointment queue

**Files:**

- Modify: `web/src/components/admin-operations.tsx`
- Modify: `web/src/components/registration-workspace.tsx`
- Modify: `web/src/components/admin-workspace.tsx`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/lib/portal-types.ts`

- [ ] Add queue filters for awaiting payment, paid pending confirmation, confirmed, failed, expired, cancelled, refund required, and unassigned.
- [ ] Show child, age, guardian contact, requested mode, requested time, assigned doctor, payment, refund, and next action in each row.
- [ ] Add a detail view with Registration, Appointment, Payment, and Books tabs.
- [ ] Require explicit confirmation before rejecting, cancelling, rescheduling, or starting a refund.
- [ ] Disable confirmation for unpaid or expired appointments.
- [ ] Display webhook events and refund identifiers without exposing secrets.
- [ ] Keep doctor views limited to assigned registrations and their own availability.
- [ ] Add frontend tests for filters, status badges, confirmation guards, and refund-required state.
- [ ] Run `cd web && pnpm test && pnpm lint`.

**Handoff:** Admin can operate the entire lifecycle from one queue and can explain every family-visible state.

### Task 11: Add browser E2E coverage and hosted smoke checks

**Files:**

- Create: `web/playwright.config.ts`
- Create: `web/e2e/family-consultation.spec.ts`
- Modify: `web/package.json`
- Modify: `web/pnpm-lock.yaml`
- Test: `internal/portal/integration_test.go`

- [ ] Add a browser test command that targets a local or staging origin and never production data by default.
- [ ] Seed two doctors, one inactive doctor, availability blocks, a child, an approved book, and a pending book release.
- [ ] Cover the golden path from registration through specific-doctor payment, admin confirmation, family verification, and book download.
- [ ] Cover any-doctor scheduling, duplicate slot race, hold expiry, failed payment, duplicate webhook, invalid signature, refund-required, and refund-complete paths.
- [ ] Cover unknown child, wrong date, wrong code, cross-child URL access, inactive doctor, and expired session.
- [ ] Capture screenshots at 360 px, 768 px, and 1440 px for the public form, family access, scheduling, checkout summary, and admin queue.
- [ ] Run the complete browser suite against the gateway test account with a signed webhook replay.
- [ ] Run hosted smoke checks for `/healthz`, public home, family access, webhook method handling, and unsigned webhook rejection.

**Handoff:** The end-to-end suite proves the intended flow against real HTTP boundaries and the gateway test environment.

### Task 12: Provision staging access and update operational documentation

**Files:**

- Modify: `docs/consultation-portal.md`
- Modify: `.env.example` if the new access-factor settings require it
- Modify: `README.md` only if the deployment entry point is currently missing

- [ ] Document the staff bootstrap command and state that production passwords are never committed or printed in this plan.
- [ ] Document staging admin, doctor, and family fixture creation through the secret manager.
- [ ] Document the gateway test webhook, fee setting, refund workflow, and required event types.
- [ ] Document the access-factor delivery adapter and the production behavior when it is unavailable.
- [ ] Document `IMPORT_ON_START` cleanup after the first successful deployment.
- [ ] Add a troubleshooting section for unpaid holds, webhook replay, stale sessions, and expired access codes.
- [ ] Run a documentation diff check and ensure no secret, token, or password appears in tracked files.

**Handoff:** Another operator can configure staging and run the complete test plan without guessing credentials or state transitions.

## Integration gates

Run these in order after all tasks are merged into the implementation branch:

1. `go test ./internal/db/... ./internal/portal/...`
2. `go test ./...`
3. `go build ./...`
4. `go vet ./...`
5. `cd web && pnpm test`
6. `cd web && pnpm lint`
7. `cd web && pnpm build`
8. `cd web && pnpm exec playwright test`
9. Run the hosted test-mode payment and signed webhook scenario.
10. Inspect and attach the real application screenshots before any visible-code push or pull request update.

The implementation is complete only when the golden path, every failure path, privacy checks, concurrency checks, visual checks, and hosted smoke checks pass. A missing gateway, access-factor adapter, database, or staging credential is a blocking deployment dependency and must be reported as such.

## Implementation record, 2026-10-07

The shipped implementation uses private-token verification as the access factor, an appointment-bound order, server-verified capture, expiring holds, admin confirmation and an idempotent full-balance refund action. A shared picker handles both public and family scheduling. The family screen lists verified children; token sessions are restricted to one registration. The queue includes payment filters, guardian contact and payment/refund references.

The operational view uses the existing registration workspace plus expandable payment references rather than adding another tabbed detail screen. SMS delivery and a provider-hosted checkout test are not represented by the local fixture. The historical task checklist above is the original design breakdown, not test evidence. Current verification and remaining hosted checks are recorded in `GATES.md` and `docs/consultation-portal.md`.
