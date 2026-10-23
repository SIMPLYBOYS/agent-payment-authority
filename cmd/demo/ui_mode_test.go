package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// /api/mode names the payment capability of a host that pays (gap review B4),
// so the viewer page never says "no payment" there.
func TestModeSaysPayments(t *testing.T) {
	for _, c := range []struct {
		a    *api
		want string
	}{
		{&api{addr: "127.0.0.1:1"}, `{"mode":"fake"}`},
		{&api{addr: "127.0.0.1:1", payCap: BaseSepolia}, `{"mode":"fake","payments":"base_sepolia"}`},
		{&api{addr: "127.0.0.1:1", payCap: LocalAnvil, live: &liveInfo{Record: "live_demo", Label: "L"}}, `"payments":"local_anvil"`},
		{&api{addr: "127.0.0.1:1", payCap: BaseSepolia, approve: &approvals{source: "virtual"}}, `{"approval":"webauthn","authenticator":"virtual","mode":"fake","payments":"base_sepolia"}`},
		{&api{addr: "127.0.0.1:1", approve: &approvals{source: "device"}}, `{"approval":"webauthn","authenticator":"device","mode":"fake"}`},
		{&api{addr: "127.0.0.1:1", payCap: LocalAnvil, history: &historyInfo{Record: "live_history", Label: "H"}}, `{"label":"H","mode":"history","record":"live_history"}`},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/mode", nil)
		r.Host = "127.0.0.1:1"
		c.a.mode(w, r)
		if body := strings.TrimSpace(w.Body.String()); w.Code != 200 || !strings.Contains(body, c.want) {
			t.Errorf("%d %s, want %s", w.Code, body, c.want)
		}
	}
}
