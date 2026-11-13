# Consultation portal

The public homepage collects a guardian's name, a child's name and date of birth, a phone number, optional email, and optional additional details for the doctor (up to 4,000 characters). It saves the request, offers available doctor intervals, places a short hold on the selected interval, and opens payment. An appointment is not sent to the administrator for confirmation until the server verifies captured payment.

Families open `/family/login` with the guardian’s full name, child’s full name and date of birth. These fields are always visible. The original registration tab supplies its saved private credential automatically; name and birth date alone never open records. Missing saved access is explained on the page. New-device access requires a single-use private link issued by an administrator after verifying the guardian. The link is consumed only when the family submits guardian and child details, never by opening or previewing it. The resulting session shows only that registration, its payment and appointment states, and its administrator-approved books.

The administrator workspace at `/admin` shows contact details, birth date, calculated age, assignment, consultation progress, internal notes and payment status. Administrators create, rename, deactivate and reset passwords for doctor accounts, manage every availability block, decide booking requests and approve or reject stored book PDFs. Deactivating a doctor revokes their sessions and returns their children to the unassigned queue. Doctors use `/doctor` to see assigned children, publish and revoke their own availability, and open their book forms. Any signed-in doctor can also generate a book from new inline details at `/books`; only a generation tied to a stored registration creates a family-visible release, and that release remains hidden until an administrator approves the exact PDF bytes.

The existing engine lives at `/console`. Its API, reference pages and book routes require staff authentication. Stored child profiles and match suggestions are limited to assigned doctors; administrators can access all records. Audit and import-history endpoints are administrator-only.

## Configuration

Set these variables in the Go server environment. `.env.example` lists them; the existing server reads exported environment variables.

| Variable | Purpose |
| --- | --- |
| `DATABASE_URL` | The Supabase Postgres transaction-pooler connection, port 6543. Use the database account that runs this application's migrations. |
| `SUPABASE_URL` | HTTPS project origin, such as `https://PROJECT.supabase.co`. |
| `SUPABASE_SECRET_KEY` | Server-only Supabase secret key or legacy service-role key. Never use a public frontend variable. |
| `APP_ORIGIN` | Exact public website origin. Defaults to `http://localhost:3000`; HTTPS enables Secure cookies. |
| `RAZORPAY_KEY_ID` | Razorpay key ID. Start with a test-mode key. |
| `RAZORPAY_KEY_SECRET` | Matching server-only payment key secret. |
| `RAZORPAY_WEBHOOK_SECRET` | Secret used to verify the configured webhook endpoint. |

Set `API_PROXY_URL` in the Next.js environment to the Go API origin. Browser requests use `/api` on the website's own origin so cookies work across the frontend and API. The existing `NEXT_PUBLIC_API_URL` remains a server-side configuration fallback for deployments using that name. Proxy and hosting timeouts must permit the existing long-running book requests; the Next server proxy uses 21 minutes.

With no Supabase credentials, staff sign-in reports that it is not configured. It never bypasses authentication. Registration storage can still run against a configured Postgres database. With no Razorpay credentials or no enabled consultation fee, checkout stays unavailable while intake remains available.

Normal queries use the transaction pooler with prepared statements disabled. Startup migrations automatically use port 5432 on the same hosted `*.pooler.supabase.com` host and credentials, since migration advisory locks require a session. This remains an IPv4-compatible pooler connection, as described in Supabase's [connection guide](https://supabase.com/docs/guides/database/connecting-to-postgres). Other database hosts keep their configured port.

## First administrator

Create the first administrator from a trusted terminal after exporting the server environment. Use an email that has not already been registered in Supabase Auth. Passwords are read from standard input and are never command-line flags.

```fish
read --silent --prompt-str 'Initial administrator password: ' portal_admin_password
printf '%s\n' "$portal_admin_password" | go run ./cmd/staff-admin --email 'admin@example.com' --name 'Administrator name'
set --erase portal_admin_password
```

