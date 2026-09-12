# Consultation intake and staff access

1. Add private Postgres tables for staff, registrations, assignment, consultation settings and payment records. Keep guardian details separate from clinical profiles.
2. Add Supabase staff authentication with server-held cookies. Enforce active accounts, admin privileges and doctor assignments at the Go API. Preserve unrestricted inline book generation for signed-in doctors.
3. Add registration and optional Razorpay checkout. Set amounts on the server, verify captured payments and process signed, repeatable webhooks. Leave checkout unavailable until credentials and a fee are configured.
4. Build the English MadamGY landing page, staff login, admin workspace and doctor caseload. Reuse the existing book form with assigned-child prefilling.
5. Verify authorization, validation, payment replay handling, database changes and frontend behavior. Preview the actual app. Record credential-dependent verification separately.

Design: the supplied MadamGY logo sets the pink accent. Use warm blush (#fff0f4), deeper blush (#ffe6ee), pale rose (#fce3eb), plum text (#58293b), muted rose (#815b69), and a darker pink button (#c31352) for legibility. Keep the existing Geist family. The informative heading sits beside the intake form; process and questions sit below. Use the supplied logo unchanged. Staff screens retain compact tables and visible statuses. No invented testimonials, clinical claims, prices or imagery. The engine remains at /console.

Status: public page, intake, payment integration and role-restricted staff workspaces are implemented. Backend tests with the complete disposable database and 42 frontend tests pass; lint and the webpack production build pass. Hosted Supabase and Razorpay credentials are pending. Local Supabase Auth now supports working admin and doctor preview accounts. Both roles signed in through the actual shared-browser form; doctor access to administration is rejected. The theme hydration issue found during staff browser testing is fixed. Native date entry and screenshot automation were unreliable in earlier checks; interactive intake submission remains unverified. No publishing requested.

Connection detail found during verification: startup migrations need session advisory locks, so the hosted Supabase pooler uses its session port for migrations and its transaction port for application queries. Password resets and deactivation serialize with session creation to prevent an in-flight login from surviving revocation.

Local account request completed: official Supabase Auth runs against its own schema in the disposable database, behind a loopback TLS gateway. One administrator and one doctor were created through the existing account APIs. Both logins and role restrictions passed API and browser checks. Preview secrets stay outside version control with unique generated account passwords. The app has no authentication bypass. The visible preview is left on the administrator's Doctors tab.
