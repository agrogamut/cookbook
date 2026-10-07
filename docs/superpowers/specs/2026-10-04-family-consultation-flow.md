# MadamGY family consultation flow

**Status:** Proposed implementation specification
**Date:** 2026-10-04
**Audience:** Product, design, frontend, backend, operations, and QA

## 1. Outcome

A family should be able to complete one continuous journey:

1. Register a child.
2. Choose a named doctor and a real available time, or request the same time from any available doctor.
3. Pay for the consultation in the same flow.
4. See a booking that is clearly paid, pending staff confirmation, confirmed, cancelled, or refunded.
5. Sign back in using the child's identity and a second verification step.
6. See appointment details, payment state, and approved books for that child.

Staff should see the same registration, appointment, payment, guardian, and book-release record in one admin queue. No state should be implied by a button label or by a browser callback.

## 2. Current state and gaps

The repository already has most of the server primitives:

- The public form at `/` collects guardian name, child name, date of birth, phone, and optional email.
- The public form creates a registration before payment. Payment is currently optional and does not reserve a slot.
- Family accounts currently use email and password at `/family/login` and `/family/register`.
- The family portal can list registrations, show released books, list availability, request an appointment, and open checkout.
- Family appointment requests currently enter `pending_admin` whether or not payment has completed.
- The admin API can list registrations, appointments, availability, staff, payments, and book releases.
- Staff credentials are provisioned by a trusted command. There are no default production credentials in the repository.

The supplied screenshots expose two product problems:

- The sign-in card looks like a generic dark form placed on a large empty pink canvas. It does not establish which child or family record will open.
- The registered state says that payment is unavailable and offers no direct appointment selection, so a family cannot complete the intended journey.

The new flow must connect these existing capabilities rather than create a second, competing registration system.

## 3. Product decisions

### 3.1 Payment is required for a confirmed appointment

An appointment is never `confirmed` until the server has verified a captured payment for the appointment's order. A browser callback alone cannot change the appointment state.

If payment is unavailable, registration remains possible, but the UI must say that no appointment has been reserved. The family cannot see a success state that looks like a booking.

### 3.2 Two scheduling modes

The family chooses one of these modes:

| Mode | Family chooses | Server behavior | Family sees |
| --- | --- | --- | --- |
| Specific doctor | Doctor, date, and one available interval | Holds that exact doctor and interval | Doctor name and exact time |
| Any available doctor | Date and a start/end time window | Chooses an eligible active doctor using the existing load-balancing rule | Exact requested window and the assigned doctor once selected |

Both modes use the same appointment record, payment order, collision checks, and admin queue. The mode is retained for reporting and rescheduling.

### 3.3 A short slot hold protects checkout

Selecting an interval creates a server-side hold for 15 minutes. The hold prevents another family from taking the interval while checkout is open.

- A hold has `awaiting_payment` status and an expiry timestamp.
- A successful payment changes it to `paid_pending_admin`.
- A failed, dismissed, or expired checkout releases the interval.
- A hold is never shown as a confirmed appointment.
- A scheduled job is not required for correctness. Reads and writes must treat expired holds as free, and a periodic cleanup may remove old rows.

### 3.4 Staff confirmation remains the final scheduling decision

Payment proves that the consultation fee was captured. It does not silently assign a doctor or bypass staff review. Admin confirmation changes `paid_pending_admin` to `confirmed` and the registration to `scheduled`.

If staff rejects a paid request, the admin queue creates a visible `refund_required` task. The refund action must be explicit, audited, and reflected in the family portal after the gateway webhook arrives.

### 3.5 Child identity is the family entry point, with a second factor

The family-facing sign-in form asks for:

- Child's full name.
- Child's date of birth.

Those two values are a lookup key, not sufficient proof for private medical files. After a match, the family must complete one of these second steps:

- A one-time code sent to the guardian phone stored on the registration, or
- The private registration access token printed on the registration receipt.

The production UI must never grant file access from name and date of birth alone. If several records match, the service must not disclose whether a particular child exists. The response should be the same for an unknown child, an incorrect date, and an incorrect second factor.

The current email/password account may remain as a legacy migration path during rollout, but it should not be the primary family experience after this change.

## 4. Roles and permissions

