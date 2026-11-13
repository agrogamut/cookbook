package portal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func walletMigration(t *testing.T, direction string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "db", "migrations", "0047_consultation_wallet."+direction+".sql"))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestWalletUpgradePreservesPopulatedLegacyRecords(t *testing.T) {
	f := newPaidFixture(t)
	id, _ := f.register(t, "Legacy migration child")
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, walletMigration(t, "down")); err != nil {
		t.Fatal(err)
	}
	var appointment string
	err = tx.QueryRow(ctx, `INSERT INTO app_private.appointment(registration_id,doctor_id,starts_at,ends_at,mode,status,requested_by) VALUES ($1,$2,$3,$3::timestamptz+interval '30 minutes','specific_doctor','confirmed',$4) RETURNING id`, id, f.doctors[0].ID, f.start, f.admin.ID).Scan(&appointment)
	if err != nil {
		t.Fatal(err)
	}
	order := "order_" + token()[:16]
	if _, err = tx.Exec(ctx, `INSERT INTO app_private.consultation_order(id,registration_id,appointment_id,amount_paise,currency,status,payment_id) VALUES($1,$2,$3,25000,'INR','paid',$4)`, order, id, appointment, "pay_"+token()[:16]); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, walletMigration(t, "up")); err != nil {
		t.Fatal(err)
	}
	var legacy bool
	if err = tx.QueryRow(ctx, `SELECT a.earning_doctor_id IS NULL AND a.earning_paise IS NULL AND a.earning_policy IS NULL AND a.meeting_url='' AND o.base_paise IS NULL AND o.tax_paise=0 AND o.amount_paise=25000 AND o.status='paid' AND r.guardian_description='' FROM app_private.appointment a JOIN app_private.consultation_order o ON o.appointment_id=a.id JOIN app_private.consultation_registration r ON r.id=a.registration_id WHERE a.id=$1`, appointment).Scan(&legacy); err != nil || !legacy {
		t.Fatalf("populated records changed: %v", err)
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM app_private.doctor_ledger`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("historical credit created: %d %v", count, err)
	}
	var earning *int
	if err = tx.QueryRow(ctx, `SELECT doctor_earning_paise FROM app_private.consultation_settings WHERE singleton`).Scan(&earning); err != nil || earning != nil {
		t.Fatalf("earnings unexpectedly enabled: %v", err)
	}
	for _, table := range []string{"doctor_destination", "doctor_withdrawal", "doctor_ledger", "wallet_reconciliation", "wallet_audit"} {
		var secured bool
		err = tx.QueryRow(ctx, `SELECT c.relrowsecurity AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(c.relacl,acldefault('r',c.relowner))) acl LEFT JOIN pg_roles r ON r.oid=acl.grantee WHERE acl.grantee=0 OR r.rolname IN ('anon','authenticated')) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='app_private' AND c.relname=$1`, table).Scan(&secured)
		if err != nil || !secured {
			t.Fatalf("%s browser protection missing: %v", table, err)
		}
	}
	if _, err = tx.Exec(ctx, `SAVEPOINT incomplete_snapshot`); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `UPDATE app_private.appointment SET earning_doctor_id=doctor_id,earning_paise=100,earning_policy=NULL WHERE id=$1`, appointment)
	if err == nil {
		t.Fatal("incomplete earning snapshot accepted")
	}
	if _, err = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT incomplete_snapshot`); err != nil {
		t.Fatal(err)
	}
}

func TestWalletMigrationRefusesFinancialHistoryLoss(t *testing.T) {
	f, doctor := walletFixture(t)
	walletCredit(t, f, 0)
	saveTestDestination(t, f, doctor)
	w := portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals", map[string]any{"amount_paise": 200000, "request_key": testUUID()}, doctor, "")
	expectCode(t, w, 201)
	value := decodeResponse[Withdrawal](t, w)
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SAVEPOINT financial_guard`); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, walletMigration(t, "down"))
	if err == nil || !strings.Contains(err.Error(), "Preserve financial history") {
		t.Fatalf("unsafe rollback accepted: %v", err)
	}
	if _, err = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT financial_guard`); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `UPDATE app_private.doctor_withdrawal SET status='paid',decided_by=$2,transferred_at=now(),transferred_paise=NULL,transfer_reference=NULL WHERE id=$1`, value.ID, f.admin.ID)
	if err == nil {
		t.Fatal("paid without transfer facts accepted")
	}
}
