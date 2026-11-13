package portal

import (
	"testing"
	"time"
)

func TestGuardianTransitionOnlyAllowsLegacyOmission(t *testing.T) {
	f := newPaidFixture(t)
	id, secret := f.register(t, "Transition child")
	sibling, _ := f.register(t, "Other transition child")
	for _, mode := range []struct {
		name   string
		until  time.Time
		legacy bool
	}{
		{"strict", time.Time{}, false},
		{"transition", time.Now().Add(time.Hour), true},
		{"expired", time.Now().Add(-time.Second), false},
	} {
		t.Run(mode.name, func(t *testing.T) {
			f.server.options.LegacyGuardianUntil = mode.until
			for _, route := range []string{"/api/family/auth/access", "/api/family/auth/recover"} {
				t.Run(route, func(t *testing.T) {
					for _, tc := range []struct {
						name                                    string
						guardian                                any
						omit, noSecret, wrongSecret, wrongChild bool
						want                                    int
					}{
						{"omitted", nil, true, false, false, false, 401},
						{"correct", "  TEST  Guardian ", false, false, false, false, 200},
						{"wrong", "Wrong guardian", false, false, false, false, 401},
						{"blank", " ", false, false, false, false, 401},
						{"null", nil, false, false, false, false, 401},
						{"number", 5, false, false, false, false, 401},
						{"missing private credential", nil, true, true, false, false, 401},
						{"wrong private credential", nil, true, false, true, false, 401},
						{"wrong child", nil, true, false, false, true, 401},
					} {
						t.Run(tc.name, func(t *testing.T) {
							credential := secret
							if route == "/api/family/auth/recover" {
								issued := portalRequest(f.router, "POST", "/api/admin/registrations/"+id+"/family-access", map[string]bool{"guardian_verified": true}, f.adminCookie, "")
								expectCode(t, issued, 201)
								credential = decodeResponse[struct {
									Token string `json:"token"`
								}](t, issued).Token
							}
							body := map[string]any{"child_name": "Transition child", "date_of_birth": "2022-05-01", "token": credential}
							if !tc.omit {
								body["guardian_name"] = tc.guardian
							}
							if tc.noSecret {
								delete(body, "token")
							}
							if tc.wrongSecret {
								body["token"] = token()
							}
							if tc.wrongChild {
								body["child_name"] = "Other transition child"
							}
							want := tc.want
							if tc.name == "omitted" && mode.legacy {
								want = 200
							}
							w := portalRequest(f.router, "POST", route, body, nil, "")
							expectCode(t, w, want)
							if route == "/api/family/auth/recover" {
								if want != 200 {
									body["guardian_name"] = "Test Guardian"
									body["child_name"] = "Transition child"
									body["token"] = credential
									w = portalRequest(f.router, "POST", route, body, nil, "")
									expectCode(t, w, 200)
								}
								access := decodeResponse[map[string]string](t, w)
								if access["id"] != id {
									t.Fatal("recovery escaped selected registration")
								}
								secret = access["token"]
								expectCode(t, portalRequest(f.router, "POST", route, body, nil, ""), 401)
								w = portalRequest(f.router, "POST", "/api/family/auth/access", map[string]string{"guardian_name": "Test Guardian", "child_name": "Transition child", "date_of_birth": "2022-05-01", "token": secret}, nil, "")
								expectCode(t, w, 200)
							}
							if w.Code == 200 {
								cookie := w.Result().Cookies()[0]
								expectCode(t, portalRequest(f.router, "GET", "/api/family/registrations/"+id, nil, cookie, ""), 200)
								expectCode(t, portalRequest(f.router, "GET", "/api/family/registrations/"+sibling, nil, cookie, ""), 404)
							}
						})
					}
				})
			}
		})
	}
}