| Role | Can do | Cannot do |
| --- | --- | --- |
| Family | Create a registration, choose a slot, pay, view own appointment and payment state, download approved books for the matched child | View another child, assign a doctor, approve a book, alter a payment amount |
| Doctor | View assigned registrations, publish availability, review assigned cases, generate books for assigned children | View unassigned children, confirm payment, approve a release, change gateway settings |
| Administrator | View every registration, assign doctors, manage availability, confirm, reject, cancel, reschedule, issue a refund action, approve or reject exact book bytes, manage staff and fee settings | Read a secret payment key in the browser |

Every protected API query must enforce ownership or staff role in SQL-backed server code. The frontend is not an authorization boundary.

## 5. Family journey

### 5.1 Entry and registration

The public home page keeps one primary action: **Book a consultation**.

The first screen collects:

- Guardian full name.
- Child full name.
- Child date of birth.
- Guardian phone number.
- Optional email.

The form shows the child's derived age and a short privacy statement. It saves the registration before checkout so a failed payment never loses the intake.

After save, the family receives a reference and an access method. The reference is safe to display. The private token is shown once and can be used as the second factor if the phone code is unavailable.

### 5.2 Doctor and time selection

The next step is a full-width scheduling panel, not a hidden link on a receipt.

1. Choose **A specific doctor** or **Any available doctor**.
2. Choose a future date in Asia/Kolkata.
3. Show only real free intervals returned by the API.
4. Require the family to select one interval whose end is after its start.
5. Show a summary card with child, doctor mode, date, time, duration, and consultation fee.
6. Create the 15-minute server-side hold.

For a specific doctor, the doctor list must show only active doctors with availability on the selected date. For any available doctor, the family must not be asked to choose a doctor name. The summary should say “Any available doctor” until the server assigns one.

### 5.3 Payment

The payment step appears in the same page after a hold is created.

- Display the fee in INR, the child name, the selected appointment window, and the hold expiry countdown.
- Open the gateway checkout with the server-created order ID and amount.
- Never accept the amount or currency from the browser as authoritative.
- On success, send the signed checkout result to the server, then poll the registration until the server reports `paid_pending_admin` or a terminal failure.
- On gateway failure or dismissal, keep the registration, release the hold, show a retry action, and state that no appointment is confirmed.
- On webhook-only completion, the next status refresh must show the same paid state as a browser callback.

The payment button must be disabled when the fee is missing, the gateway is not configured, the hold has expired, or the server reports that the slot was taken.

### 5.4 Confirmation and follow-up

After verified payment, the family sees:

- `Payment received`.
- `Awaiting staff confirmation`.
- The requested doctor mode and time window.
- A clear statement that the appointment is not final until staff confirmation.

After admin confirmation, the family sees:

- `Appointment confirmed`.
- Doctor name.
- Start and end time in Asia/Kolkata.
- Payment status and receipt reference.
- A cancel or reschedule action only when policy permits it.

### 5.5 Books and child files

The family dashboard has one child switcher when an account owns multiple registrations. Each child card shows only that child's:

- Registration state.
- Appointment state.
- Payment state.
- Approved Book 1 and Book 2 releases.

Pending or rejected book releases must not be downloadable. A download endpoint must re-check family ownership and `approved` status on every request.

## 6. Family access design

### 6.1 Login screens

Replace the current generic email/password card with a two-step family access page:

1. **Find your child's record:** full name and date of birth.
2. **Verify access:** one-time code to the registered guardian phone, or private registration token.

The page must include a link to register a new child, a recovery path that tells the family to contact MadamGY staff, and a small explanation that the details are used to find the child's consultation record.

The server should issue a short-lived, HttpOnly family session after verification. Do not put child names, dates of birth, tokens, or phone numbers in query strings or analytics events.

### 6.2 Family account migration

Existing email/password family accounts need a migration path:

- On the next successful email/password login, require child selection and second-factor verification before showing files.
- Allow the family to add a verified phone to the child record.
- Keep existing sessions valid only until their current expiry, then require the new flow.
- Do not silently merge records by email address.

### 6.3 Rate limiting and privacy

- Rate-limit lookup and verification attempts by IP, normalized child lookup, and device session.
- Return the same user-facing error for no match, wrong date, and wrong code.
- Do not reveal whether a record has a payment, appointment, or book until verification succeeds.
- Log a redacted access event for successful and failed verification attempts.

## 7. Visual and interaction specification

The public family experience should feel calm and trustworthy while the staff console remains dense and operational.

