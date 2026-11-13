# Consultation meetings and doctor wallet

Migrations 0047 and 0048 extend the existing portal. Admin and doctor workspaces remain at `/admin` and `/doctor`. Families use `/family/login` and their existing portal. No shared appointment page, automatic transfer, messaging or reminder service is added.

## Access and meeting links

Public intake accepts optional guardian-entered details (4,000 characters maximum). These remain separate from staff notes and are labelled guardian-reported, not clinical findings. Only authorized staff receive them; rendering escapes markup.

Family access and single-use recovery require guardian name, child name and DOB plus the existing private credential or recovery grant. Name matching ignores case and repeated spaces. Names and dates alone never authorize access. Staff-verified recovery remains registration-scoped, expires in 15 minutes and consumes atomically on submission, not preview. Family passwords remain retired; existing sessions keep their existing scope until expiry.

Admin and the appointment's assigned doctor can save or clear a full HTTPS meeting URL. Embedded URL credentials are rejected. The server never fetches the link. An authorized signed-in family sees an existing link for current pending or confirmed appointments. Cancellation, expiry, rejection and refund states hide it. Doctor reassignment clears the old link. Book generation entry points have a deep-pink border; light and dark themes remain available. Calls display dates and times in Asia/Kolkata (IST).

## Earnings and balances

Admin sets a fixed integer-paise earning amount at `PUT /api/admin/earnings`, separately from the patient consultation fee. Blank disables earnings for future appointments; zero produces no ledger credit. A new appointment snapshots the assigned doctor and configured amount. Existing appointments receive no snapshot or historical backfill.

Only a currently verified captured payment that matches the stored order, amount and INR currency can earn credit. Authorization and the checkout callback alone cannot. The appointment must still be eligible, not expired or cancelled, and must retain its snapshotted doctor. Attaching an old captured payment does not create new earnings. If the first verified capture is already partially refunded, an otherwise eligible appointment records its original snapshotted earning before entering the refund hold. Admin then decides each actual refund; expired/cancelled holds and historical appointments still earn nothing.

A signed dispute remains actionable even if a newer refund overtakes its fetched payment snapshot. The transaction preserves the newer payment/refund totals while applying the dispute reversal and hold and recording the event once.

The ledger has independent unique identities for appointment earnings, payment credit and withdrawal debit, in addition to webhook event deduplication. Credit, refund, booking and withdrawal writes share the existing transaction advisory lock. This deliberately serializes writes for the current clinic workload; it is not intended for unrestricted payment throughput.

- Balance is the signed ledger total.
- Reserved is the gross total of requested/processing withdrawals.
- Available is balance minus reserved.
- A successful manual paid record debits gross once and removes its reservation in the same transaction.
- Cancellation or rejection releases the reservation without a debit.
- A credited appointment cannot be reassigned to another doctor. Reconcile/refund it and create a new appointment instead.

Pending withdrawals are never truncated by history: requested and processing entries are returned completely, oldest first, followed by at most 500 recent closed entries. This complete queue is bounded by pending workload, not a fixed response size; use cursor pagination if that workload becomes large.

Meeting saves compare the expected doctor and prior URL under the booking lock. Stale edits return 409. A pristine editor follows refreshed data; a conflicting draft is retained but cannot be saved until the user reviews the current link. Reassignment still clears the URL.

## Partial refund decisions

Every processed provider refund has its own immutable ID, amount and time in `app_private.payment_refund`. The API fetches all pages of the current payment's refund list, accepting only processed INR refunds for that payment. Cumulative totals never regress. Missing, inconsistent, overlapping or unavailable provider details retain a reconciliation hold and leave the callback retryable. A bounded pagination failure also requires retry, never a guessed allocation.

At Admin > Doctor withdrawals > Refund reconciliation, admin reviews each refund, appointment, snapshotted doctor, original earning, previous reductions and remaining appointment earning. Admin explicitly chooses an integer-paise deduction from zero through the remaining appointment earning, gives a nonblank explanation (500 characters maximum) and confirms. This is a business allocation, so the bound is the remaining doctor earning, not an assumed ratio of the patient refund. The doctor amount may differ from the patient charge. Zero preserves the earning.

Each decision is permanent through the application and records actor, timestamp, explanation and earning before the decision. Positive deductions append a negative ledger entry; original earnings are never rewritten. A unique refund decision and ledger identity plus the booking transaction lock prevent concurrent/duplicate deductions. Stale remaining-earning values conflict and require refresh. API routes are admin-only:

- `GET /api/admin/refund-reviews`
- `POST /api/admin/refund-reviews/{order_id}/sync`
- `POST /api/admin/refund-reviews/{refund_id}/decision`

