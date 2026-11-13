package portal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"os"
	"path/filepath"
	"testing"
)

func refundMigration(t *testing.T, direction string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "db", "migrations", "0048_refund_decisions."+direction+".sql"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func TestRefundUpgradePreservesPopulatedFinancialSnapshots(t *testing.T) {
	f, doctor := walletFixture(t)
	_, p := walletCredit(t, f, 0)
	saveTestDestination(t, f, doctor)
	w := portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals", map[string]any{"amount_paise": 200000, "request_key": testUUID()}, doctor, "")
	expectCode(t, w, 201)
	withdrawal := decodeResponse[Withdrawal](t, w)
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, refundMigration(t, "down")); err != nil {
		t.Fatal(err)
	}
	var before string
	query := `SELECT jsonb_build_object('order',(SELECT to_jsonb(o) FROM app_private.consultation_order o WHERE id=$1),'withdrawal',(SELECT to_jsonb(w) FROM app_private.doctor_withdrawal w WHERE id=$2),'ledger',(SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM app_private.doctor_ledger l WHERE doctor_id=$3))::text`
	if err = tx.QueryRow(ctx, query, p.OrderID, withdrawal.ID, f.doctors[0].ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, refundMigration(t, "up")); err != nil {
		t.Fatal(err)
	}
	var after string
	post := `SELECT jsonb_build_object('order',(SELECT to_jsonb(o) FROM app_private.consultation_order o WHERE id=$1),'withdrawal',(SELECT to_jsonb(w) FROM app_private.doctor_withdrawal w WHERE id=$2),'ledger',(SELECT jsonb_agg(to_jsonb(l)-'refund_id' ORDER BY id) FROM app_private.doctor_ledger l WHERE doctor_id=$3))::text`
	if err = tx.QueryRow(ctx, post, p.OrderID, withdrawal.ID, f.doctors[0].ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("upgrade changed existing price, earning or withdrawal snapshots")
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM app_private.payment_refund)+(SELECT count(*) FROM app_private.refund_decision)`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invented historical identities: %d %v", count, err)
	}
	for _, table := range []string{"payment_refund", "refund_decision"} {
		var secured bool
		err = tx.QueryRow(ctx, `SELECT c.relrowsecurity AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(c.relacl,acldefault('r',c.relowner))) acl LEFT JOIN pg_roles r ON r.oid=acl.grantee WHERE acl.grantee=0 OR r.rolname IN ('anon','authenticated')) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='app_private' AND c.relname=$1`, table).Scan(&secured)
		if err != nil || !secured {
			t.Fatalf("%s private protection missing: %v", table, err)
		}
	}
}
func TestRefundRollbackRefusesDecisionHistoryLoss(t *testing.T) {
	f, _ := walletFixture(t)
	_, p := walletCredit(t, f, 0)
	_, cases := applyTestRefund(t, f, p, 1)
	expectCode(t, refundDecision(f, cases[0].ID, refundBody(0, 500000, "Retain earning"), f.adminCookie), 200)
	tx, err := f.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(context.Background(), refundMigration(t, "down")); err == nil {
		t.Fatal("rollback discarded refund decisions")
	}
}

func TestRefundPrivateRowsRemainInvisibleWithAccidentalSelect(t *testing.T) {
	f, _ := walletFixture(t)
	_, p := walletCredit(t, f, 0)
	_, cases := applyTestRefund(t, f, p, 1)
	expectCode(t, refundDecision(f, cases[0].ID, refundBody(0, 500000, "Retain earning"), f.adminCookie), 200)
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	role := pgx.Identifier{"refund_browser_" + token()[:12]}.Sanitize()
	for _, sql := range []string{"CREATE ROLE " + role + " NOLOGIN", "GRANT USAGE ON SCHEMA app_private TO " + role, "GRANT SELECT ON app_private.payment_refund,app_private.refund_decision TO " + role, "SET LOCAL ROLE " + role} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	var visible int
	if err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM app_private.payment_refund)+(SELECT count(*) FROM app_private.refund_decision)`).Scan(&visible); err != nil || visible != 0 {
		t.Fatalf("private rows visible=%d: %v", visible, err)
	}
}