### 7.1 Shared visual language

- Use the existing MadamGY logo and burgundy, raspberry, and warm cream palette.
- Use one centered content column for authentication and a wider two-column layout for scheduling.
- Replace the black login card with a light card, clear step indicator, stronger hierarchy, and an obvious primary action.
- Use the same button, input, error, success, and status badge styles across public, family, and staff pages.
- Keep focus rings visible and preserve keyboard navigation.
- Use CSS and existing assets. Do not add unverified or generated clinical imagery.

### 7.2 Required screens

| Screen | Required content |
| --- | --- |
| Public home | Consultation value, intake form, visible payment and scheduling explanation, family access link |
| Family access | Child name, date of birth, second-factor step, recovery and registration links |
| Scheduling | Doctor mode, doctor list when relevant, calendar, real intervals, hold expiry, summary |
| Checkout | Fee, appointment summary, gateway button, failure and expiry states |
| Family dashboard | Child cards, appointment timeline, payment state, book releases, next action |
| Staff login | Staff role explanation, email, password, password recovery instruction, return link |
| Admin queue | Filters, counts, registration details, payment state, appointment actions, release actions |

### 7.3 Responsive acceptance

The family flow must be usable at 360 px, 768 px, and 1440 px viewport widths. On mobile, doctor selection, time selection, payment summary, and the next action remain visible without horizontal scrolling.

## 8. State model

### 8.1 Registration status

Keep the existing registration lifecycle, with appointment and payment states shown separately. A registration can be `new`, `contacted`, `scheduled`, `completed`, or `cancelled`.

### 8.2 Appointment status

Add these states to the existing appointment model:

| State | Meaning | Slot held |
| --- | --- | --- |
| `awaiting_payment` | Family selected a valid interval and checkout is open | Yes, until expiry |
| `paid_pending_admin` | Payment is verified, staff has not confirmed | Yes |
| `confirmed` | Admin confirmed the paid appointment | Yes |
| `rejected` | Admin rejected the request | No |
| `expired` | Payment was not completed before the hold expired | No |
| `cancelled` | Family or staff cancelled it | No |
| `refund_required` | A captured payment needs an explicit refund action | No |
| `refunded` | Gateway confirms the refund | No |

The server must reject a transition that skips payment verification. A paid request cannot become `confirmed` from a browser-only callback.

### 8.3 Payment status

Keep gateway states distinct:

`unpaid`, `pending`, `authorized`, `paid`, `failed`, `partially_refunded`, and `refunded`.

The UI label may be friendly, but the raw state remains available to staff. Duplicate webhook event IDs must be idempotent. Late failure or refund events must not overwrite a newer settled state.

## 9. Data and API changes

### 9.1 Database changes

Create a migration that:

- Adds appointment hold and expiry timestamps.
- Links each consultation order to the appointment attempt it funds.
- Allows a new order after an expired or refunded appointment while retaining the audit history.
- Stores the requested doctor mode and the original family-selected window.
- Stores `refund_required` and refund identifiers separately from payment status.
- Adds a verified guardian contact record or an access-factor table for one-time codes.
- Records access attempts without storing raw verification codes.
- Preserves existing family account data for migration.

Use partial unique indexes for one active appointment per registration and one active hold per doctor/time range. Keep the existing exclusion constraint for overlap protection.

### 9.2 Public and family endpoints

