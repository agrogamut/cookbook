# Consultation meetings and doctor wallet

Migration 0047 extends the existing portal. Admin and doctor workspaces remain at `/admin` and `/doctor`. Families use `/family/login` and their existing portal. No shared appointment page, automatic transfer, messaging or reminder service is added.

## Access and meeting links

Public intake accepts optional guardian-entered details (4,000 characters maximum). These remain separate from staff notes and are labelled guardian-reported, not clinical findings. Only authorized staff receive them; rendering escapes markup.

Family access and single-use recovery require guardian name, child name and DOB plus the existing private credential or recovery grant. Name matching ignores case and repeated spaces. Names and dates alone never authorize access. Staff-verified recovery remains registration-scoped, expires in 15 minutes and consumes atomically on submission, not preview. Family passwords remain retired; existing sessions keep their existing scope until expiry.

Admin and the appointment's assigned doctor can save or clear a full HTTPS meeting URL. Embedded URL credentials are rejected. The server never fetches the link. An authorized signed-in family sees an existing link for current pending or confirmed appointments. Cancellation, expiry, rejection and refund states hide it. Doctor reassignment clears the old link. Book generation entry points have a deep-pink border; light and dark themes remain available. Calls display dates and times in Asia/Kolkata (IST).

## Earnings and balances

Admin sets a fixed integer-paise earning amount at `PUT /api/admin/earnings`, separately from the patient consultation fee. Blank disables earnings for future appointments; zero produces no ledger credit. A new appointment snapshots the assigned doctor and configured amount. Existing appointments receive no snapshot or historical backfill.

Only a currently verified captured payment that matches the stored order, amount and INR currency can earn credit. Authorization and the checkout callback alone cannot. The appointment must still be eligible, not expired or cancelled, and must retain its snapshotted doctor. Attaching an old captured payment does not create new earnings.

The ledger has independent unique identities for appointment earnings, payment credit and withdrawal debit, in addition to webhook event deduplication. Credit, refund, booking and withdrawal writes share the existing transaction advisory lock. This deliberately serializes writes for the current clinic workload; it is not intended for unrestricted payment throughput.

- Balance is the signed ledger total.
- Reserved is the gross total of requested/processing withdrawals.
- Available is balance minus reserved.
- A successful manual paid record debits gross once and removes its reservation in the same transaction.
- Cancellation or rejection releases the reservation without a debit.
- A credited appointment cannot be reassigned to another doctor. Reconcile/refund it and create a new appointment instead.

A partial refund conservatively reverses the whole fixed earning and holds the wallet for reconciliation, because no proportional earning rule has been agreed. A full refund keeps the single reversal and clears a partial-refund hold. Dispute events freeze/reverse the affected earning even if delivered after capture. A later capture replay or dispute-won event does not silently restore it. Negative balances, refund-required appointments and disputes block new requests and paid marks. There is no automatic debt recovery or reconciliation override. Finance must resolve these exceptional cases under a separately reviewed policy. If a manual bank transfer races a refund, preserve its receipt; the app refuses paid recording until reconciliation is resolved.

## Private manual withdrawals

Configure `WALLET_ENCRYPTION_KEY` as a stable, server-only 32-byte key encoded with 64 hex characters. Invalid keys fail startup; an absent key disables saving payout details and requesting withdrawals. Earnings still accrue. Store the key in the deployment secret store and retain it securely with database backups. Replacing it without decrypting/re-encrypting all destinations and immutable request snapshots makes them unreadable. No automatic key rotation is implemented.

A doctor saves account holder plus bank, IFSC and confirmed account number, or account holder plus UPI ID. These fields are encrypted using AES-256-GCM, a fresh nonce and the doctor identity as authenticated context. API lists return masks only. Each request snapshots its encrypted destination; later edits cannot redirect it. An administrator can explicitly reveal a pending request's destination for manual transfer review; that access is audited. Do not place payout data in review notes or logs.

Request amounts range from ₹1 to ₹10,00,000 and cannot exceed available funds. The client supplies a UUID retry key; repeated submissions of the same amount/key reuse the same request. A changed amount with the same key conflicts. Concurrent requests reserve atomically. The estimate is 2 to 4 business days, not a guaranteed settlement deadline.

Admin may mark processing, reject, or record paid. A paid record requires the actual transferred amount, transfer reference, actual time (IST in the form), and explicit confirmation that the transfer is already complete. The check mark records a manual transfer and never initiates one. Matching retries are idempotent; conflicting transfer facts are rejected. A doctor can cancel only while requested, before processing.