Resolving the last known case releases only that partial-refund hold. Other unresolved refunds, missing identities, disputes, outstanding full-refund requests, negative balances and reservations exceeding the remaining balance still block new withdrawals or paid marks. An over-reserved request stays held; admin can explicitly reject it with an audit note to release its reservation. A previously paid doctor can have a negative balance after the adjustment. The app does not recover a bank transfer automatically.

A verified full refund reverses only the remaining appointment earning once, after any prior partial adjustments. Dispute events freeze/reverse remaining earnings. Later capture or dispute-won callbacks do not silently restore earnings. Partial-refund cases can now be completed in-app without database edits. Dispute restoration, changing an immutable decision, or settling a negative balance outside future legitimate earnings still requires a separately designed audited correction process; no general manual-credit control is provided.

Webhook/current-provider verification cannot atomically lock a provider against a simultaneous external refund or manual bank transfer. Verify the wallet immediately before a manual transfer, retain its receipt, and reconcile any event arriving in that interval. Request contents and payout details must stay out of logs.

## Private manual withdrawals

Configure `WALLET_ENCRYPTION_KEY` as a stable, server-only 32-byte key encoded with 64 hex characters. Invalid keys fail startup; an absent key disables saving payout details and requesting withdrawals. Earnings still accrue. Store the key in the deployment secret store and retain it securely with database backups. Replacing it without decrypting/re-encrypting all destinations and immutable request snapshots makes them unreadable. No automatic key rotation is implemented.

A doctor saves account holder plus bank, IFSC and confirmed account number, or account holder plus UPI ID. These fields are encrypted using AES-256-GCM, a fresh nonce and the doctor identity as authenticated context. API lists return masks only. Each request snapshots its encrypted destination; later edits cannot redirect it. An administrator can explicitly reveal a pending request's destination for manual transfer review; that access is audited. Do not place payout data in review notes or logs.

Request amounts range from ₹1 to ₹10,00,000 and cannot exceed available funds. The client supplies a UUID retry key; repeated submissions of the same amount/key reuse the same request. A changed amount with the same key conflicts. Concurrent requests reserve atomically. The estimate is 2 to 4 business days, not a guaranteed settlement deadline.

Admin may mark processing, reject, or record paid. A paid record requires the actual transferred amount, transfer reference, actual time (IST in the form), and explicit confirmation that the transfer is already complete. The check mark records a manual transfer and never initiates one. Matching retries are idempotent; conflicting transfer facts are rejected. A doctor can cancel only while requested, before processing.

New financial tables use the private schema with RLS and no PUBLIC, anon or authenticated privileges. Route middleware enforces staff roles; family sessions cannot read wallets and doctors cannot access another doctor's withdrawals. History shows the latest 200 ledger entries, every pending withdrawal and up to 500 recent closed withdrawals; balance calculations include all rows.

## Owner-selected active 18% calculation

The owner requests 18% added to family fees and an 18% deduction from doctor withdrawals, and states AgroGamut Services Private Limited is GST registered. The local candidate enables that calculation by default for new orders and requests. `owner-18-v1` records the owner's selection, not professional approval.

| Server variable | Empty or `owner-18-v1` | Explicit `inactive` override |
| --- | --- | --- |
| `PORTAL_PATIENT_TAX_POLICY` | New orders snapshot base, 18% GST calculation and actual total. | New orders use the base consultation price with zero added tax. |
| `PORTAL_WITHDRAWAL_DEDUCTION_POLICY` | New requests snapshot the 18% deduction and payable amount. | Requests retain a provisional 18% estimate; final payout is pending and paid recording is blocked. |

Other values fail startup. Retired `PORTAL_PATIENT_TAX_APPROVAL` and `PORTAL_WITHDRAWAL_DEDUCTION_APPROVAL` names are no longer read. They do not disable the active default. The admin screen displays policy state but cannot change it.

Example: gross ₹2,000 (200,000 paise), deduction ₹360 (36,000 paise), final withdrawable ₹1,640 (164,000 paise). Integer half-up rounding: `(gross_paise * 1800 + 5000) / 10000`. The wallet reserves/debits ₹2,000 once. For patient fees, a ₹2,000 base produces a ₹360 addition and ₹2,360 total.

Existing patient orders retain their stored price and policy, including tax-off orders. The family confirms the actual order price before checkout. Old `pending_review` withdrawals remain provisional even after activation: the doctor cancels, or admin rejects, then the doctor submits and confirms a new request. Requests under a different/inactive current policy cannot be marked paid. Completed history is unchanged.