Then sign in at `/login`. The administrator can create doctor accounts from the Doctors tab. No invitation email is sent; share initial passwords through the team's existing private channel. Staff can change their own password at `/account`. Administrator recovery uses the trusted database/identity administration environment; the public site has no administrator signup route.

Supabase validates staff passwords through its [Auth API](https://github.com/supabase/auth/blob/master/README.md). The app then issues one random, HttpOnly session cookie. Only its SHA-256 hash is stored in `app_private.staff_session`, with a 12-hour expiry. App sessions are checked against current staff activity and role on every request. Logout and password resets revoke these app sessions. Managing credentials directly in the Supabase dashboard does not replace the app's session revocation controls.

## Payment setup

1. Set the three Razorpay variables and restart the Go server.
2. In the Razorpay dashboard, configure automatic capture and a webhook for `payment.captured`, `payment.authorized`, `payment.failed`, `order.paid` and `refund.processed` at `https://YOUR_API_HOST/api/payments/webhook`. For wallet protection, also subscribe to `payment.dispute.created`, `payment.dispute.under_review`, `payment.dispute.action_required`, `payment.dispute.won`, `payment.dispute.lost` and `payment.dispute.closed`.
3. Use the same webhook secret as the server environment.
4. In Administration, open Consultation fee. Enter the fee in INR and enable online payment.
5. Verify a test-mode payment, cancellation, duplicate notification and refund before using live keys.

Fees are stored as integer paise. The server creates the order and keeps its amount and currency, even if the fee setting later changes. Concurrent checkout requests for the same appointment reuse a single order. Rebooking creates a new appointment and order; a previously captured or refunded order cannot fund it. A browser callback alone cannot mark a request paid: the server checks its signature and fetches the payment from Razorpay, checking the stored order, amount, currency and captured state. Authorized payments remain distinct from captured payments. Follow Razorpay's [Standard Checkout integration](https://razorpay.com/docs/payments/payment-gateway/web-integration/standard/integration-steps/?preferred-country=IN).

Webhooks are checked against their raw body before parsing, as required by [Razorpay's webhook validation guide](https://razorpay.com/docs/webhooks/validate-test/?locale=en-US). Event IDs are recorded transactionally to handle duplicates. Delayed failures and older refund amounts cannot overwrite a newer settled state. Refunds initiated in Razorpay appear in the app when their verified notifications arrive. Administrators can initiate a persisted, idempotent refund request from the booking queue. Only verified provider state completes it; the portal does not manually override payment status.

Families pay immediately after choosing a time from either the public intake receipt or the family portal. The browser never supplies the amount. A missing fee, disabled checkout or unpaid hold remains visible as its real state, and an unpaid hold expires after 15 minutes.

## Availability and booking

Doctors publish half-open availability ranges from `/doctor`, using Asia/Kolkata in the browser and `timestamptz` in Postgres. Administrators can create, edit and revoke any active doctor's blocks. The database exclusion constraint rejects overlapping active blocks.

Each consultation can last at most 30 minutes. Selecting availability defaults to 30 minutes or the remaining free time when shorter. Changing the start time updates that end limit. Public, family and administrator booking routes, including confirmation and rescheduling, reject longer intervals on the server. Doctor availability blocks can still span a full working day. Existing appointments are not automatically shortened.

Family booking requests begin as `awaiting_payment` with a 15-minute hold. A captured payment advances them to `paid_pending_admin`; only then can an administrator confirm. A time-range request selects an eligible free doctor by fewest confirmed appointments in that India-calendar week, then staff identifier. A specific-doctor request only considers that doctor's free interval. Active requests hold their interval, and a second overlapping request returns no available slot. Confirmation assigns the doctor and changes the registration to `scheduled`; rejection and cancellation release the interval. A payment that arrives after an expired hold is marked `refund_required` for staff follow-up.

## Data and access boundaries

Migrations `0041` through `0044` create the private staff, family, session, registration, setting, order, event, availability, appointment and book-release tables. Migration `0043` adds expiring holds, payment-linked appointment states and the order-to-appointment link. Family accounts use the existing server-side identity provider but have their own account and session tables. The schema is not exposed to Supabase's browser Data API, privileges are revoked from browser roles, and row-level security is enabled. Existing child-profile tables are also protected from direct browser access. Go checks staff roles, family ownership and doctor assignments before returning patient data. Family registration and approved-book queries apply ownership in SQL.

Every registration gets a separate child profile with the supplied name and birth date. Guardian identity stays on the registration; it is never inferred to be the mother's identity. Clinical fields remain absent until a staff member supplies them. The landing form does not create clinical advice or invented records. Inline book generation keeps its existing transient behavior; registration notes are stored separately from book inputs.

Displayed age is derived from the guardian-supplied birth date, relative to the current date in Asia/Kolkata: `completed_months = 12 * (current_year - birth_year) + current_month - birth_month - (current_day < birth_day ? 1 : 0)`. Displayed years are `floor(completed_months / 12)` and remaining months are `completed_months % 12`. The birth date stays visible to staff. Age is never stored. The existing book engine retains its own documented age calculation.

A ten-digit Indian mobile number is normalized with `+91`. Other numbers must include their country code. An omitted email stays null. Public payment-status reads require a private registration token and return only a registration identifier and payment state. The current browser tab may store that token for retries; it does not store the submitted names or contact details. Request logs exclude search query strings.

The portal does not enforce application-level request quotas for public intake, checkout, family access or recovery, recovery-link issuance, or staff login. Per-address and per-email counters and the shared counter-capacity limit have been removed. Authentication, private registration credentials, one-use recovery grants, ownership checks, request-origin checks, input validation, and body-size limits remain enforced. Hosting, reverse proxies, identity providers, and payment providers can impose independent limits; an upstream HTTP 429 is not removed by this change. Diagnose a remaining 429 from its response and provider logs rather than assuming the portal produced it.

## Verification

`go test ./...`, `go build ./...`, `go vet ./...`, and `pnpm test`, `pnpm lint`, `pnpm exec next build --webpack` inside `web` cover the implementation. Set `PORTAL_TEST_DATABASE_URL` to a disposable local Postgres database to include family isolation, role boundaries, availability and appointment exclusion constraints, doctor selection, confirmation assignment, release approval, duplicate-order and payment-state integration tests. `TEST_DATABASE_URL` separately enables the existing corpus integration suite. Provider-client tests use local mock HTTP servers; they do not establish that supplied hosted credentials work.

The local preview was also checked with official Supabase Auth v2.196.0 and a loopback TLS gateway. Administrator provisioning, doctor creation, both password logins, logout and role enforcement passed against that service. Both accounts signed in through the shared browser's actual form, and the administrator's Doctors tab displayed their active records. Preview passwords and service keys are outside version control. Hosted Supabase and Razorpay checkout still require verification with the project's credentials.

English is the current public and staff interface. A book-language toggle remains outside this change.

## Paid booking and private child access

New appointments hold the requested interval for up to 15 minutes, capped at the appointment start. Expired holds release the interval. Staff can confirm only an appointment whose own order is fully captured. Authorization, failed payment and partial or full refunds cannot confirm a booking.

Capture after expiry, rejection or cancellation leaves that appointment in `refund_required`. A captured cancellation or doctor deactivation also requires a refund. In the booking queue, choose **Refund payment**, then confirm the action. The server persists the amount and retry key before submitting to Razorpay. Retries retain the same key and body. A processed refund, verified from the provider, changes the appointment to `refunded`. [Razorpay refund retry contract](https://github.com/razorpay/markdown-docs/blob/master/api/refunds/normal-refunds-idempotent.md).

Family email/password sign-in and signup are retired: valid requests to their existing API endpoints return HTTP 410. `/family/register` redirects to `/family/login`. Existing family accounts, ownership links and records remain intact. Existing full-account sessions continue until their normal 12-hour expiry or logout; new child sessions are restricted to one registration, including when the same account owns siblings. Staff authentication is unchanged.

An administrator selects the registration under **Child access recovery**, verifies the guardian independently of the child’s name/DOB, and explicitly confirms verification before creating a link. Each 256-bit random link expires in 15 minutes, is stored only as a hash, and replaces any unconsumed link for that registration. The secret travels in a URL fragment, is removed from the address bar, and is submitted only with the child’s details. Successful atomic redemption replaces that registration’s saved credential and revokes its prior child-scoped sessions; full-account sessions and other registrations remain unchanged. No message is sent automatically. Share the link privately only with the verified guardian. If a response is lost after redemption, request another link. Browser storage restrictions can prevent access surviving a page reload; the current sign-in can still establish its secure session. Migration `0046` adds the private grant table; rollback discards outstanding grants only.

Booking and family birth-date fields use `DD/MM/YYYY` with a calendar picker. Eight digits such as `08102026` are accepted and displayed as `08/10/2026`. Invalid calendar dates, future birth dates and past booking dates are rejected. Requests still send ISO `YYYY-MM-DD` dates to the server.

Scheduling and payment transitions share a transaction-level PostgreSQL advisory lock because expiry can release another registration's slot. This deliberately serializes the small clinic's booking writes. Database exclusion and uniqueness constraints also reject overlapping holds. At substantially greater booking volume, replace the global lock with ordered per-doctor locks and a separate expiry worker.

Migration 0043 removes the one-order-per-registration restriction and preserves existing active appointment/payment associations. Migration 0044 records refund requests. Rollback refuses to discard multiple-order or refund history; restore a verified backup or reconcile that history before rolling back.

## Browser verification

The suite runs the real Next.js frontend, Go HTTP handlers and PostgreSQL. Only external identity and checkout providers are simulated in a test-only Go fixture. It never makes a real charge and does not prove that hosted Razorpay credentials are configured.

Use a disposable local database with all migrations. Start the fixture from the repository root:

```sh
env PORTAL_BROWSER_TEST=1 PORTAL_TEST_DATABASE_URL=postgres://postgres:portal-test@127.0.0.1:55437/browser_test?sslmode=disable GOCACHE=/tmp/recipie-go-cache go test ./internal/portal -run '^TestPortalBrowserServer$' -count=1 -timeout 2h -v
```

Start the frontend from `web/` in a second terminal:

```sh
env API_PROXY_URL=http://127.0.0.1:8807 pnpm exec next dev --webpack --hostname 127.0.0.1 --port 3307
```

Then run from `web/`:

```sh
pnpm exec playwright install chromium
pnpm test:e2e
```

The loopback fixture's `/__test/*` routes exist only in a Go test binary. They reset test records, expose disposable fixture access and simulate provider responses. Never point this fixture at a shared or production database. It is not part of the production server.

The browser suite verifies registration, specific-doctor scheduling, captured payment, administrator confirmation, child access, approved-only download, any-doctor scheduling and cancellation. PostgreSQL integration tests additionally cover slot races, expiry, late capture, invalid signatures, webhook replay, refunds, rebooking, sibling isolation, expired sessions and migration rollback/upgrade. Run those with `PORTAL_TEST_DATABASE_URL` set; without it they skip.

Screenshots and a recording from the running application are in [consultation-flow evidence](evidence/consultation-flow/). All shown names, payments and files are disposable fixtures. Hosted payment settlement still requires a controlled Razorpay test-account transaction.


## Meeting links and doctor wallet

[Consultation wallet operations](consultation-wallet.md) documents migration 0047, per-appointment meeting access, new-only earnings, encrypted manual withdrawals, policy gates and release sequencing. No public meeting page or automatic payout is introduced. Guardian names are an additional match requirement, never a replacement for the private registration credential.

The new family form always requires guardian name. For the API-first rollout only, an explicitly configured, expiring omission-only compatibility mode supports old tabs without weakening private-credential or recovery-grant checks. Strict mode is the default; see the staged release sequence in [consultation-wallet.md](consultation-wallet.md#deployment-and-verification).