New financial tables use the private schema with RLS and no PUBLIC, anon or authenticated privileges. Route middleware enforces staff roles; family sessions cannot read wallets and doctors cannot access another doctor's withdrawals. Default history lists show the latest 200 ledger entries and 500 withdrawals; balance calculations include all rows.

## Requested tax calculation and inactive policy

The owner requests 18% added to family fees and an 18% deduction from doctor withdrawals, and states that AgroGamut Services Private Limited is GST registered. This is a requested policy, not a legal conclusion. A qualified Indian GST adviser must confirm the actual supplies, healthcare exemptions, contractual deduction basis, invoicing, place of supply, reporting and any other withholding before activation. Registration alone does not establish that either charge is lawful GST. The implementation is an aggregate calculation, not a complete tax invoice or statutory filing system.

Both approval variables default to empty and have no browser activation control:

| Server variable | Empty/default behavior | Explicit reviewed reference |
| --- | --- | --- |
| `PORTAL_PATIENT_TAX_APPROVAL` | Family checkout uses the actual current consultation price, without a new charge. | New orders snapshot base, 18% charge and total. |
| `PORTAL_WITHDRAWAL_DEDUCTION_APPROVAL` | Show the requested 18% estimate as provisional. Final payout is pending review; server rejects paid actions. | New requests snapshot the reviewed 18% deduction and final amount. |

References are short, non-sensitive identifiers for a documented review, not booleans. Do not enable these variables solely to make a pending withdrawal payable. Qualified review and operational invoicing/reporting readiness are prerequisites.

Requested example: gross ₹2,000 (200,000 paise), provisional or reviewed deduction ₹360 (36,000 paise), net ₹1,640 (164,000 paise). Integer rounding is half-up to the nearest paise: `(gross_paise * 1800 + 5000) / 10000`. The wallet always reserves/debits ₹2,000, not ₹1,640.

A request made before approval retains `pending_review` permanently. Later approval does not reprice or authorize it. The doctor must cancel it, or admin reject it, then the doctor creates and reconfirms a fresh request. A pending request under a revoked/different policy is also blocked. The current approved reference must match its immutable snapshot before paid. Completed history remains unchanged.

Existing patient orders keep their exact stored amounts through policy/fee changes. Checkout asks the family to confirm the actual order total before opening the provider. The family screen uses the existing order snapshot where present.

## Deployment and verification

This feature has not been deployed by this work. Migrations run on API startup. Migration 0047 is additive; its down migration refuses to discard ledger, withdrawal or order-price history. Roll forward once financial data exists.

Release API and frontend from the same reviewed commit. Apply the API migration before serving the new frontend. During a mixed-version interval an old family form lacks required guardian name and is denied by the new API; refresh after frontend rollout. Conversely, the old API rejects new fields/routes. Existing authenticated sessions and staff login remain valid. Avoid new intake during the brief mixed deployment window. Do not configure earnings until both services are verified; appointments created by an older API have no earning snapshot.

Keep both tax approval variables unset until their independent reviews are complete. Configure the encryption key before enabling payout destination entry, then set the intended fixed earning amount. Verify both deployed revisions, migration state and health independently. Use authorized synthetic test data for any later acceptance check; local evidence does not establish provider or live deployment status.

Subscribe the existing signed payment webhook to the documented payment/refund events and to `payment.dispute.created`, `payment.dispute.under_review`, `payment.dispute.action_required`, `payment.dispute.won`, `payment.dispute.lost` and `payment.dispute.closed`. Dispute handling verifies the signature and fetches current payment state. An undisclosed/unreceived provider dispute cannot be detected by the app; external notification configuration remains an operational dependency.

Tests use `PORTAL_TEST_DATABASE_URL` for a disposable PostgreSQL database. The wallet suite exercises populated migration upgrade, no backfill, privileges, cross-account authorization, capture replay, competing withdrawals, refunds/disputes, policy transition and manual paid idempotency. `go test ./...`, `go build ./...` and `go vet ./...` remain required. Corpus tests separately need `TEST_DATABASE_URL` and imported provider data.

The browser fixture uses a separate database with synthetic identity and gateway responses only. Run `web/e2e/consultation-wallet.spec.ts` once with default policy and once with `PORTAL_BROWSER_REVIEWED_POLICY=1` on both fixture and test process. It covers real Next.js/Go/PostgreSQL flows, mobile/light/dark views and the exact requested arithmetic without real transfers. Run the existing family-consultation browser regression too.

No application request quotas are restored. Hosting, proxies, identity and payment providers may still produce independent HTTP 429 responses.
