package portal

import (
	"context"
	"strings"
	"testing"
)

func TestCheckoutVersionBoundaryPreservesOrdersAndAuthentication(t *testing.T) {
	f, _ := walletFixture(t)
	ctx := context.Background()
	id, secret := f.register(t, "Synthetic rollout child")
	f.book(t, id, secret, f.doctors[0].ID, f.start)
	family := childCookie(t, f, "Synthetic rollout child", secret)
	public := "/api/public/registrations/" + id
	private := "/api/family/registrations/" + id
	legacy := f.order(t, id, secret)
	f.server.options.TaxPolicy = TaxPolicy{}
	before := f.gateway.creates
	for _, route := range []string{public, private} {
		cookie := family
		if route == public {
			cookie = nil
		}
		for i := 0; i < 2; i++ {
			w := portalRequest(f.router, "POST", route+"/order", map[string]any{}, cookie, secret)
			expectCode(t, w, 409)
			if !strings.Contains(w.Body.String(), "Refresh") {
				t.Fatal("legacy client lacks refresh guidance")
			}
		}
		w := portalRequest(f.router, "POST", route+"/order/itemized-v1", map[string]any{}, cookie, secret)
		expectCode(t, w, 200)
		v := decodeResponse[struct {
			ID     string `json:"order_id"`
			Amount int    `json:"amount_paise"`
			Price  Price  `json:"price"`
		}](t, w)
		if v.ID != legacy.ID || v.Amount != 25000 || v.Price.TotalPaise != 25000 || v.Price.TaxPaise != 0 || v.Price.Policy != "inactive" {
			t.Fatalf("old order changed: %+v", v)
		}
	}
	if f.gateway.creates != before {
		t.Fatal("refused legacy request or stored order called provider")
	}
	expectCode(t, portalRequest(f.router, "POST", private+"/order/itemized-v1", nil, nil, ""), 401)
	expectCode(t, portalRequest(f.router, "POST", public+"/order/itemized-v1", nil, nil, token()), 404)
	id2, secret2 := f.register(t, "Synthetic new priced child")
	f.book(t, id2, secret2, f.doctors[1].ID, f.start)
	w := portalRequest(f.router, "POST", "/api/public/registrations/"+id2+"/order", nil, nil, secret2)
	expectCode(t, w, 409)
	var orders int
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM app_private.consultation_order WHERE registration_id=$1", id2).Scan(&orders); err != nil || orders != 0 {
		t.Fatalf("legacy request wrote order: %d %v", orders, err)
	}
	if f.gateway.creates != before {
		t.Fatal("legacy request contacted provider")
	}
	active := f.order(t, id2, secret2)
	if active.Amount != 29500 {
		t.Fatalf("new order default = %d", active.Amount)
	}
	if f.gateway.creates != before+1 {
		t.Fatal("wrong order creation count")
	}
}

func TestMeetingCompareAndSetRejectsExternalChangeAndReassignment(t *testing.T) {
	f, doctor := walletFixture(t)
	ctx := context.Background()
	id, secret := f.register(t, "Synthetic concurrent meeting")
	appointment := f.book(t, id, secret, f.doctors[0].ID, f.start)
	path := "/api/appointments/" + appointment + "/meeting"
	body := map[string]string{"meeting_url": "https://meet.example.invalid/one", "expected_doctor_id": f.doctors[0].ID, "expected_meeting_url": ""}
	expectCode(t, portalRequest(f.router, "PUT", path, body, doctor, ""), 200)
	stale := map[string]string{"meeting_url": "https://meet.example.invalid/stale", "expected_doctor_id": f.doctors[0].ID, "expected_meeting_url": ""}
	expectCode(t, portalRequest(f.router, "PUT", path, stale, f.adminCookie, ""), 409)
	body["expected_meeting_url"] = body["meeting_url"]
	body["meeting_url"] = "https://meet.example.invalid/two"
	expectCode(t, portalRequest(f.router, "PUT", path, body, f.adminCookie, ""), 200)
	if _, err := f.pool.Exec(ctx, "UPDATE app_private.appointment SET doctor_id=$2,meeting_url='' WHERE id=$1", appointment, f.doctors[1].ID); err != nil {
		t.Fatal(err)
	}
	expectCode(t, portalRequest(f.router, "PUT", path, body, f.adminCookie, ""), 409)
	expectCode(t, portalRequest(f.router, "PUT", path, body, doctor, ""), 404)
	expectCode(t, portalRequest(f.router, "PUT", path, map[string]string{"meeting_url": body["meeting_url"]}, f.adminCookie, ""), 409)
	var link string
	if err := f.pool.QueryRow(ctx, "SELECT meeting_url FROM app_private.appointment WHERE id=$1", appointment).Scan(&link); err != nil || link != "" {
		t.Fatalf("old link restored: %q %v", link, err)
	}
	body["expected_doctor_id"] = f.doctors[1].ID
	body["expected_meeting_url"] = ""
	body["meeting_url"] = "https://meet.example.invalid/new-doctor"
	expectCode(t, portalRequest(f.router, "PUT", path, body, f.adminCookie, ""), 200)
}