The implementation may retain existing paths where their semantics stay compatible. The target contract is:

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/public/registrations` | Create or resume a registration |
| `GET` | `/api/public/availability` | List free intervals for a date and optional doctor |
| `POST` | `/api/family/access/lookup` | Start child name and date-of-birth verification |
| `POST` | `/api/family/access/verify` | Exchange one-time code or private token for a family session |
| `GET` | `/api/family/registrations` | List only verified family's registrations |
| `POST` | `/api/family/appointments/hold` | Create a 15-minute hold |
| `POST` | `/api/family/appointments/{id}/order` | Create or reuse the payment order for the hold |
| `POST` | `/api/family/appointments/{id}/verify` | Verify browser checkout result |
| `POST` | `/api/payments/webhook` | Verify gateway events and update payment state |
| `POST` | `/api/admin/appointments/{id}/decision` | Confirm, reject, cancel, or reschedule |
| `POST` | `/api/admin/payments/{id}/refund` | Start an audited refund action |
| `GET` | `/api/family/book-releases/{id}/download` | Download an approved book for the verified child |

Every mutating browser request must retain the existing origin and request-header protections. The webhook remains signature-verified against its raw request body.

## 10. Admin workspace

The admin home should open on a queue, not a blank form. Required filters:

- Awaiting payment.
- Paid, awaiting confirmation.
- Confirmed and upcoming.
- Failed, expired, cancelled, and refund required.
- Unassigned registrations.

Each row shows child, derived age, guardian contact, registration time, requested mode, requested doctor or any-doctor flag, appointment window, payment state, and next action.

The detail view has four tabs:

1. **Registration:** intake fields, contact details, notes, and access history.
2. **Appointment:** requested and confirmed doctor/time, availability conflicts, confirm/reject/reschedule/cancel controls.
3. **Payment:** order ID, amount, currency, payment ID, gateway state, webhook events, refund action, and audit trail.
4. **Books:** generated bytes, approval status, approver, timestamps, and family visibility.

Admin actions must be explicit and confirmable. A reject or cancel action on a captured payment must show the refund consequence before submission.

## 11. Credentials and environment setup

No production passwords should be written into this specification or committed to the repository.

Current staff provisioning is documented in `docs/consultation-portal.md`. The first administrator is created from a trusted terminal with `cmd/staff-admin`, then creates doctor accounts from the admin workspace. Passwords are supplied through standard input and shared through the team's private channel.

For staging, create these accounts in the secret manager:

| Fixture | Purpose | Required data |
| --- | --- | --- |
| Admin | Full queue, payment, refund, and release approval | Email, password |
| Doctor | Assigned child, availability, and book generation | Email, password |
| Family | Child lookup, second factor, payment, appointment, book download | Child full name, DOB, guardian phone, test access factor |

The test fixture must use a clearly non-production child and a gateway test key. Do not create a hidden universal family login.

Required deployment settings remain on the API service:

- Database connection and Supabase identity settings.
- `RAZORPAY_KEY_ID`.
- `RAZORPAY_KEY_SECRET`.
- `RAZORPAY_WEBHOOK_SECRET`.
- A configured INR consultation fee.
- A verified phone or token delivery adapter for family access.

If the access-factor delivery adapter is not configured, production must keep file access disabled and show a staff contact path.

## 12. End-to-end test plan

### 12.1 Test environment

Use a disposable Postgres database, a test-mode gateway account, a local or staging API, and a seeded fixture set. Keep all test data outside production. The test runner should be Playwright or the project's browser-capable Vitest setup, with API integration tests in Go.

Required test fixtures:

- One active admin.
- Two active doctors with overlapping and non-overlapping availability.
- One inactive doctor.
- One child with an existing registration and approved Book 1.
- One child with a pending book release.
- A configured consultation fee.
- Gateway test keys and a webhook signing secret.

### 12.2 Golden-path tests

| ID | Scenario | Expected result |
| --- | --- | --- |
| E2E-01 | Family enters child and guardian details | Registration is created once, a reference is shown, and no appointment is implied |
| E2E-02 | Family chooses a specific doctor and free interval | The selected doctor and interval appear in the summary and a 15-minute hold is created |
| E2E-03 | Family chooses any doctor and a valid time window | The request stores `time_range`, selects an eligible doctor, and shows the requested window |
| E2E-04 | Family completes a successful test payment | Payment becomes `paid`, appointment becomes `paid_pending_admin`, and the hold remains protected |
| E2E-05 | Admin confirms the paid request | Appointment becomes `confirmed`, registration becomes `scheduled`, and the family sees the final details |
| E2E-06 | Family verifies access with child name, DOB, and second factor | Only that child's registration, appointment, payment, and approved books are visible |
| E2E-07 | Admin approves a generated book | The family sees the release and can download the exact approved bytes |

### 12.3 Payment and failure tests

| ID | Scenario | Expected result |
| --- | --- | --- |
| E2E-08 | Checkout is dismissed | Registration remains, hold is released, and the next action is retry payment |
| E2E-09 | Gateway reports failure | Appointment is not confirmed, payment is `failed`, and the interval can be selected again |
| E2E-10 | Hold expires during checkout | Payment attempt cannot confirm the appointment and the family receives a clear expiry message |
| E2E-11 | Webhook arrives before browser callback | The family status refresh discovers the paid state and does not create a duplicate order |
| E2E-12 | Duplicate captured webhook is replayed | The second event is ignored transactionally and state is unchanged |
| E2E-13 | Invalid webhook signature is sent | Response is rejected, no payment state changes, and the event is not stored |
| E2E-14 | Admin rejects a captured request | Appointment is marked `refund_required`, the admin sees the action, and the family sees pending refund |
| E2E-15 | Refund webhook arrives twice | Payment becomes `refunded` once and the duplicate is harmless |
| E2E-16 | Fee or gateway configuration is missing | Booking confirmation is blocked; registration remains available with a staff-contact message |

### 12.4 Availability and concurrency tests

| ID | Scenario | Expected result |
| --- | --- | --- |
| E2E-17 | Two families select the same specific slot | One hold succeeds and the other receives a conflict without an overlapping row |
| E2E-18 | Two families request any doctor for the same window | Eligible doctors are selected according to the documented load rule; no doctor overlap is created |
| E2E-19 | Doctor availability blocks overlap | The second availability write is rejected with a validation message |
| E2E-20 | Admin reschedules a confirmed appointment | New interval is checked atomically, old interval is released, and the family sees the new time |
| E2E-21 | Doctor is deactivated | Existing access is revoked, future unconfirmed requests are returned to admin, and the family cannot select that doctor |

### 12.5 Access and privacy tests

| ID | Scenario | Expected result |
| --- | --- | --- |
| E2E-22 | Unknown child name and DOB | Response does not reveal whether a record exists |
| E2E-23 | Correct name and DOB, wrong second factor | No session is issued and no child data is returned |
| E2E-24 | Family requests another child's registration ID | Response is 404 or an equivalent non-disclosing denial |
| E2E-25 | Family requests a pending or rejected book download | Download is denied |
| E2E-26 | Doctor requests an unassigned child's profile | Access is denied |
| E2E-27 | Admin deactivates a doctor | Doctor sessions are revoked and the doctor cannot call protected routes |
| E2E-28 | Session expires or is logged out | Protected requests return 401 and the browser returns to the correct login screen |

### 12.6 Visual and accessibility tests

Run the golden path at 360 px, 768 px, and 1440 px. Verify:

- No horizontal scrolling.
- Keyboard-only completion from registration through payment.
- Visible focus on every control.
- Labels are associated with inputs.
- Errors are announced and remain next to the failed step.
- The appointment summary and payment amount remain visible before checkout.
- Loading, success, expired, failed, and refund states are visually distinct.
- Family login does not display a child record before verification.

### 12.7 Verification commands

The implementation is complete only when these checks pass in the implementation branch:

```text
go test ./...
go build ./...
go vet ./...
cd web && pnpm test
cd web && pnpm lint
cd web && pnpm build
```

The hosted smoke check must also verify:

1. `GET /healthz` returns 200.
2. Public home and family access pages return 200.
3. `GET /api/payments/webhook` returns 405 with `Allow: POST`.
4. An unsigned POST to the webhook is rejected without changing payment state.
5. A real test-mode payment and signed webhook complete the golden path.

## 13. Acceptance criteria

The feature is ready when all of the following are true:

- A family can register, choose either scheduling mode, pay, and see a truthful status without leaving the public/family experience.
- No appointment is `confirmed` without verified payment and admin confirmation.
- A failed, dismissed, or expired payment cannot hold a slot indefinitely.
- Admin sees one coherent queue containing registration, appointment, payment, refund, and book-release state.
- Family access requires child full name, date of birth, and a second verification factor before files are visible.
- Family users cannot access another child's registration or book by changing an ID in the URL.
- The login pages are responsive, branded, keyboard usable, and provide clear recovery states.
- Duplicate and out-of-order payment events are safe.
- The complete E2E matrix passes against a disposable database and gateway test account.
- Production credentials are provisioned through the documented staff command and secret manager, with no default password in source control.

## 14. Delivery order

1. Add the appointment hold, payment link, refund state, and access-factor migrations.
2. Implement server transition rules and idempotent payment handling.
3. Replace family access screens and add the scheduling and checkout journey.
4. Redesign staff login and build the admin queue around the new states.
5. Add browser E2E coverage and backend concurrency tests.
6. Run the test-mode payment flow in staging, then switch production keys only after the complete matrix passes.
