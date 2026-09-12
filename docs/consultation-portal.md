# Consultation portal

The public homepage collects a guardian's name, a child's name and date of birth, a phone number, and optional email. It saves the request before offering optional payment for a doctor consultation. Payment does not reserve an appointment time or restrict book generation.

The administrator workspace at `/admin` shows contact details, birth date, calculated age, assignment, consultation progress, internal notes and payment status. Administrators create, rename, deactivate and reset passwords for doctor accounts. Deactivating a doctor revokes their sessions and returns their children to the unassigned queue. Doctors use `/doctor` to see assigned children and open their book forms. Any signed-in doctor can also generate a book from new inline details at `/books`.

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
2. In the Razorpay dashboard, configure automatic capture and a webhook for `payment.captured`, `payment.authorized`, `payment.failed`, `order.paid` and `refund.processed` at `https://YOUR_API_HOST/api/payments/webhook`.
3. Use the same webhook secret as the server environment.
4. In Administration, open Consultation fee. Enter the fee in INR and enable optional online payment.
5. Verify a test-mode payment, cancellation, duplicate notification and refund before using live keys.

Fees are stored as integer paise. The server creates the order and keeps its amount and currency, even if the fee setting later changes. Concurrent checkout requests for one registration reuse a single order. A browser callback alone cannot mark a request paid: the server checks its signature and fetches the payment from Razorpay, checking the stored order, amount, currency and captured state. Authorized payments remain distinct from captured payments. Follow Razorpay's [Standard Checkout integration](https://razorpay.com/docs/payments/payment-gateway/web-integration/standard/integration-steps/?preferred-country=IN).

Webhooks are checked against their raw body before parsing, as required by [Razorpay's webhook validation guide](https://razorpay.com/docs/webhooks/validate-test/?locale=en-US). Event IDs are recorded transactionally to handle duplicates. Delayed failures and older refund amounts cannot overwrite a newer settled state. Refunds initiated in Razorpay appear in the app when their verified notifications arrive. The portal does not initiate refunds or manually override payment status.

## Data and access boundaries

Migration `0041` creates the private staff, session, registration, setting, order and event tables. The schema is not exposed to Supabase's browser Data API, privileges are revoked from browser roles, and row-level security is enabled. Existing child-profile tables are also protected from direct browser access. Go checks staff roles and assignments before returning patient data.

Every registration gets a separate child profile with the supplied name and birth date. Guardian identity stays on the registration; it is never inferred to be the mother's identity. Clinical fields remain absent until a staff member supplies them. The landing form does not create clinical advice or invented records. Inline book generation keeps its existing transient behavior; registration notes are stored separately from book inputs.

Displayed age is derived from the guardian-supplied birth date, relative to the current date in Asia/Kolkata: `completed_months = 12 * (current_year - birth_year) + current_month - birth_month - (current_day < birth_day ? 1 : 0)`. Displayed years are `floor(completed_months / 12)` and remaining months are `completed_months % 12`. The birth date stays visible to staff. Age is never stored. The existing book engine retains its own documented age calculation.

A ten-digit Indian mobile number is normalized with `+91`. Other numbers must include their country code. An omitted email stays null. Public payment-status reads require a private registration token and return only a registration identifier and payment state. The current browser tab may store that token for retries; it does not store the submitted names or contact details. Request logs exclude search query strings.

Public intake, login and checkout have bounded in-process rate limits. The server intentionally does not trust arbitrary forwarded IP headers. Configure rate limiting at the deployment edge using its verified client address as well, especially when several API replicas or a shared reverse proxy are used.

## Verification

`go test ./...`, `go build ./...`, `go vet ./...`, and `pnpm test`, `pnpm lint`, `pnpm build` inside `web` cover the implementation. Set `PORTAL_TEST_DATABASE_URL` to a disposable local Postgres database to include registration, assignment, session, RLS, duplicate-order and payment-state integration tests. `TEST_DATABASE_URL` separately enables the existing corpus integration suite. Provider-client tests use local mock HTTP servers; they do not establish that supplied hosted credentials work.

The local preview was also checked with official Supabase Auth v2.196.0 and a loopback TLS gateway. Administrator provisioning, doctor creation, both password logins, logout and role enforcement passed against that service. Both accounts signed in through the shared browser's actual form, and the administrator's Doctors tab displayed their active records. Preview passwords and service keys are outside version control. Hosted Supabase and Razorpay checkout still require verification with the project's credentials.

English is the current public and staff interface. A book-language toggle remains outside this change.