Legal classification remains unverified. GST registration alone does not establish taxable treatment of each actual supply or that a withdrawal deduction is lawful GST. A qualified Indian GST adviser must assess contracts, healthcare exemptions (including CBIC entry 74 and Circular 32/06/2018), invoicing, reporting and any other withholding. The code does not invent adviser approval, a GSTIN, tax invoices, tax credits or compliance certification. This is the requested calculation, not a complete tax accounting system.

## Deployment and verification

This feature has not been deployed by this work. Migrations run on API startup. Migration 0047 adds wallet fields; 0048 adds refund identities/decisions and extends ledger constraints. Their down migrations refuse financial/refund history loss. Roll forward once financial data exists.

Checkout uses versioned routes: public-token and signed-in-family clients POST to `/api/public/registrations/{id}/order/itemized-v1` and `/api/family/registrations/{id}/order/itemized-v1`. The previous `/order` routes return 409 with a refresh instruction before any order lookup/write or provider call. New clients never fall back to those routes. An old API has no versioned route and returns 404, so a new client cannot create an untaxed order there. Returned currency, integer amounts, tax arithmetic and total must agree before the family confirms the actual stored price and checkout opens. Missing price metadata fails closed.

Keep the requested `owner-18-v1` defaults active. Explicit `inactive` overrides remain supported for a deliberate separate operational choice, but they are no longer a prerequisite or substitute for the versioned contract.

1. Deploy the API and migrations 0047/0048 first. Verify its exact serving revision, migration success and health. Drain every old API replica before opening new checkout traffic: an old client talking to an old API can still use the old route and price. Routing across mixed replicas cannot be proven safe by one successful health request. Verification/webhooks remain unchanged for payments already in flight.
2. Deploy the matching frontend. Verify actual itemized confirmation, guardian access/recovery and withdrawal/refund screens. New frontend/old API checkout is unavailable without side effects; old frontend/new API checkout asks for refresh. A legacy unpaid order returned by the new route keeps its stored price, including 0% tax. Do not replace or reprice it. Already-open provider checkout cannot retroactively receive the new application prompt.
3. Guardian mode stays strict by default. New forms require guardian name and the existing private credential or one-use grant; an old API rejects the added field without consuming a grant. Old forms against the strict new API receive a refresh instruction. A brief service interruption is preferable to removing either access check. If uninterrupted legacy access is operationally required, explicitly configure `PORTAL_LEGACY_GUARDIAN_UNTIL` to a short absolute RFC3339 deadline, no more than 24 hours ahead, before API-first deployment. Only omission is allowed during that window; supplied wrong/blank/null names still fail. Clear it after frontend verification. Existing authenticated sessions and staff login remain valid.

The contract guards make either mixed client/server direction fail closed for new checkout, but they cannot change an old server, verify hosting controls, drain replicas or refresh user tabs. An uncoordinated main auto-deploy does not prove full availability or the requested active policy everywhere. Verify both revisions and the routing boundary before declaring rollout complete.

No production compatibility setting was changed by this work. Do not configure earnings until both services are verified; appointments created by an older API have no earning snapshot.

Configure the encryption key before enabling payout destination entry, then set the intended fixed earning amount. Verify both deployed revisions, migration state and health independently. Use authorized synthetic test data for any later acceptance check; local evidence does not establish provider or live deployment status.

Subscribe the existing signed payment webhook to the documented payment/refund events and to `payment.dispute.created`, `payment.dispute.under_review`, `payment.dispute.action_required`, `payment.dispute.won`, `payment.dispute.lost` and `payment.dispute.closed`. Dispute handling verifies the signature and fetches current payment state. An undisclosed/unreceived provider dispute cannot be detected by the app; external notification configuration remains an operational dependency.

Tests use `PORTAL_TEST_DATABASE_URL` for a disposable PostgreSQL database. The wallet suite exercises populated migration upgrade, no backfill, privileges, cross-account authorization, capture replay, competing withdrawals, refunds/disputes, policy transition and manual paid idempotency. `go test ./...`, `go build ./...` and `go vet ./...` remain required. Corpus tests separately need `TEST_DATABASE_URL` and imported provider data.

The browser fixture uses a separate database with synthetic identity and gateway responses only. Run `web/e2e/consultation-wallet.spec.ts` once with default active policy and once with `PORTAL_BROWSER_INACTIVE_POLICY=1` on both fixture and test process. It covers real Next.js/Go/PostgreSQL flows, mobile/light/dark views, price confirmation, in-app refund decisions and the exact requested arithmetic without real transfers. Run the existing family-consultation browser regression too.

No application request quotas are restored. Hosting, proxies, identity and payment providers may still produce independent HTTP 429 responses.
